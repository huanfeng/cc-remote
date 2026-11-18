package server

import (
	"cc-remote/internal/store"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

const localSupplierKind = "local"
const localSupplierPrefix = "local:"

var supplierConfigsMu sync.Mutex

type supplierConfig struct {
	SourceID      string            `json:"sourceId,omitempty"`
	ID            string            `json:"id"`
	Website       string            `json:"website,omitempty"`
	Notes         string            `json:"notes,omitempty"`
	ModelMappings map[string]string `json:"modelMappings,omitempty"`
	ModelNames    map[string]string `json:"modelNames,omitempty"`
	TimeoutMs     int64             `json:"timeoutMs,omitempty"`
	CodexOptions  map[string]any    `json:"codexOptions,omitempty"`
	Name          string            `json:"name"`
	App           string            `json:"app"`
	BaseURL       string            `json:"baseUrl"`
	Model         string            `json:"model"`
	AuthType      string            `json:"authType"`
	HasCredential bool              `json:"hasCredential"`
	Revision      string            `json:"revision"`
	Credential    string            `json:"credential,omitempty"`
}
type supplierConfigReply struct {
	CCSwitchHidden bool             `json:"ccSwitchHidden"`
	Profiles       []supplierConfig `json:"profiles"`
	Revision       string           `json:"revision"`
	Capabilities   map[string]bool  `json:"capabilities"`
}

func readSupplierConfigs(dir string) ([]supplierConfig, []byte, error) {
	b, e := os.ReadFile(filepath.Join(dir, "supplier-configs.json"))
	if os.IsNotExist(e) {
		return []supplierConfig{}, nil, nil
	}
	if e != nil {
		return nil, nil, e
	}
	var v []supplierConfig
	e = json.Unmarshal(b, &v)
	return v, b, e
}
func safeSupplierConfigs(v []supplierConfig, b []byte) supplierConfigReply {
	x := supplierConfigReply{Profiles: make([]supplierConfig, 0, len(v)), Revision: settingsRevision(b), Capabilities: map[string]bool{"advanced": true, "modelNames": true, "probe": true, "ccSwitchImport": true}}
	for _, p := range v {
		p.HasCredential = p.Credential != ""
		p.Credential = ""
		p.Revision = x.Revision
		x.Profiles = append(x.Profiles, p)
	}
	return x
}
func (p supplierConfig) profile() supplierProfile {
	x := supplierProfile{ID: p.ID, Name: p.Name, App: p.App}
	x.Settings.Kind = localSupplierKind
	if p.App == "claude" {
		key := "ANTHROPIC_API_KEY"
		if p.AuthType == "authToken" {
			key = "ANTHROPIC_AUTH_TOKEN"
		}
		x.Settings.Env = map[string]string{"ANTHROPIC_BASE_URL": p.BaseURL, key: p.Credential}
		if p.Model != "" {
			x.Settings.Env["ANTHROPIC_MODEL"] = p.Model
		}
		for k, v := range p.ModelMappings {
			if v != "" {
				x.Settings.Env["ANTHROPIC_DEFAULT_"+strings.ToUpper(k)+"_MODEL"] = v
			}
		}
		for k, v := range p.ModelNames {
			if v != "" {
				x.Settings.Env["ANTHROPIC_DEFAULT_"+strings.ToUpper(k)+"_MODEL_NAME"] = v
			}
		}
		if p.TimeoutMs > 0 {
			x.Settings.Env["API_TIMEOUT_MS"] = strconv.FormatInt(p.TimeoutMs, 10)
		}
	} else {
		x.Settings.Auth = map[string]string{"OPENAI_API_KEY": p.Credential}
		x.Settings.ConfigOverrides = map[string]any{"model_provider": "cc_remote", "model_providers": map[string]any{"cc_remote": map[string]any{"name": p.Name, "base_url": p.BaseURL, "env_key": "OPENAI_API_KEY", "wire_api": "responses"}}}
		if p.Model != "" {
			x.Settings.ConfigOverrides["model"] = p.Model
		}
		for k, v := range p.CodexOptions {
			if v != "" && v != float64(0) {
				x.Settings.ConfigOverrides[codexSupplierKeys[k]] = v
			}
		}
	}
	return x
}
func validSupplierConfig(p supplierConfig) bool {
	if !validSupplierAdvanced(p) {
		return false
	}
	if !validOAuthName(p.Name) || len(p.BaseURL) > 2048 || len(p.Model) > 160 || len(p.Credential) > 8192 || p.Credential == "" {
		return false
	}
	if strings.ContainsFunc(p.Credential, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) || strings.ContainsFunc(p.Model, unicode.IsControl) {
		return false
	}
	u, e := url.Parse(p.BaseURL)
	if e != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if p.AuthType != "apiKey" && p.AuthType != "authToken" {
		return false
	}
	if p.App == "codex" && p.AuthType != "apiKey" {
		return false
	}
	return p.profile().validate() == nil
}
func (s *Server) supplierConfigsAPI(w http.ResponseWriter, r *http.Request) {
	supplierConfigsMu.Lock()
	defer supplierConfigsMu.Unlock()
	v, b, e := readSupplierConfigs(s.cfg.DataDir)
	if e != nil {
		fail(w, 500, "无法读取供应商配置 / Could not read supplier configurations")
		return
	}
	if r.Method == http.MethodGet {
		reply := safeSupplierConfigs(v, b)
		state, _, stateErr := readCCSwitchState(s.cfg.DataDir)
		if stateErr != nil {
			fail(w, 500, "无法读取 CC-Switch 显示设置")
			return
		}
		reply.CCSwitchHidden = state.Hidden
		respond(w, 200, reply)
		return
	}
	var raw json.RawMessage
	if !decode(w, r, &raw) {
		return
	}
	var req supplierConfig
	if json.Unmarshal(raw, &req) != nil {
		fail(w, 400, "Invalid supplier configuration")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return
	}
	if req.Revision != settingsRevision(b) {
		fail(w, 409, "供应商配置已修改，请刷新 / Supplier settings changed; reload")
		return
	}
	id := r.PathValue("id")
	at := -1
	for i, p := range v {
		if p.ID == id {
			at = i
		}
	}
	if r.Method != http.MethodPost && at < 0 {
		fail(w, 404, "供应商不存在 / Supplier not found")
		return
	}
	if r.Method == http.MethodDelete {
		v = append(v[:at], v[at+1:]...)
	} else {
		req.Name = strings.TrimSpace(req.Name)
		req.BaseURL = strings.TrimSpace(req.BaseURL)
		req.Model = strings.TrimSpace(req.Model)
		if r.Method == http.MethodPost {
			req.ID = localSupplierPrefix + store.ID()
			req.SourceID = ""
		} else {
			req.ID = id
			old := v[at]
			req.SourceID = old.SourceID
			if _, ok := fields["website"]; !ok {
				req.Website = old.Website
			}
			if _, ok := fields["notes"]; !ok {
				req.Notes = old.Notes
			}
			if _, ok := fields["modelMappings"]; !ok {
				req.ModelMappings = old.ModelMappings
			}
			if _, ok := fields["modelNames"]; !ok {
				req.ModelNames = old.ModelNames
			}
			if _, ok := fields["timeoutMs"]; !ok {
				req.TimeoutMs = old.TimeoutMs
			}
			if _, ok := fields["codexOptions"]; !ok {
				req.CodexOptions = old.CodexOptions
			}
			if req.App != v[at].App {
				fail(w, 400, "不能更改供应商类型 / Cannot change supplier application")
				return
			}
			if req.Credential == "" {
				req.Credential = v[at].Credential
			}
		}
		if !validSupplierConfig(req) {
			fail(w, 400, "供应商配置无效：请检查名称、HTTP(S)端点、认证类型和凭据 / Invalid supplier configuration")
			return
		}
		req.HasCredential = false
		req.Revision = ""
		if at < 0 {
			v = append(v, req)
		} else {
			v[at] = req
		}
	}
	next, _ := json.MarshalIndent(v, "", "  ")
	e = writeSupplierData(s.cfg.DataDir, "supplier-configs.json", next)
	if e != nil {
		fail(w, 500, "无法保存供应商配置 / Could not save supplier configurations")
		return
	}
	reply := safeSupplierConfigs(v, next)
	state, _, _ := readCCSwitchState(s.cfg.DataDir)
	reply.CCSwitchHidden = state.Hidden
	respond(w, 200, reply)
}
func (s *Server) localSupplierProfiles() ([]supplierProfile, error) {
	supplierConfigsMu.Lock()
	defer supplierConfigsMu.Unlock()
	v, _, e := readSupplierConfigs(s.cfg.DataDir)
	var out []supplierProfile
	for _, p := range v {
		out = append(out, p.profile())
	}
	return out, e
}
func (s *Server) resolveLocalSupplier(id string) (supplierProfile, error) {
	v, e := s.localSupplierProfiles()
	if e != nil {
		return supplierProfile{}, e
	}
	for _, p := range v {
		if p.ID == id {
			return p, nil
		}
	}
	return supplierProfile{}, errors.New("供应商配置已删除，请重新选择供应商")
}

var codexSupplierKeys = map[string]string{"reasoningEffort": "model_reasoning_effort", "reasoningSummary": "model_reasoning_summary", "verbosity": "model_verbosity", "contextWindow": "model_context_window", "autoCompactTokenLimit": "model_auto_compact_token_limit", "disableResponseStorage": "disable_response_storage"}

func validSupplierAdvanced(p supplierConfig) bool {
	if len(p.Website) > 2048 || len(p.Notes) > 2000 || p.TimeoutMs < 0 || p.TimeoutMs > 3600000 {
		return false
	}
	if p.Website != "" {
		u, e := url.Parse(p.Website)
		if e != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
	}
	if p.App != "claude" && (len(p.ModelMappings) > 0 || len(p.ModelNames) > 0 || p.TimeoutMs != 0) {
		return false
	}
	if p.App != "codex" && len(p.CodexOptions) > 0 {
		return false
	}
	for _, fields := range []map[string]string{p.ModelMappings, p.ModelNames} {
		for k, v := range fields {
			if (k != "haiku" && k != "sonnet" && k != "opus" && k != "fable") || len(v) > 160 || strings.ContainsFunc(v, unicode.IsControl) {
				return false
			}
		}
	}
	for k, v := range p.CodexOptions {
		if _, ok := codexSupplierKeys[k]; !ok {
			return false
		}
		switch k {
		case "disableResponseStorage":
			if _, ok := v.(bool); !ok {
				return false
			}
		case "contextWindow", "autoCompactTokenLimit":
			n, ok := v.(float64)
			if !ok || n < 0 || n > 1e8 || n != float64(int64(n)) {
				return false
			}
		default:
			text, ok := v.(string)
			if !ok {
				return false
			}
			choices := map[string]string{"reasoningEffort": " minimal low medium high xhigh none ", "reasoningSummary": " auto concise detailed none ", "verbosity": " low medium high "}
			if text != "" && !strings.Contains(choices[k], " "+text+" ") {
				return false
			}
		}
	}
	return true
}

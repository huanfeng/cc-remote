package server

import (
	"cc-remote/internal/store"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ccSwitchState struct {
	Hidden   bool              `json:"hidden"`
	Defaults map[string]string `json:"defaults,omitempty"`
}

func readCCSwitchState(dir string) (ccSwitchState, []byte, error) {
	var out ccSwitchState
	b, e := os.ReadFile(filepath.Join(dir, "cc-switch-integration.json"))
	if os.IsNotExist(e) {
		return out, nil, nil
	}
	if e != nil {
		return out, nil, e
	}
	e = json.Unmarshal(b, &out)
	return out, b, e
}
func writeSupplierData(dir, name string, b []byte) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".supplier-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(f.Name(), filepath.Join(dir, name))
	}
	return e
}
func importCCSwitchProfile(p supplierProfile) (supplierConfig, error) {
	q := supplierConfig{Name: p.Name, App: p.App, SourceID: p.ID, AuthType: "apiKey", ModelMappings: map[string]string{}, ModelNames: map[string]string{}, CodexOptions: map[string]any{}}
	unsupported := errors.New("此配置包含尚不支持的高级连接参数，暂不能完整导入")
	if p.App == "claude" {
		for k, v := range p.Settings.Env {
			if v == "" {
				continue
			}
			switch k {
			case "ANTHROPIC_BASE_URL":
				q.BaseURL = v
			case "ANTHROPIC_API_KEY":
				if q.Credential != "" {
					return q, unsupported
				}
				q.Credential = v
			case "ANTHROPIC_AUTH_TOKEN":
				if q.Credential != "" {
					return q, unsupported
				}
				q.Credential = v
				q.AuthType = "authToken"
			case "ANTHROPIC_MODEL":
				q.Model = v
			case "ANTHROPIC_DEFAULT_HAIKU_MODEL":
				q.ModelMappings["haiku"] = v
			case "ANTHROPIC_DEFAULT_SONNET_MODEL":
				q.ModelMappings["sonnet"] = v
			case "ANTHROPIC_DEFAULT_OPUS_MODEL":
				q.ModelMappings["opus"] = v
			case "ANTHROPIC_DEFAULT_FABLE_MODEL":
				q.ModelMappings["fable"] = v
			case "ANTHROPIC_DEFAULT_HAIKU_MODEL_NAME", "ANTHROPIC_DEFAULT_SONNET_MODEL_NAME", "ANTHROPIC_DEFAULT_OPUS_MODEL_NAME", "ANTHROPIC_DEFAULT_FABLE_MODEL_NAME":
				role := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(k, "ANTHROPIC_DEFAULT_"), "_MODEL_NAME"))
				q.ModelNames[role] = v
			case "API_TIMEOUT_MS":
				var e error
				q.TimeoutMs, e = strconv.ParseInt(v, 10, 64)
				if e != nil {
					return q, unsupported
				}
			default:
				return q, unsupported
			}
		}
	} else {
		q.Credential = p.Settings.Auth["OPENAI_API_KEY"]
		config := p.Settings.ConfigOverrides
		q.Model, _ = config["model"].(string)
		selected, _ := config["model_provider"].(string)
		providers, _ := config["model_providers"].(map[string]any)
		connection, _ := providers[selected].(map[string]any)
		q.BaseURL, _ = connection["base_url"].(string)
		for k, v := range connection {
			switch k {
			case "name", "base_url", "env_key", "requires_openai_auth":
			case "wire_api":
				if v != "responses" {
					return q, unsupported
				}
			default:
				return q, unsupported
			}
		}
		for k, v := range config {
			if k == "model" || k == "model_provider" || k == "model_providers" {
				continue
			}
			found := false
			for key, native := range codexSupplierKeys {
				if k == native {
					q.CodexOptions[key] = v
					found = true
					break
				}
			}
			if !found {
				return q, unsupported
			}
		}
	}
	if !validSupplierConfig(q) {
		return q, errors.New("供应商不符合本地配置要求，暂不能导入")
	}
	return q, nil
}
func (s *Server) ccSwitchImportAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		App      string `json:"app"`
		Revision string `json:"revision"`
	}
	if !decode(w, r, &req) {
		return
	}
	f, e := s.ccSwitchProfiles()
	if e != nil {
		fail(w, 400, "无法读取 CC-Switch 配置")
		return
	}
	var imported supplierConfig
	found := false
	for _, p := range f.Profiles {
		if p.ID == req.ID && p.App == req.App {
			imported, e = importCCSwitchProfile(p)
			found = true
			break
		}
	}
	if !found {
		fail(w, 404, "未找到可导入的 API 供应商")
		return
	}
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	supplierConfigsMu.Lock()
	defer supplierConfigsMu.Unlock()
	v, b, e := readSupplierConfigs(s.cfg.DataDir)
	if e != nil {
		fail(w, 500, "无法读取本地配置")
		return
	}
	if req.Revision != settingsRevision(b) {
		fail(w, 409, "配置已改变，请刷新后重试")
		return
	}
	for _, p := range v {
		if p.SourceID == req.ID && p.App == req.App {
			fail(w, 409, "此供应商已导入，可直接编辑本地副本")
			return
		}
	}
	imported.ID = localSupplierPrefix + store.ID()
	v = append(v, imported)
	next, _ := json.MarshalIndent(v, "", "  ")
	if writeSupplierData(s.cfg.DataDir, "supplier-configs.json", next) != nil {
		fail(w, 500, "无法保存本地副本")
		return
	}
	reply := safeSupplierConfigs(v, next)
	state, _, _ := readCCSwitchState(s.cfg.DataDir)
	reply.CCSwitchHidden = state.Hidden
	respond(w, 200, reply)
}
func (s *Server) ccSwitchVisibilityAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hidden   bool   `json:"hidden"`
		Revision string `json:"revision"`
	}
	if !decode(w, r, &req) {
		return
	}
	// Collect before locking local storage: catalog itself reads that storage.
	catalog, e := s.profileCatalog()
	if e != nil {
		fail(w, 500, "无法读取默认供应商")
		return
	}
	supplierConfigsMu.Lock()
	defer supplierConfigsMu.Unlock()
	v, b, e := readSupplierConfigs(s.cfg.DataDir)
	if e != nil {
		fail(w, 500, "无法读取本地配置")
		return
	}
	if req.Revision != settingsRevision(b) {
		fail(w, 409, "配置已改变，请刷新")
		return
	}
	state := ccSwitchState{Hidden: req.Hidden, Defaults: map[string]string{}}
	previous, _, stateErr := readCCSwitchState(s.cfg.DataDir)
	if stateErr != nil {
		fail(w, 500, "无法读取显示设置")
		return
	}
	if previous.Hidden && req.Hidden {
		state.Defaults = previous.Defaults
	}
	if req.Hidden {
		for app, id := range catalog.DefaultProfiles {
			if strings.HasPrefix(id, "ccswitch:") {
				mapped := ""
				for _, p := range v {
					if p.SourceID == id && p.App == app {
						mapped = p.ID
						break
					}
				}
				if mapped == "" {
					fail(w, 409, "请先导入当前默认供应商，或在应用配置中选择本地供应商/原生配置，再关闭 CC-Switch 列表")
					return
				}
				state.Defaults[app] = mapped
			}
		}
	}
	raw, _ := json.Marshal(state)
	if writeSupplierData(s.cfg.DataDir, "cc-switch-integration.json", raw) != nil {
		fail(w, 500, "无法保存显示设置")
		return
	}
	reply := safeSupplierConfigs(v, b)
	reply.CCSwitchHidden = req.Hidden
	respond(w, 200, reply)
}

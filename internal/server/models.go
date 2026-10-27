package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
	"cc-remote/internal/i18n"
	"cc-remote/internal/jsonedit"
	"cc-remote/internal/prefs"
)

type modelOption struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Source        string   `json:"source"`
	Description   string   `json:"description,omitempty"`
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"defaultEffort,omitempty"`
	IsDefault     bool     `json:"isDefault,omitempty"`
}

type modelList struct {
	Models     []modelOption `json:"models"`
	FetchedAt  string        `json:"fetchedAt"`
	Restricted bool          `json:"restricted"`
	Warnings   []string      `json:"warnings,omitempty"`
}

// modelsCache never holds mu while collecting; concurrent callers for the same
// key wait for the one in-flight collection instead.
type modelsCache struct {
	mu       sync.Mutex
	entries  map[string]cachedModels
	inflight map[string]*modelsCall
}

type modelsCall struct {
	done chan struct{}
	list modelList
}

type cachedModels struct {
	list modelList
	at   time.Time
}

const modelsTTL = 10 * time.Minute

// Aliases documented at https://code.claude.com/docs/en/model-config (checked against Claude Code 2.1.300).
func modelAliases(l i18n.Lang) []modelOption {
	return []modelOption{
		{ID: "default", Label: "default", Source: "alias", Description: l.T("Claude Code's default alias: the recommended model for your account type", "Claude Code 的 default 别名：按账户类型使用推荐模型")},
		{ID: "best", Label: "best", Source: "alias", Description: l.T("Fable when available, otherwise the same as opus", "可用时使用 Fable，否则与 opus 相同")},
		{ID: "fable", Label: "fable", Source: "alias", Description: l.T("The hardest, longest tasks", "最难、最长的任务")},
		{ID: "opus", Label: "opus", Source: "alias", Description: l.T("Latest Opus, complex reasoning", "最新 Opus，复杂推理")},
		{ID: "sonnet", Label: "sonnet", Source: "alias", Description: l.T("Latest Sonnet, everyday coding", "最新 Sonnet，日常编码")},
		{ID: "haiku", Label: "haiku", Source: "alias", Description: l.T("Fast and efficient for simple tasks", "快速高效的简单任务")},
		{ID: "opus[1m]", Label: "opus[1m]", Source: "alias", Description: l.T("Opus with a 1M-token context", "Opus 100 万 token 上下文")},
		{ID: "sonnet[1m]", Label: "sonnet[1m]", Source: "alias", Description: l.T("Sonnet with a 1M-token context", "Sonnet 100 万 token 上下文")},
		{ID: "opusplan", Label: "opusplan", Source: "alias", Description: l.T("opus in plan mode, sonnet when executing", "规划模式用 opus，执行时切换到 sonnet")},
	}
}

var modelEnvKeys = []string{"ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL"}

// managedSettingsPath is a variable so tests never read the machine's policy file.
var managedSettingsPath = func() string {
	switch runtime.GOOS {
	case "windows":
		return `C:\Program Files\ClaudeCode\managed-settings.json`
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	}
	return "/etc/claude-code/managed-settings.json"
}

type modelSettings struct {
	Model           string                     `json:"model"`
	AvailableModels *[]string                  `json:"availableModels"`
	Env             map[string]json.RawMessage `json:"env"`
}

func readModelSettings(l i18n.Lang, path string) (modelSettings, bool, error) {
	var v modelSettings
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return v, false, nil
	}
	if err != nil {
		return v, false, err
	}
	b = jsonedit.StripBOM(b)
	if len(strings.TrimSpace(string(b))) == 0 {
		return v, true, nil
	}
	if err = json.Unmarshal(b, &v); err != nil {
		return v, false, l.Errorf("%s is not valid JSON", "%s 不是有效 JSON", filepath.Base(path))
	}
	return v, true, nil
}

// envString reads one string value from a settings env block.
func (m modelSettings) envString(key string) string {
	var x string
	_ = json.Unmarshal(m.Env[key], &x)
	return x
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	provider, ok := requestProvider(w, r)
	if !ok {
		return
	}
	cwd := ""
	if id := r.URL.Query().Get("sessionId"); id != "" {
		x, err := s.store.Get(id)
		if err != nil {
			fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
			return
		}
		cwd = x.Cwd
		provider = x.Provider
	}
	respond(w, 200, s.providerModels(s.lang(r), provider, cwd, r.URL.Query().Get("refresh") == "1"))
}

// modelList must not be called with s.mu held: collection reads files and may call the API.
// Lists are cached per language, as their descriptions and warnings are text.
func (s *Server) modelList(l i18n.Lang, cwd string, refresh bool) modelList {
	return s.providerModels(l, "claude", cwd, refresh)
}

func (s *Server) providerModels(l i18n.Lang, provider, cwd string, refresh bool) modelList {
	key := provider + "|" + string(l) + "|" + cwd
	c := &s.modelCache
	c.mu.Lock()
	if c.entries == nil {
		c.entries, c.inflight = map[string]cachedModels{}, map[string]*modelsCall{}
	}
	if e, ok := c.entries[key]; ok && !refresh && time.Since(e.at) < modelsTTL {
		c.mu.Unlock()
		return e.list
	}
	if call, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.list
	}
	call := &modelsCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	var list modelList
	cacheable := true
	if provider == "codex" {
		list = s.collectCodexModels(l, cwd)
	} else {
		list, cacheable = s.collectModels(l, cwd)
	}
	call.list = list
	c.mu.Lock()
	delete(c.inflight, key)
	if cacheable {
		c.entries[key] = cachedModels{list, time.Now()}
	}
	c.mu.Unlock()
	close(call.done)
	return list
}

func (s *Server) collectCodexModels(l i18n.Lang, cwd string) modelList {
	list := modelList{Models: []modelOption{}, FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := codex.Models(ctx, s.cfg.CodexExecutable, s.cfg.CodexConfigDir, cwd)
	if err != nil {
		list.Warnings = []string{l.T("Could not read Codex models: ", "无法读取 Codex 模型：") + err.Error()}
		return list
	}
	for _, m := range models {
		if prefs.ValidModel(m.ID) && m.ID != "" {
			list.Models = append(list.Models, modelOption{ID: m.ID, Label: m.Label, Description: m.Description, Source: "codex", Efforts: m.Efforts, DefaultEffort: m.DefaultEffort, IsDefault: m.IsDefault})
		}
	}
	return list
}

// collectModels reports cacheable=false when the API request was cancelled, so
// the next request tries again. Timeouts are cached to avoid repeated 5 s waits.
func (s *Server) collectModels(lang i18n.Lang, cwd string) (modelList, bool) {
	result := modelList{Models: []modelOption{}, FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	seen := map[string]bool{}
	var allowed *[]string
	add := func(id, source, description string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || !prefs.ValidModel(id) {
			return
		}
		if allowed != nil && !modelAllowed(id, *allowed) {
			return
		}
		seen[id] = true
		result.Models = append(result.Models, modelOption{ID: id, Label: id, Source: source, Description: description})
	}

	// Managed policy wins outright; otherwise user/project/local lists are combined.
	managed, _, err := readModelSettings(lang, managedSettingsPath())
	if err != nil {
		result.Warnings = append(result.Warnings, lang.T("Could not read the organization's managed settings: ", "无法读取组织托管设置：")+err.Error())
	}
	paths := []string{filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")}
	if cwd != "" {
		paths = append(paths, filepath.Join(cwd, ".claude", "settings.json"), filepath.Join(cwd, ".claude", "settings.local.json"))
	}
	var layers []modelSettings
	var user modelSettings
	for i, path := range paths {
		v, ok, err := readModelSettings(lang, path)
		if err != nil {
			result.Warnings = append(result.Warnings, err.Error())
		}
		if ok {
			layers = append(layers, v)
		}
		if i == 0 {
			user = v
		}
	}
	if managed.AvailableModels != nil {
		allowed = managed.AvailableModels
	} else {
		for _, l := range layers {
			if l.AvailableModels != nil {
				merged := []string{}
				if allowed != nil {
					merged = append(merged, *allowed...)
				}
				merged = append(merged, *l.AvailableModels...)
				allowed = &merged
			}
		}
	}
	result.Restricted = allowed != nil

	if allowed != nil {
		// The Default option is always selectable; list entries are themselves choices.
		seen["default"] = true
		aliases := modelAliases(lang)
		result.Models = append(result.Models, aliases[0])
		for _, id := range *allowed {
			description := lang.T("Allowed by availableModels", "availableModels 允许")
			for _, a := range aliases {
				if a.ID == id {
					description = a.Description
				}
			}
			add(id, "settings", description)
		}
	} else {
		for _, a := range modelAliases(lang) {
			add(a.ID, a.Source, a.Description)
		}
	}
	for _, l := range append([]modelSettings{managed}, layers...) {
		add(l.Model, "settings", lang.T("model in settings.json", "settings.json 中的 model"))
		for _, key := range modelEnvKeys {
			add(l.envString(key), "settings", "settings env "+key)
		}
	}
	for _, key := range modelEnvKeys {
		add(os.Getenv(key), "env", lang.T("Environment variable ", "环境变量 ")+key)
	}
	for _, id := range claude.RecentModels(s.cfg.ClaudeConfigDir, 200) {
		if len(result.Models) > 200 {
			break
		}
		add(id, "recent", lang.T("Recently used by native sessions on this host", "本机原生会话最近使用"))
	}
	api, attempted, err := fetchAPIModels(lang, user)
	cacheable := true
	if err != nil {
		result.Warnings = append(result.Warnings, lang.T("Could not fetch the model list from the API: ", "无法从 API 获取模型列表：")+err.Error())
		cacheable = !errors.Is(err, context.Canceled)
	} else if attempted {
		for _, m := range api {
			add(m.ID, "api", m.Description)
		}
	}
	return result, cacheable
}

// modelAllowed approximates Claude Code's availableModels matching: exact ID,
// version prefix (claude-sonnet-4-5 allows claude-sonnet-4-5-20250929) or family alias.
func modelAllowed(id string, list []string) bool {
	if id == "default" {
		return true
	}
	for _, e := range list {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if id == e || strings.HasPrefix(id, e+"-") || strings.HasPrefix(id, e+"[") {
			return true
		}
		switch e {
		case "opus", "sonnet", "haiku", "fable":
			if strings.HasPrefix(id, "claude-"+e+"-") || strings.Contains(id, "-"+e+"-") {
				return true
			}
		}
	}
	return false
}

const modelsAPITimeout = 5 * time.Second

// modelsClient never follows redirects, so credentials are only sent to the configured host.
var modelsClient = &http.Client{
	Timeout:       modelsAPITimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// modelsBaseURL validates ANTHROPIC_BASE_URL: https, or plain http only to a loopback host.
func modelsBaseURL(l i18n.Lang, raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "https://api.anthropic.com", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", l.Errorf("ANTHROPIC_BASE_URL is invalid", "ANTHROPIC_BASE_URL 无效")
	}
	switch u.Scheme {
	case "https":
		return raw, nil
	case "http":
		host := u.Hostname()
		if ip := net.ParseIP(host); strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback()) {
			return raw, nil
		}
		return "", l.Errorf("ANTHROPIC_BASE_URL must use https (except for local addresses); no credentials were sent", "ANTHROPIC_BASE_URL 必须使用 https（本机地址除外），未发送凭据")
	}
	return "", l.Errorf("ANTHROPIC_BASE_URL is invalid", "ANTHROPIC_BASE_URL 无效")
}

// apiCredentials takes the key and base URL from one layer only: the host process
// environment, or else the user settings env block. A key from one source is never
// sent to a base URL from another.
func apiCredentials(user modelSettings) (apiKey, authToken, base string) {
	apiKey, authToken = strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")), strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN"))
	if apiKey != "" || authToken != "" {
		return apiKey, authToken, os.Getenv("ANTHROPIC_BASE_URL")
	}
	return strings.TrimSpace(user.envString("ANTHROPIC_API_KEY")), strings.TrimSpace(user.envString("ANTHROPIC_AUTH_TOKEN")), user.envString("ANTHROPIC_BASE_URL")
}

// fetchAPIModels only runs when an API credential is configured; OAuth logins have none.
// It uses its own timeout, independent of the browser request that triggered it,
// because the result is cached and shared.
func fetchAPIModels(l i18n.Lang, user modelSettings) ([]modelOption, bool, error) {
	apiKey, authToken, rawBase := apiCredentials(user)
	if apiKey == "" && authToken == "" {
		return nil, false, nil
	}
	base, err := modelsBaseURL(l, rawBase)
	if err != nil {
		return nil, true, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), modelsAPITimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models?limit=1000", nil)
	if err != nil {
		return nil, true, l.Errorf("ANTHROPIC_BASE_URL is invalid", "ANTHROPIC_BASE_URL 无效")
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	resp, err := modelsClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, true, l.Errorf("request timed out: %w", "请求超时：%w", ctx.Err())
		}
		return nil, true, l.Errorf("request failed", "请求失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, true, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, true, l.Errorf("the response is not valid JSON", "响应不是有效 JSON")
	}
	out := make([]modelOption, 0, len(body.Data))
	for _, m := range body.Data {
		out = append(out, modelOption{ID: m.ID, Label: m.ID, Source: "api", Description: m.DisplayName})
	}
	return out, true, nil
}

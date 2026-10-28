package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

type supplierProfile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	App      string `json:"app"`
	Settings struct {
		Env             map[string]string `json:"env,omitempty"`
		Auth            map[string]string `json:"auth,omitempty"`
		ConfigOverrides map[string]any    `json:"configOverrides,omitempty"`
	} `json:"settings"`
}
type profileFile struct {
	Version         int                  `json:"version"`
	DefaultProfiles map[string]string    `json:"defaultProfiles"`
	Profiles        []supplierProfile    `json:"profiles"`
	Source          string               `json:"source"`
	Found           bool                 `json:"found"`
	Unavailable     []unavailableProfile `json:"unavailable"`
}

func (p supplierProfile) snapshot() string {
	if p.ID == "" {
		return ""
	}
	b, _ := json.Marshal(p)
	return string(b)
}
func (s *Server) profileCatalog() (profileFile, error) {
	return s.ccSwitchProfiles()
}
func (p supplierProfile) validate() error {
	if p.App == "claude" {
		for k := range p.Settings.Env {
			if !(strings.HasPrefix(k, "ANTHROPIC_") || k == "API_TIMEOUT_MS" || strings.HasPrefix(k, "CLAUDE_CODE_") && strings.Contains(k, "MODEL")) {
				return errors.New("供应商环境变量不受支持")
			}
		}
		if p.Settings.Env["ANTHROPIC_BASE_URL"] == "" || (p.Settings.Env["ANTHROPIC_API_KEY"] == "" && p.Settings.Env["ANTHROPIC_AUTH_TOKEN"] == "") {
			return errors.New("Claude 供应商需要端点和 API 凭据")
		}
	} else if p.App == "codex" {
		if p.Settings.Auth["OPENAI_API_KEY"] == "" || len(p.Settings.ConfigOverrides) == 0 {
			return errors.New("Codex 供应商需要 API Key 和 configOverrides")
		}
		for k := range p.Settings.ConfigOverrides {
			if k != "model" && k != "model_provider" && k != "model_providers" && k != "model_reasoning_effort" && k != "model_reasoning_summary" && k != "model_verbosity" && k != "model_context_window" && k != "model_auto_compact_token_limit" && k != "disable_response_storage" {
				return errors.New("Codex 配置含非连接字段")
			}
		}
		if e := validateCodexProviders(p.Settings.ConfigOverrides); e != nil {
			return e
		}
		if _, ok := p.Settings.ConfigOverrides["model_provider"].(string); !ok {
			return errors.New("Codex 供应商缺少 model_provider")
		}
	} else {
		return errors.New("不支持的供应商应用")
	}
	return nil
}
func (s *Server) selectProfile(app, id string) (supplierProfile, error) {
	if id == "@native" {
		return supplierProfile{}, nil
	}
	f, e := s.profileCatalog()
	if e != nil {
		return supplierProfile{}, e
	}
	if id == "" {
		id = f.DefaultProfiles[app]
	}
	if id == "" {
		return supplierProfile{}, nil
	}
	for _, p := range f.Profiles {
		if (p.ID == id || strings.TrimPrefix(p.ID, "ccswitch:") == id) && p.App == app {
			return p, nil
		}
	}
	return supplierProfile{}, errors.New("供应商不存在")
}
func (s *Server) profilesAPI(w http.ResponseWriter, r *http.Request) {
	f, e := s.profileCatalog()
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	items := []map[string]string{}
	for _, p := range f.Profiles {
		items = append(items, map[string]string{"id": p.ID, "name": p.Name, "app": p.App})
	}
	respond(w, 200, map[string]any{"schemaVersion": 1, "capabilities": map[string]bool{"resumeSelection": true, "switchSupplier": true}, "profiles": items, "defaultProfiles": f.DefaultProfiles, "source": f.Source, "found": f.Found, "unavailable": f.Unavailable})
}
// supplierEnvAPI returns one supplier's connection variables so the terminal
// can launch native Claude with it (GET /api/profiles/{id}/env). The catalog
// endpoint deliberately omits them; this one hands them only to a caller that
// already holds the access token.
func (s *Server) supplierEnvAPI(w http.ResponseWriter, r *http.Request) {
	f, e := s.profileCatalog()
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	id := r.PathValue("id")
	for _, p := range f.Profiles {
		if p.ID == id && p.App == "claude" {
			respond(w, 200, map[string]any{"id": p.ID, "name": p.Name, "env": p.Settings.Env})
			return
		}
	}
	fail(w, 404, s.lang(r).T("Supplier configuration not found", "未找到该供应商配置"))
}

func sessionProfile(x store.Session) (supplierProfile, error) {
	var p supplierProfile
	if x.ProfileJSON == "" {
		if x.ProfileID != "" {
			return p, errors.New("会话供应商配置缺失")
		}
		return p, nil
	}
	if json.Unmarshal([]byte(x.ProfileJSON), &p) != nil || p.ID != x.ProfileID || p.App != x.Provider {
		return p, errors.New("会话供应商配置无效")
	}
	return p, p.validate()
}
func profileOptions(x store.Session, o *claude.Options) error {
	p, e := sessionProfile(x)
	if e != nil {
		return e
	}
	if p.ID == "" {
		return nil
	}
	if p.App == "claude" {
		o.ProfileEnv = claude.SupplierEnv(p.Settings.Env)
		if x.Model == "" {
			o.Model = ""
		}
	} else {
		o.ProfileEnv = map[string]string{"OPENAI_API_KEY": p.Settings.Auth["OPENAI_API_KEY"]}
		o.ConfigOverrides = p.Settings.ConfigOverrides
		o.ModelProvider, _ = p.Settings.ConfigOverrides["model_provider"].(string)
		if x.Model == "" {
			o.Model, _ = p.Settings.ConfigOverrides["model"].(string)
		}
	}
	return nil
}

// Switching detaches the old process before storing a new launch snapshot. It
// never replays pending messages; the next send resumes the native conversation.
func (s *Server) switchProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProfileID string `json:"profileId"`
		Force     bool   `json:"force"`
	}
	if !decode(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	s.configMu.Lock()
	defer s.configMu.Unlock()
	s.mu.Lock()
	x, e := s.store.Get(id)
	if e != nil {
		s.mu.Unlock()
		fail(w, 404, "会话不存在")
		return
	}
	if s.switching[id] {
		s.mu.Unlock()
		fail(w, 409, "正在切换配置")
		return
	}
	p, e := s.selectProfile(x.Provider, req.ProfileID)
	if e != nil {
		s.mu.Unlock()
		fail(w, 400, e.Error())
		return
	}
	if x.ProfileID == p.ID && x.ProfileJSON == p.snapshot() {
		s.decorate(&x)
		s.mu.Unlock()
		respond(w, 200, x)
		return
	}
	rt := s.procs[id]
	ts := s.terminals[id]
	oldRun := s.runs[id]
	busy := x.Status == "running" || x.Status == "waiting" || s.runs[id] != nil
	if rt != nil {
		busy = busy || len(rt.sent) > 0 || rt.approvals > 0 || rt.proc != nil && rt.proc.BackgroundTasks() > 0
	}
	if ts != nil {
		busy = true
	} // interactive Claude may have work absent from transcripts
	if busy && !req.Force {
		s.mu.Unlock()
		fail(w, 409, "切换将中断当前任务及待处理请求，保留上下文；不会自动重发。确认继续？")
		return
	}
	s.switching[id] = true
	if rt != nil {
		s.detachLocked(id)
	}
	if ts != nil {
		ts.closing = true
	} // leave registered until terminalExited completes cleanup
	s.mu.Unlock()

	stopped := make(chan struct{})
	s.closers.Add(1)
	go func() {
		defer s.closers.Done()
		defer close(stopped)
		closeRuntime(rt)
		if rt != nil && rt.proc != nil {
			<-rt.proc.Done()
		}
		if oldRun != nil {
			<-oldRun.done
		}
		if ts != nil {
			ts.runner.Close(terminalCloseWait)
			<-ts.cleaned
		}
	}()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-stopped:
	case <-timer.C:
		// Until cleanup completes, do not permit another process for this ID.
		go func() { <-stopped; s.mu.Lock(); delete(s.switching, id); s.mu.Unlock() }()
		fail(w, 504, "旧会话进程尚未停止，未切换配置；清理完成后可重试")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.switching, id)
	// Terminal cleanup may have marked the prior turn interrupted; preserve ID/history.
	if e = s.store.SetProfile(id, p.ID, p.snapshot()); e != nil {
		fail(w, 500, "无法保存会话配置")
		return
	}
	_, _ = s.store.Append(id, "status", fmt.Sprintf("供应商配置已切换：%s。下次发送时恢复会话；未自动重发请求。", p.Name), nil)
	x, _ = s.store.Get(id)
	s.decorate(&x)
	respond(w, 200, x)
}

func validateCodexProviders(config map[string]any) error {
	selected, ok := config["model_provider"].(string)
	if !ok || selected == "" {
		return errors.New("Codex 缺少自定义供应商选择")
	}
	providers, ok := config["model_providers"].(map[string]any)
	if !ok {
		return errors.New("Codex 缺少自定义供应商定义")
	}
	for _, raw := range providers {
		p, ok := raw.(map[string]any)
		if !ok {
			return errors.New("Codex 供应商定义无效")
		}
		for k, v := range p {
			switch k {
			case "name", "base_url", "env_key", "wire_api":
				if _, ok := v.(string); !ok {
					return errors.New("Codex 连接字段必须是字符串")
				}
			case "requires_openai_auth":
				if _, ok := v.(bool); !ok {
					return errors.New("Codex 认证字段无效")
				}
			case "http_headers", "query_params":
				m, ok := v.(map[string]any)
				if !ok || len(m) > 0 {
					return errors.New("静态认证头或参数不能放入启动配置")
				}
			case "env_http_headers":
				m, ok := v.(map[string]any)
				if !ok {
					return errors.New("Codex 环境头字段无效")
				}
				for header, value := range m {
					if strings.EqualFold(header, "Authorization") {
						return errors.New("Authorization 必须使用供应商 API Key")
					}
					if _, ok := value.(string); !ok {
						return errors.New("Codex 环境头字段无效")
					}
				}
			case "request_max_retries", "stream_max_retries", "stream_idle_timeout_ms":
				if _, ok := v.(float64); !ok {
					return errors.New("Codex 重试字段必须是数字")
				}
			default:
				return errors.New("Codex 供应商含不支持的连接字段")
			}
		}
	}
	p, ok := providers[selected].(map[string]any)
	if !ok || p["env_key"] != "OPENAI_API_KEY" || p["requires_openai_auth"] == true {
		return errors.New("Codex API Key 供应商需使用 OPENAI_API_KEY，且不能依赖官方登录")
	}
	return nil
}

func profileValue(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

func (s *Server) resumeProfile(app string, id *string) (*supplierProfile, error) {
	if id == nil {
		return nil, nil
	}
	p, err := s.selectProfile(app, *id)
	return &p, err
}

// Caller holds s.mu. A running session must use the explicit switch flow.
func (s *Server) applyResumeProfile(x *store.Session, p *supplierProfile) error {
	if p == nil || x.ProfileID == p.ID && x.ProfileJSON == p.snapshot() {
		return nil
	}
	busy := s.switching[x.ID] || s.runs[x.ID] != nil || s.terminals[x.ID] != nil || x.Status == "running" || x.Status == "waiting"
	if rt := s.procs[x.ID]; rt != nil {
		busy = busy || len(rt.sent) > 0 || rt.approvals > 0 || rt.proc != nil && rt.proc.BackgroundTasks() > 0
	}
	if busy {
		return errors.New("会话正在运行，请先停止会话或使用供应商切换入口")
	}
	if err := s.store.SetProfile(x.ID, p.ID, p.snapshot()); err != nil {
		return errors.New("无法保存供应商配置")
	}
	x.ProfileID, x.ProfileJSON = p.ID, p.snapshot()
	x.ProfileName = p.Name
	return nil
}

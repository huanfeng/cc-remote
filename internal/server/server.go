package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

type Runner func(context.Context, claude.Options, func(claude.Event) error) error
type Config struct {
	UploadDir                                                                                 string
	Token, ClaudeExecutable, HostExecutable, Cwd, ClaudeConfigDir, BridgeURL, TerminalCommand string
	Assets                                                                                    fs.FS
	Runner                                                                                    Runner
	// Host metadata shared by status, preferences and service management.
	DataDir, Listen, Version, LogFile string
	StartedAt                         time.Time
	// Shutdown asks the host process to exit gracefully; nil disables remote shutdown.
	Shutdown func()
}
type execution struct {
	cancel    context.CancelFunc
	ctx       context.Context
	sessionID string
	token     string
	done      chan struct{}
}
type answer struct {
	// allow / deny; "terminal" (shared terminal only) hands the prompt back to the native dialog.
	Decision string            `json:"decision"`
	Answers  map[string]string `json:"answers,omitempty"`
	// Shared terminal ExitPlanMode only: permission mode after approval, feedback on rejection.
	Mode    string `json:"mode,omitempty"`
	Message string `json:"message,omitempty"`
}
type pending struct {
	sessionID string
	ctx       context.Context
	channel   chan answer
	// Set for a shared terminal question held by its PermissionRequest hook.
	connection *channelConnection
	toolName   string
	input      json.RawMessage
}
type Server struct {
	store           *store.Store
	cfg             Config
	mu              sync.Mutex
	runs            map[string]*execution
	requests        map[string]*pending
	closing         bool
	channels        map[string]*channelConnection
	channelRequests map[string]*channelApproval
	modelCache      modelsCache
	statusCache     statusCache
	// configMu serializes writers of preferences.json, Claude settings.json and
	// session overrides, keeping their file I/O off s.mu. Order: configMu, then s.mu.
	configMu sync.Mutex
}

func New(st *store.Store, cfg Config) *Server {
	if cfg.Runner == nil {
		cfg.Runner = claude.Run
	}
	return &Server{store: st, cfg: cfg, runs: map[string]*execution{}, requests: map[string]*pending{}, channels: map[string]*channelConnection{}, channelRequests: map[string]*channelApproval{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/bridge/request", s.bridge)
	mux.HandleFunc("POST /api/channels/register", s.channelRegister)
	mux.HandleFunc("GET /api/channels/{id}/commands", s.channelCommands)
	mux.HandleFunc("POST /api/channels/{id}/heartbeat", s.channelHeartbeat)
	mux.HandleFunc("POST /api/channels/{id}/disconnect", s.channelDisconnect)
	mux.HandleFunc("POST /api/channels/{id}/events", s.channelEvent)
	mux.HandleFunc("POST /api/channels/{id}/interactions", s.channelInteraction)
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/sessions/{id}/stats", s.sessionStats)
	mux.HandleFunc("POST /api/admin/shutdown", s.shutdown)
	mux.HandleFunc("GET /api/claude/settings", s.settings)
	mux.HandleFunc("PUT /api/claude/settings", s.settings)
	mux.HandleFunc("GET /api/preferences", s.preferences)
	mux.HandleFunc("PUT /api/preferences", s.preferences)
	mux.HandleFunc("GET /api/models", s.models)
	mux.HandleFunc("PATCH /api/sessions/{id}", s.patchSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.deleteSession)
	mux.HandleFunc("GET /api/sessions", s.list)
	mux.HandleFunc("POST /api/sessions", s.create)
	mux.HandleFunc("GET /api/sessions/{id}", s.get)
	mux.HandleFunc("GET /api/sessions/{id}/events", s.events)
	mux.HandleFunc("POST /api/sessions/{id}/messages", s.message)
	mux.HandleFunc("POST /api/sessions/{id}/uploads", s.upload)
	mux.HandleFunc("GET /api/sessions/{id}/uploads/{uploadID}", s.uploadGet)
	mux.HandleFunc("GET /api/sessions/{id}/commands", s.commands)
	mux.HandleFunc("POST /api/sessions/{id}/commands", s.commands)
	mux.HandleFunc("POST /api/sessions/{id}/stop", s.stop)
	mux.HandleFunc("GET /api/requests", s.pending)
	mux.HandleFunc("POST /api/requests/{id}", s.resolve)
	mux.HandleFunc("GET /api/native/sessions", s.nativeList)
	mux.HandleFunc("GET /api/native/sessions/{id}", s.nativeGet)
	mux.HandleFunc("POST /api/native/sessions/{id}/import", s.nativeImport)
	mux.HandleFunc("/", s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'self'; object-src 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if r.URL.Path != "/api/bridge/request" && !equalToken(r.Header.Get("Authorization"), s.cfg.Token) {
				fail(w, 401, "访问凭证无效")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r) {
				fail(w, 403, "不允许跨站访问")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether the browser Origin names this host. Behind a
// reverse proxy that rewrites Host to the upstream address, the public host
// arrives in X-Forwarded-Host instead. Trusting that header is safe here: a
// cross-site page cannot set it (or the token's Authorization header) without
// a CORS preflight, which the host never grants.
func sameOrigin(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	fwd, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ",")
	fwd = strings.TrimSpace(fwd)
	if fwd == "" {
		return false
	}
	if strings.EqualFold(u.Host, fwd) {
		return true
	}
	// Some proxies send the host without the default port of the scheme.
	return u.Port() == "" && strings.EqualFold(u.Hostname(), strings.TrimSuffix(strings.TrimSuffix(fwd, ":443"), ":80"))
}

func equalToken(header, token string) bool {
	return token != "" && subtle.ConstantTimeCompare([]byte(header), []byte("Bearer "+token)) == 1
}
func respond(w http.ResponseWriter, status int, x any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(x); err != nil {
		log.Printf("response: %v", err)
	}
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "无效的请求内容")
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		fail(w, 400, "只允许一个完整 JSON 对象")
		return false
	}
	return true
}
func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	path, err := exec.LookPath(s.cfg.ClaudeExecutable)
	version := ""
	if err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		b, e := exec.CommandContext(ctx, path, "--version").Output()
		if e == nil {
			version = strings.TrimSpace(string(b))
		}
	}
	respond(w, 200, map[string]any{"version": s.cfg.Version, "cwd": s.cfg.Cwd, "claudeAvailable": err == nil, "claudeVersion": version, "terminalCommand": s.cfg.TerminalCommand, "channelExperimental": true})
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	x, e := s.store.List()
	if e != nil {
		fail(w, 500, "无法读取会话")
		return
	}
	s.mu.Lock()
	for i := range x {
		s.decorateChannel(&x[i])
	}
	s.mu.Unlock()
	respond(w, 200, x)
}
func validateCwd(cwd string) (string, error) {
	if !filepath.IsAbs(cwd) {
		return "", errors.New("请输入宿主上的绝对目录路径")
	}
	p, e := filepath.Abs(cwd)
	if e != nil {
		return "", e
	}
	f, e := os.Stat(p)
	if e != nil || !f.IsDir() {
		return "", errors.New("工作目录不存在或不是目录")
	}
	return p, nil
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Cwd   string `json:"cwd"`
		overrideRequest
	}
	if !decode(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		req.Title = "新会话"
	}
	if len(req.Title) > 300 {
		fail(w, 400, "会话名称过长")
		return
	}
	cwd, e := validateCwd(req.Cwd)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	var overrides store.Session
	if e = req.apply(&overrides); e != nil {
		fail(w, 400, e.Error())
		return
	}
	x, e := s.store.Create(req.Title, cwd, store.ID(), false)
	if e == nil {
		e = s.store.SetOverrides(x.ID, overrides.PermissionMode, overrides.Model, overrides.Effort)
		x.PermissionMode, x.Model, x.Effort = overrides.PermissionMode, overrides.Model, overrides.Effort
	}
	if e != nil {
		fail(w, 500, "无法创建会话")
		return
	}
	respond(w, 201, x)
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	x, e := s.store.Get(r.PathValue("id"))
	if e != nil {
		fail(w, 404, "会话不存在")
		return
	}
	events, e := s.store.Events(x.ID, 0)
	if e != nil {
		fail(w, 500, "无法读取消息")
		return
	}
	s.mu.Lock()
	s.decorateChannel(&x)
	s.mu.Unlock()
	respond(w, 200, map[string]any{"session": x, "events": events})
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, e := s.store.Get(id); e != nil {
		fail(w, 404, "会话不存在")
		return
	}
	after := int64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || n < 0 {
			fail(w, 400, "无效的事件游标")
			return
		}
		after = n
	}
	x, e := s.store.Events(id, after)
	if e != nil {
		fail(w, 500, "无法读取消息")
		return
	}
	respond(w, 200, x)
}

func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text          string   `json:"text"`
		AttachmentIDs []string `json:"attachmentIds"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if (req.Text == "" && len(req.AttachmentIDs) == 0) || len(req.Text) > 1<<20 {
		fail(w, 400, "消息为空或超过 1 MiB")
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		fail(w, 503, "宿主正在关闭")
		return
	}
	if _, ok := s.runs[id]; ok {
		s.mu.Unlock()
		fail(w, 409, "此会话仍在运行，请等待或停止当前轮次")
		return
	}
	x, e := s.store.Get(id)
	if e != nil {
		s.mu.Unlock()
		fail(w, 404, "会话不存在")
		return
	}
	promptText := req.Text
	if strings.HasPrefix(req.Text, "/") {
		name, args, _ := strings.Cut(strings.TrimPrefix(req.Text, "/"), " ")
		var found bool
		promptText, found, e = s.skillPrompt(x.Cwd, name, strings.TrimSpace(args))
		if e != nil || !found {
			s.mu.Unlock()
			message := "请通过命令入口执行内置 / 命令"
			if e != nil {
				message = e.Error()
			}
			fail(w, 400, message)
			return
		}
	}
	prompt, payload, e := s.messageAttachments(x.ID, promptText, req.AttachmentIDs)
	if e != nil {
		s.mu.Unlock()
		fail(w, 400, e.Error())
		return
	}
	if x.Mode == "channel" {
		s.channelMessageLocked(w, x, req.Text, prompt, payload)
		s.mu.Unlock()
		return
	}
	if _, e = validateCwd(x.Cwd); e != nil {
		s.mu.Unlock()
		fail(w, 400, e.Error())
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &execution{cancel: cancel, ctx: ctx, sessionID: id, token: store.ID() + store.ID(), done: make(chan struct{})}
	if _, e = s.store.Append(id, "user", req.Text, payload); e == nil {
		e = s.store.SetStatus(id, "running")
	}
	if e != nil {
		cancel()
		s.mu.Unlock()
		fail(w, 500, "无法保存消息")
		return
	}
	s.runs[id] = run
	s.mu.Unlock()
	go s.execute(x, prompt, run)
	respond(w, 202, map[string]string{"status": "running"})
}

func (s *Server) execute(x store.Session, text string, run *execution) {
	defer close(run.done)
	defer run.cancel()
	opts := claude.Options{Executable: s.cfg.ClaudeExecutable, HostExecutable: s.cfg.HostExecutable, SessionID: x.ClaudeSessionID, Cwd: x.Cwd, Message: text, Resume: x.Resume, BridgeURL: s.cfg.BridgeURL, BridgeToken: run.token, ConfigDir: s.cfg.ClaudeConfigDir}
	p := s.effectivePreferences(x)
	opts.PermissionMode, opts.Model, opts.Effort = p.PermissionMode, p.Model, p.Effort
	err := s.cfg.Runner(run.ctx, opts, func(ev claude.Event) error {
		if ev.Kind == "session" || ev.Kind == "result" {
			if e := s.store.SetResume(x.ID); e != nil {
				return e
			}
		}
		_, e := s.store.Append(x.ID, ev.Kind, ev.Text, ev.Payload)
		return e
	})
	status := "completed"
	if err != nil {
		status = "failed"
		if run.ctx.Err() != nil {
			status = "interrupted"
		}
		if _, e := s.store.Append(x.ID, "error", err.Error(), nil); e != nil {
			log.Printf("save execution error: %v", e)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.requests {
		if p.sessionID == x.ID {
			if e := s.store.Resolve(id, "expired"); e != nil {
				log.Printf("expire: %v", e)
			}
			delete(s.requests, id)
		}
	}
	if e := s.store.SetStatus(x.ID, status); e != nil {
		log.Printf("save status: %v", e)
	}
	if _, e := s.store.Append(x.ID, "status", status, nil); e != nil {
		log.Printf("save status event: %v", e)
	}
	delete(s.runs, x.ID)
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	run, ok := s.runs[r.PathValue("id")]
	if ok {
		run.cancel()
	}
	s.mu.Unlock()
	if !ok {
		fail(w, 409, "会话没有正在运行的轮次")
		return
	}
	respond(w, 200, map[string]string{"status": "stopping"})
}

const deleteStopTimeout = 15 * time.Second

// deleteSession removes cc-remote's records of a session (events, interactions,
// transcript keys, uploads). Claude's native JSONL history is never touched.
// With ?stop=1 an active managed run is stopped first.
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	x, err := s.store.Get(id)
	if err != nil {
		s.mu.Unlock()
		fail(w, 404, "会话不存在")
		return
	}
	s.decorateChannel(&x)
	run := s.runs[id]
	if x.Online {
		s.mu.Unlock()
		fail(w, 409, "共享终端在线，请先退出终端")
		return
	}
	if run != nil {
		if r.URL.Query().Get("stop") != "1" {
			s.mu.Unlock()
			fail(w, 409, "请先停止执行")
			return
		}
		run.cancel()
	}
	s.mu.Unlock()
	if run != nil {
		timer := time.NewTimer(deleteStopTimeout)
		defer timer.Stop()
		select {
		case <-run.done:
		case <-timer.C:
			fail(w, 504, "停止执行超时，请稍后重试")
			return
		case <-r.Context().Done():
			return
		}
	}
	s.mu.Lock()
	x.Online = false
	s.decorateChannel(&x)
	if s.runs[id] != nil || x.Online {
		s.mu.Unlock()
		fail(w, 409, "会话已重新开始执行或终端已连接，请重试")
		return
	}
	for rid, p := range s.requests {
		if p.sessionID == id {
			delete(s.requests, rid)
			if p.connection != nil {
				// Release the waiting hook; the native dialog keeps the question.
				select {
				case p.channel <- answer{Decision: "terminal"}:
				default:
				}
			}
		}
	}
	for rid, p := range s.channelRequests {
		if p.connection.sessionID == id {
			delete(s.channelRequests, rid)
		}
	}
	for cid, c := range s.channels {
		if c.sessionID == id {
			delete(s.channels, cid)
		}
	}
	err = s.store.Delete(id)
	s.mu.Unlock()
	if err != nil {
		fail(w, 500, "无法删除会话")
		return
	}
	if dir, e := s.sessionUploadDir(id); e == nil {
		if e = os.RemoveAll(dir); e != nil {
			log.Printf("delete uploads %s: %v", id, e)
		}
	}
	s.statusCache.mu.Lock()
	s.statusCache.at = time.Time{}
	s.statusCache.mu.Unlock()
	respond(w, 200, map[string]string{"status": "deleted"})
}

func (s *Server) bridge(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		fail(w, 403, "仅允许本机桥接")
		return
	}
	s.mu.Lock()
	var run *execution
	for _, x := range s.runs {
		if equalToken(r.Header.Get("Authorization"), x.token) {
			run = x
			break
		}
	}
	s.mu.Unlock()
	if run == nil {
		fail(w, 401, "执行凭证无效或已失效")
		return
	}
	var req struct {
		Kind     string          `json:"kind"`
		ToolName string          `json:"toolName"`
		Input    json.RawMessage `json:"input"`
	}
	if !decode(w, r, &req) {
		return
	}
	if (req.Kind != "approval" && req.Kind != "question" && req.Kind != "plan") || !json.Valid(req.Input) {
		fail(w, 400, "无效的交互类型")
		return
	}
	x := store.Interaction{ID: store.ID(), SessionID: run.sessionID, Kind: req.Kind, ToolName: req.ToolName, Input: req.Input}
	p := &pending{sessionID: run.sessionID, ctx: run.ctx, channel: make(chan answer, 1)}
	s.mu.Lock()
	if run.ctx.Err() != nil || s.runs[run.sessionID] != run {
		s.mu.Unlock()
		fail(w, 409, "执行已停止")
		return
	}
	if err := s.store.AddInteraction(x); err != nil {
		s.mu.Unlock()
		fail(w, 500, "无法保存交互请求")
		return
	}
	s.requests[x.ID] = p
	if err := s.store.SetStatus(run.sessionID, "waiting"); err != nil {
		log.Printf("waiting status: %v", err)
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, exists := s.requests[x.ID]; exists {
			delete(s.requests, x.ID)
			if e := s.store.Resolve(x.ID, "expired"); e != nil {
				log.Printf("expire: %v", e)
			}
		}
		hasPending := false
		for _, p := range s.requests {
			if p.sessionID == run.sessionID {
				hasPending = true
			}
		}
		if current := s.runs[run.sessionID]; current == run && !hasPending && run.ctx.Err() == nil {
			if e := s.store.SetStatus(run.sessionID, "running"); e != nil {
				log.Printf("running status: %v", e)
			}
		}
	}()
	timer := time.NewTimer(30 * time.Minute)
	defer timer.Stop()
	select {
	case a := <-p.channel:
		respond(w, 200, a)
	case <-run.ctx.Done():
		fail(w, 409, "执行已停止")
	case <-r.Context().Done():
		return
	case <-timer.C:
		respond(w, 200, answer{Decision: "deny"})
	}
}

func (s *Server) pending(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	for id, p := range s.channelRequests {
		if !s.channelLive(p.connection) || time.Since(p.created) > 30*time.Minute {
			_ = s.store.Resolve(id, "expired")
			delete(s.channelRequests, id)
		}
	}
	s.mu.Unlock()
	x, e := s.store.Pending(r.URL.Query().Get("sessionId"))
	if e != nil {
		fail(w, 500, "无法读取交互请求")
		return
	}
	respond(w, 200, x)
}
func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	var a answer
	if !decode(w, r, &a) {
		return
	}
	if a.Decision != "allow" && a.Decision != "deny" && a.Decision != "terminal" {
		fail(w, 400, "无效的审批结果")
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.channelRequests[id]; ok {
		if a.Decision == "terminal" {
			fail(w, 400, "无效的审批结果")
			return
		}
		s.channelResolveLocked(w, id, p, a)
		return
	}
	p, ok := s.requests[id]
	if !ok || p.ctx.Err() != nil {
		fail(w, 409, "请求已处理或已失效")
		return
	}
	status := "resolved"
	if p.connection != nil {
		if msg := checkSharedAnswer(p, &a); msg != "" {
			fail(w, 400, msg)
			return
		}
		if a.Decision == "terminal" {
			status = "terminal"
		}
	} else if a.Decision == "terminal" || a.Mode != "" || a.Message != "" {
		fail(w, 400, "无效的审批结果")
		return
	}
	if err := s.store.Resolve(id, status); err != nil {
		fail(w, 409, err.Error())
		return
	}
	delete(s.requests, id)
	if p.connection != nil {
		outcome := "web"
		if status == "terminal" {
			outcome = "handoff"
		}
		s.interactionEventLocked(id, p, outcome, a)
	}
	p.channel <- a
	respond(w, 200, map[string]string{"status": status})
}

func (s *Server) nativeList(w http.ResponseWriter, r *http.Request) {
	x, e := claude.ListNative(s.cfg.ClaudeConfigDir)
	if e != nil {
		fail(w, 500, "无法读取 Claude Code 原生会话："+e.Error())
		return
	}
	respond(w, 200, x)
}
func (s *Server) nativeGet(w http.ResponseWriter, r *http.Request) {
	x, ev, e := claude.NativeHistory(s.cfg.ClaudeConfigDir, r.PathValue("id"))
	if e != nil {
		fail(w, 404, "无法读取原生会话："+e.Error())
		return
	}
	out := []store.Event{}
	for i, v := range ev {
		raw := v.Payload
		if len(raw) == 0 {
			raw = json.RawMessage(`null`)
		}
		out = append(out, store.Event{Seq: int64(i + 1), SessionID: x.ID, Kind: v.Kind, Text: v.Text, Payload: raw, CreatedAt: x.UpdatedAt})
	}
	respond(w, 200, map[string]any{"session": x, "events": out})
}
func (s *Server) nativeImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConfirmStopped bool `json:"confirmStopped"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !req.ConfirmStopped {
		fail(w, 400, "请先停止原生会话再交接")
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, e := s.store.FindClaude(id); e == nil {
		respond(w, 200, x)
		return
	} else if !errors.Is(e, sql.ErrNoRows) {
		fail(w, 500, "无法读取会话")
		return
	}
	x, ev, e := claude.NativeHistory(s.cfg.ClaudeConfigDir, id)
	if e != nil {
		fail(w, 404, "无法读取原生会话："+e.Error())
		return
	}
	cwd, e := validateCwd(x.Cwd)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	managed, e := s.store.Create(x.Title, cwd, x.ID, true)
	if e != nil {
		fail(w, 500, "无法导入会话")
		return
	}
	for _, v := range ev {
		if _, e = s.store.Append(managed.ID, v.Kind, v.Text, v.Payload); e != nil {
			fail(w, 500, "无法保存历史")
			return
		}
	}
	respond(w, 201, managed)
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		fail(w, 404, "接口不存在")
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, "方法不支持")
		return
	}
	if s.cfg.Assets == nil {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if strings.Contains(path, "..") || strings.Contains(path, "\\") {
		http.NotFound(w, r)
		return
	}
	if _, err := fs.Stat(s.cfg.Assets, path); err != nil {
		if strings.Contains(filepath.Base(path), ".") {
			http.NotFound(w, r)
			return
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = "/index.html"
		r = clone
	}
	if strings.Contains(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.FileServer(http.FS(s.cfg.Assets)).ServeHTTP(w, r)
}

func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	// Waiting shared-terminal hooks hold long polls; release them so shutdown does not wait.
	for id, p := range s.requests {
		if p.connection != nil {
			s.finishSharedLocked(id, p, "expired")
		}
	}
	runs := []*execution{}
	for _, r := range s.runs {
		r.cancel()
		runs = append(runs, r)
	}
	s.mu.Unlock()
	for _, r := range runs {
		select {
		case <-r.done:
		case <-ctx.Done():
			return fmt.Errorf("waiting for Claude processes: %w", ctx.Err())
		}
	}
	return nil
}

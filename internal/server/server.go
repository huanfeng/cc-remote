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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/hostauth"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

type Runner func(context.Context, claude.Options, func(claude.Event) error) error
type Config struct {
	UploadDir                                                                string
	Token, ClaudeExecutable, HostExecutable, Cwd, ClaudeConfigDir, BridgeURL string
	AttachCommand                                                            string // cc-remote attach with this host's flags; the web appends a session
	// ClaudeConfigEnv is the CLAUDE_CONFIG_DIR given to Claude Code: empty unless
	// a config directory was chosen (setting it to ~/.claude would move .claude.json).
	ClaudeConfigEnv string
	// AllowOrigins are standalone web frontends (normalized origins) allowed to
	// call the API from another site; see cors.go.
	AllowOrigins []string
	// Language of the host's messages and the web UI (a page can choose its own).
	Language i18n.Lang
	Assets   fs.FS
	// Runner runs one turn per message instead of a long-lived process (tests).
	Runner Runner
	// StartProcess launches the long-lived Claude process of a managed session.
	StartProcess ProcessStarter
	// Host metadata shared by status, preferences and service management.
	DataDir, Listen, Version, LogFile string
	StartedAt                         time.Time
	// Shutdown asks the host process to exit gracefully; nil disables remote shutdown.
	Shutdown func()
	// Restart asks the host process to exit so its service manager starts it
	// again (an installed update takes effect); nil when not run as a service.
	Restart func()
	// Reload applies the service settings it can while running (listen
	// addresses) and names the ones that need a restart; nil when not run as
	// a service.
	Reload func() (ReloadResult, error)
}

// execution is a busy period of a managed session (see runtime.go).
type execution struct {
	cancel    context.CancelFunc // ends the session's Claude process
	interrupt func()             // stops the running turn, keeping the process; call with s.mu held
	ctx       context.Context
	sessionID string
	token     string
	done      chan struct{}
}
type answer struct {
	// allow / deny; "terminal" (terminal sessions only) hands the prompt back to the native dialog.
	Decision string            `json:"decision"`
	Answers  map[string]string `json:"answers,omitempty"`
	// Terminal session ExitPlanMode only: permission mode after approval, feedback on rejection.
	Mode    string `json:"mode,omitempty"`
	Message string `json:"message,omitempty"`
}
type pending struct {
	sessionID string
	ctx       context.Context
	channel   chan answer
	// Set for a terminal session question held by its PermissionRequest hook.
	connection *channelConnection
	toolName   string
	input      json.RawMessage
	// The transcript call it asks about, when the transcript shows which: only
	// that call's result ends it (an identical earlier call's must not). A
	// subagent's or an ambiguous call is left to its hook, which reads the
	// right transcript.
	useID      string
	noBackstop bool
	// A main-agent tool approval: its native dialog is on the terminal's
	// screen. Claude Code runs an approved tool before the hook returns, and
	// writes nothing until its result, so the dialog leaving the screen is the
	// first sign the terminal answered (dialogSeen, then dialogGone).
	onScreen   bool
	dialogSeen bool
	dialogGone time.Time
}
type Server struct {
	store          *store.Store
	cfg            Config
	mu             sync.Mutex
	downloads      downloadTickets          // links the browser downloads files with (downloads.go)
	streamTickets  downloadTickets          // the web's terminal streams, once each (terminal.go)
	streamPing     time.Duration            // tests: ping terminal streams this often instead
	origins        atomic.Pointer[[]string] // allowed web frontends once reloaded (cors.go)
	reloadMu       sync.Mutex               // one reload at a time, so listen ends up current
	files          fileRoots
	runs           map[string]*execution
	procs          map[string]*sessionRuntime
	terminals      map[string]*terminalSession // running terminal sessions by session ID
	startProcess   ProcessStarter
	stopReaper     chan struct{}
	closers        sync.WaitGroup // detached processes being closed
	reaperStopped  bool
	idleTimeout    time.Duration // guarded by mu
	requests       map[string]*pending
	closing        bool
	restartPending bool   // a restart waits for busy sessions (update installed)
	listen         string // bound addresses, guarded by mu; changed by a reload
	closedOnce     bool   // Close saved the running terminal sessions
	channels       map[string]*channelConnection
	modelCache     modelsCache
	statusCache    statusCache
	usageCache     usageCache
	// configMu serializes writers of preferences.json, Claude settings.json and
	// session overrides, keeping their file I/O off s.mu. Order: configMu, then s.mu.
	configMu sync.Mutex
}

func New(st *store.Store, cfg Config) *Server {
	start := cfg.StartProcess
	if start == nil {
		start = defaultProcessStarter(cfg)
	}
	s := &Server{store: st, cfg: cfg, runs: map[string]*execution{}, procs: map[string]*sessionRuntime{}, terminals: map[string]*terminalSession{}, startProcess: start, stopReaper: make(chan struct{}), listen: cfg.Listen, idleTimeout: processIdleTimeout, requests: map[string]*pending{}, channels: map[string]*channelConnection{}}
	go s.reapIdle(s.stopReaper, time.Minute)
	go s.sampleUsage(s.stopReaper, usageSampleEvery)
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/bridge/request", s.bridge)
	mux.HandleFunc("POST /api/channels/{id}/interactions", s.channelInteraction)
	mux.HandleFunc("GET "+hostauth.Path, s.hello)
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("GET /api/identity", s.identityAPI)
	mux.HandleFunc("PUT /api/identity", s.identityAPI)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/usage", s.usage)
	mux.HandleFunc("GET /api/usage/history", s.usageHistory)
	mux.HandleFunc("GET /api/sessions/{id}/stats", s.sessionStats)
	mux.HandleFunc("POST /api/admin/shutdown", s.shutdown)
	mux.HandleFunc("POST /api/admin/restart", s.restart)
	mux.HandleFunc("POST /api/admin/reload", s.reload)
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
	mux.HandleFunc("GET /api/sessions/{id}/events/{seq}", s.eventAt)
	mux.HandleFunc("GET /api/sessions/{id}/tasks", s.tasks)
	mux.HandleFunc("POST /api/terminals/{id}/hook", s.terminalHook)
	mux.HandleFunc("POST /api/terminals/{id}/status", s.terminalStatus)
	mux.HandleFunc("GET /api/terminals/{id}/stream", s.terminalStream)
	mux.HandleFunc("POST /api/terminals/{id}/ticket", s.terminalTicket)
	mux.HandleFunc("POST /api/sessions/{id}/messages", s.message)
	mux.HandleFunc("POST /api/sessions/{id}/uploads", s.upload)
	mux.HandleFunc("GET /api/sessions/{id}/uploads/{uploadID}", s.uploadGet)
	mux.HandleFunc("GET /api/sessions/{id}/file", s.file)
	mux.HandleFunc("POST /api/sessions/{id}/file/link", s.fileLink)
	mux.HandleFunc("GET /api/download/{ticket}", s.download)
	mux.HandleFunc("GET /api/sessions/{id}/commands", s.commands)
	mux.HandleFunc("POST /api/sessions/{id}/commands", s.commands)
	mux.HandleFunc("POST /api/sessions/{id}/stop", s.stop)
	mux.HandleFunc("POST /api/sessions/{id}/queue/withdraw", s.withdrawQueue)
	mux.HandleFunc("GET /api/requests", s.pending)
	mux.HandleFunc("POST /api/requests/{id}", s.resolve)
	mux.HandleFunc("GET /api/native/sessions", s.nativeList)
	mux.HandleFunc("GET /api/native/sessions/{id}", s.nativeGet)
	mux.HandleFunc("GET /api/native/sessions/{id}/events", s.nativeEventsList)
	mux.HandleFunc("GET /api/native/sessions/{id}/events/{seq}", s.nativeEventAt)
	mux.HandleFunc("POST /api/native/sessions/{id}/import", s.nativeImport)
	mux.HandleFunc("/", s.static)
	served := condensed(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; worker-src 'self'; frame-ancestors 'none'; base-uri 'self'; object-src 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if s.cors(w, r) {
				return
			}
			// A download's ticket is its credential: the browser saves it without the token.
			// So is a web terminal stream's: a browser WebSocket cannot send the header.
			if r.URL.Path != "/api/bridge/request" && r.URL.Path != hostauth.Path && !strings.HasPrefix(r.URL.Path, "/api/download/") && !streamTicket(r) && !equalToken(r.Header.Get("Authorization"), s.cfg.Token) && !s.hookRequest(r) {
				fail(w, 401, s.lang(r).T("Invalid access token", "访问凭证无效"))
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r) && !s.allowedOrigin(origin) {
				fail(w, 403, s.lang(r).T("Cross-site requests are not allowed", "不允许跨站访问"))
				return
			}
		}
		served.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether the browser Origin names this host. Behind a
// reverse proxy that rewrites Host to the upstream address, the public host
// arrives in X-Forwarded-Host instead. Trusting that header is safe here: a
// cross-site page cannot set it (or the token's Authorization header) without
// a CORS preflight, which the host grants only to --allow-origin sites and never
// for X-Forwarded-Host (keep it out of Access-Control-Allow-Headers).
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

// hello is GET /api/hello?nonce=N, without the token: the proof that this host
// holds it, which local clients check before sending it (hostauth.Check).
func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	nonce := r.URL.Query().Get("nonce")
	if !hostauth.ValidNonce(nonce) {
		fail(w, 400, s.lang(r).T("Invalid nonce", "无效的 nonce"))
		return
	}
	local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok {
		fail(w, 500, s.lang(r).T("Unknown listening address", "未知的监听地址"))
		return
	}
	respond(w, 200, map[string]string{"proof": hostauth.Proof(s.cfg.Token, nonce, local.String())})
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
	// A free function has no host setting at hand; the process language is the same.
	l, ok := i18n.Parse(r.Header.Get("X-CC-Lang"))
	if !ok || l == i18n.Auto {
		l = i18n.Current()
	}
	if err := d.Decode(v); err != nil {
		fail(w, 400, l.T("Invalid request body", "无效的请求内容"))
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		fail(w, 400, l.T("Only one complete JSON object is allowed", "只允许一个完整 JSON 对象"))
		return false
	}
	return true
}
func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	id := s.identity()
	name, hex := id.Resolved()
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
	respond(w, 200, map[string]any{"version": s.cfg.Version, "cwd": s.cfg.Cwd, "claudeAvailable": err == nil, "claudeVersion": version, "attachCommand": s.cfg.AttachCommand, "restartPending": s.pendingRestart(), "language": s.language(), "name": name, "color": hex, "icon": id.Icon})
}

// language is the host's language (English when unset, as in tests).
func (s *Server) language() i18n.Lang {
	if s.cfg.Language == i18n.ZH {
		return i18n.ZH
	}
	return i18n.EN
}

// lang is the language of a response: the page's choice (X-CC-Lang), else the host's.
func (s *Server) lang(r *http.Request) i18n.Lang {
	if l, ok := i18n.Parse(r.Header.Get("X-CC-Lang")); ok && l != i18n.Auto {
		return l
	}
	return s.language()
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	x, e := s.store.List()
	if e != nil {
		fail(w, 500, s.lang(r).T("Could not read sessions", "无法读取会话"))
		return
	}
	s.mu.Lock()
	for i := range x {
		s.decorate(&x[i])
	}
	s.mu.Unlock()
	s.fillActiveTasks(x)
	respond(w, 200, x)
}
func validateCwd(l i18n.Lang, cwd string) (string, error) {
	if !filepath.IsAbs(cwd) {
		return "", l.Errorf("Enter an absolute folder path on the host", "请输入宿主上的绝对目录路径")
	}
	p, e := filepath.Abs(cwd)
	if e != nil {
		return "", e
	}
	f, e := os.Stat(p)
	if e != nil || !f.IsDir() {
		return "", l.Errorf("The working folder does not exist or is not a folder", "工作目录不存在或不是目录")
	}
	return p, nil
}

// folderTitle is the name of an absolute folder, or "" for a root such as / or D:\.
func folderTitle(dir string) string {
	name := filepath.Base(dir)
	if name == "." || name == string(filepath.Separator) || name == filepath.VolumeName(dir) {
		return ""
	}
	return name
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Cwd   string `json:"cwd"`
		// Mode "terminal" runs an interactive Claude Code the host holds; the
		// request must confirm trusting the directory (Claude asks on first use).
		Mode        string `json:"mode"`
		TrustFolder bool   `json:"trustFolder"`
		// Resume is a Claude Code session ID a terminal session continues (as claude --resume).
		Resume string `json:"resume"`
		// ClaudeArgs are Claude Code options a terminal session starts with.
		ClaudeArgs []string `json:"claudeArgs"`
		overrideRequest
	}
	if !decode(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if len(req.ClaudeArgs) > 0 && !s.checkClaudeArgs(w, r, req.Mode, req.ClaudeArgs) {
		return
	}
	if req.Resume != "" {
		s.createResumed(w, r, req.Title, req.Resume, req.Mode, req.TrustFolder, req.ClaudeArgs, req.overrideRequest)
		return
	}
	if len(req.Title) > 300 {
		fail(w, 400, s.lang(r).T("Session name is too long", "会话名称过长"))
		return
	}
	cwd, e := validateCwd(s.lang(r), req.Cwd)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	// Unnamed, a session is named after its folder until Claude Code names it.
	if req.Title == "" {
		req.Title = folderTitle(cwd)
	}
	if req.Title == "" {
		req.Title = s.lang(r).T("New session", "新会话")
	}
	var overrides store.Session
	if e = req.apply(s.lang(r), &overrides); e != nil {
		fail(w, 400, e.Error())
		return
	}
	if req.Mode != "" && req.Mode != "managed" && req.Mode != "terminal" {
		fail(w, 400, s.lang(r).T("Invalid session type", "无效的会话类型"))
		return
	}
	if req.Mode == "terminal" && !req.TrustFolder && !s.claudeTrusts(cwd) {
		s.trustRequired(w, r)
		return
	}
	x, e := s.store.Create(req.Title, cwd, store.ID(), false)
	if e == nil {
		e = s.store.SetOverrides(x.ID, overrides.PermissionMode, overrides.Model, overrides.Effort)
		x.PermissionMode, x.Model, x.Effort = overrides.PermissionMode, overrides.Model, overrides.Effort
	}
	if e == nil && req.Mode == "terminal" {
		e = s.store.SetMode(x.ID, "terminal")
		x.Mode = "terminal"
	}
	if e == nil && len(req.ClaudeArgs) > 0 {
		e = s.store.SetClaudeArgs(x.ID, req.ClaudeArgs)
		x.ClaudeArgs = req.ClaudeArgs
	}
	if e != nil {
		fail(w, 500, s.lang(r).T("Could not create the session", "无法创建会话"))
		return
	}
	respond(w, 201, x)
}

// trustRequired refuses a terminal session in a folder Claude Code does not
// trust yet: the request must confirm trusting it (trustFolder), as Claude
// Code's own dialog would ask.
func (s *Server) trustRequired(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusPreconditionRequired, map[string]string{"error": s.lang(r).T("A terminal session requires confirming that you trust this folder", "终端会话需要确认信任此目录"), "code": "trust-required"})
}

// createResumed answers a terminal session continuing a Claude Code session:
// the one that already does, or a new one in the session's own directory.
func (s *Server) createResumed(w http.ResponseWriter, r *http.Request, title, claudeID, mode string, trust bool, claudeArgs []string, o overrideRequest) {
	l := s.lang(r)
	if mode != "terminal" {
		fail(w, 400, l.T("Only a terminal session can resume a Claude Code session", "只有终端会话可以恢复 Claude Code 会话"))
		return
	}
	claudeID = strings.ToLower(strings.TrimSpace(claudeID))
	if !channelUUID.MatchString(claudeID) {
		fail(w, 400, l.T("Invalid Claude Code session ID", "无效的 Claude Code 会话 ID"))
		return
	}
	var overrides store.Session
	if e := o.apply(l, &overrides); e != nil {
		fail(w, 400, i18n.Text(l, e))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, e := s.store.FindClaude(claudeID); e == nil {
		if x.Mode != "terminal" {
			fail(w, 409, l.F("This Claude Code session is already “%s” in CC Remote (%s)", "这个 Claude Code 会话已是 CC Remote 中的「%s」（%s）", x.Title, modeName(l, x.Mode)))
			return
		}
		s.decorate(&x)
		respond(w, 200, x)
		return
	} else if !errors.Is(e, sql.ErrNoRows) {
		fail(w, 500, l.T("Could not read sessions", "无法读取会话"))
		return
	}
	n, _, e := claude.NativeHistory(s.cfg.ClaudeConfigDir, claudeID)
	if e != nil {
		fail(w, 404, l.T("Claude Code session not found", "找不到这个 Claude Code 会话"))
		return
	}
	cwd, e := validateCwd(l, n.Cwd)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	if !trust && !s.claudeTrusts(cwd) {
		s.trustRequired(w, r)
		return
	}
	if title == "" {
		title = n.Title
	}
	x, e := s.store.Create(title, cwd, claudeID, true)
	if e == nil {
		e = s.store.SetOverrides(x.ID, overrides.PermissionMode, overrides.Model, overrides.Effort)
		x.PermissionMode, x.Model, x.Effort = overrides.PermissionMode, overrides.Model, overrides.Effort
	}
	if e == nil {
		e = s.store.SetMode(x.ID, "terminal")
		x.Mode = "terminal"
	}
	if e == nil && len(claudeArgs) > 0 {
		e = s.store.SetClaudeArgs(x.ID, claudeArgs)
		x.ClaudeArgs = claudeArgs
	}
	if e != nil {
		fail(w, 500, l.T("Could not create the session", "无法创建会话"))
		return
	}
	respond(w, 201, x)
}

// checkClaudeArgs accepts Claude Code options for a terminal session: ones
// the host does not set itself, and no prompt (that is sent as a message).
func (s *Server) checkClaudeArgs(w http.ResponseWriter, r *http.Request, mode string, args []string) bool {
	l := s.lang(r)
	if mode != "terminal" {
		fail(w, 400, l.T("Only a terminal session takes Claude Code options", "只有终端会话可以带 Claude Code 参数"))
		return false
	}
	size := 0
	for _, a := range args {
		size += len(a)
	}
	if len(args) > 100 || size > 16<<10 {
		fail(w, 400, l.T("Too many Claude Code options", "Claude Code 参数过多"))
		return false
	}
	if bad := claude.CheckTerminalArgs(args); bad != "" {
		fail(w, 400, l.F("A terminal session cannot pass %q to Claude Code", "终端会话不能把 %q 传给 Claude Code", bad))
		return false
	}
	return true
}

// modeName is how the web names a session type.
func modeName(l i18n.Lang, mode string) string {
	switch mode {
	case "terminal":
		return l.T("terminal session", "终端会话")
	}
	return l.T("managed session", "托管会话")
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	s.markViewed(r, r.PathValue("id"))
	x, e := s.store.Get(r.PathValue("id"))
	if e != nil {
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	q := r.URL.Query()
	tail := q.Get("tail")
	limit, ok := pageLimit(tail)
	if !ok {
		fail(w, 400, s.lang(r).T("Invalid paging parameters", "无效的分页参数"))
		return
	}
	var events []store.Event
	older := false
	if tail != "" {
		events, older, e = s.store.EventsPage(x.ID, 0, limit, pageReach)
	} else {
		events, e = s.store.Events(x.ID, 0)
	}
	if e != nil {
		fail(w, 500, s.lang(r).T("Could not read messages", "无法读取消息"))
		return
	}
	if q.Get("compact") == "1" {
		events = trimEvents(events)
	}
	s.mu.Lock()
	s.decorate(&x)
	s.mu.Unlock()
	one := []store.Session{x}
	s.fillActiveTasks(one)
	respond(w, 200, map[string]any{"session": one[0], "events": events, "hasMore": older})
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.markViewed(r, id)
	if _, e := s.store.Get(id); e != nil {
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	q := r.URL.Query()
	compact := q.Get("compact") == "1"
	if raw := q.Get("before"); raw != "" {
		before, ok := cursor(raw)
		limit, okLimit := pageLimit(q.Get("limit"))
		if !ok || !okLimit {
			fail(w, 400, s.lang(r).T("Invalid paging parameters", "无效的分页参数"))
			return
		}
		page, older, e := s.store.EventsPage(id, before, limit, pageReach)
		if e != nil {
			fail(w, 500, s.lang(r).T("Could not read messages", "无法读取消息"))
			return
		}
		if compact {
			page = trimEvents(page)
		}
		respond(w, 200, map[string]any{"events": page, "hasMore": older})
		return
	}
	after, ok := cursor(q.Get("after"))
	if !ok {
		fail(w, 400, s.lang(r).T("Invalid event cursor", "无效的事件游标"))
		return
	}
	x, e := s.store.Events(id, after)
	if e != nil {
		fail(w, 500, s.lang(r).T("Could not read messages", "无法读取消息"))
		return
	}
	if compact {
		x = trimEvents(x)
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
		fail(w, 400, s.lang(r).T("The message is empty or larger than 1 MiB", "消息为空或超过 1 MiB"))
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		fail(w, 503, s.lang(r).T("The host is shutting down", "宿主正在关闭"))
		return
	}
	x, e := s.store.Get(id)
	if e != nil {
		s.mu.Unlock()
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	if x.Mode == "terminal" {
		// Claude Code's own terminal runs every command and skill itself.
		if strings.HasPrefix(req.Text, "/") && len(req.AttachmentIDs) > 0 {
			s.mu.Unlock()
			fail(w, 400, s.lang(r).T("Commands cannot carry attachments", "命令不能附带附件"))
			return
		}
		prompt, payload, e := s.messageAttachments(s.lang(r), x.ID, req.Text, req.AttachmentIDs)
		if e != nil {
			s.mu.Unlock()
			fail(w, 400, e.Error())
			return
		}
		s.terminalMessageLocked(w, r, x, prompt, store.QueuedMessage{Text: req.Text, Payload: payload})
		s.mu.Unlock()
		return
	}
	promptText := req.Text
	name, args, _ := strings.Cut(strings.TrimPrefix(req.Text, "/"), " ")
	native := strings.HasPrefix(req.Text, "/") && s.nativeCommand(x, name)
	if strings.HasPrefix(req.Text, "/") && name == "compact" {
		message, code := compactBlocked(s.lang(r), x)
		if code != 0 {
			s.mu.Unlock()
			fail(w, code, message)
			return
		}
		native = true
	}
	if native {
		// Claude Code runs the command itself (print mode), so it is sent verbatim.
		if len(req.AttachmentIDs) > 0 {
			s.mu.Unlock()
			fail(w, 400, s.lang(r).F("/%s cannot carry attachments", "/%s 命令不能附带附件", name))
			return
		}
	} else if strings.HasPrefix(req.Text, "/") {
		var found bool
		promptText, found, e = s.skillPrompt(s.lang(r), x.Cwd, name, strings.TrimSpace(args))
		if e != nil || !found {
			s.mu.Unlock()
			message := s.lang(r).T("Run built-in / commands from the command menu", "请通过命令入口执行内置 / 命令")
			if e != nil {
				message = e.Error()
			}
			fail(w, 400, message)
			return
		}
	}
	prompt, payload, e := s.messageAttachments(s.lang(r), x.ID, promptText, req.AttachmentIDs)
	if e != nil {
		s.mu.Unlock()
		fail(w, 400, e.Error())
		return
	}
	if _, e = validateCwd(s.lang(r), x.Cwd); e != nil {
		s.mu.Unlock()
		fail(w, 400, e.Error())
		return
	}
	if rt := s.procs[id]; rt != nil && len(rt.sent) >= maxQueuedMessages {
		s.mu.Unlock()
		fail(w, 429, s.lang(r).T("Too many queued messages; wait for the current turn to finish", "排队的消息过多，请等待当前轮次完成"))
		return
	}
	if len(payload) == 0 {
		payload = nil
	}
	rt := s.runtimeLocked(x)
	queued := s.enqueueLocked(rt, outgoing{QueuedMessage: store.QueuedMessage{Text: req.Text, Payload: payload}, prompt: prompt})
	s.mu.Unlock()
	status := "running"
	if queued {
		status = "queued"
	}
	respond(w, 202, map[string]string{"status": status})
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.terminals[r.PathValue("id")] != nil {
		s.terminalStopLocked(w, r, r.PathValue("id"))
		s.mu.Unlock()
		return
	}
	run, ok := s.runs[r.PathValue("id")]
	if ok {
		run.interrupt()
	}
	s.mu.Unlock()
	if !ok {
		fail(w, 409, s.lang(r).T("The session has no running turn", "会话没有正在运行的轮次"))
		return
	}
	respond(w, 200, map[string]string{"status": "stopping"})
}

// withdrawQueue takes a terminal session's queued web messages back for editing.
func (s *Server) withdrawQueue(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	ts := s.terminals[r.PathValue("id")]
	if ts == nil {
		s.mu.Unlock()
		fail(w, 409, s.lang(r).T("Only a running terminal session has queued messages to withdraw", "只有运行中的终端会话可以撤回排队消息"))
		return
	}
	s.terminalWithdrawLocked(w, r, ts) // unlocks s.mu
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
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	run := s.runs[id]
	if ts := s.terminals[id]; ts != nil {
		// Deleting ends the terminal session's Claude Code (its history stays).
		delete(s.terminals, id)
		if s.channels[ts.claudeID] == ts.conn {
			delete(s.channels, ts.claudeID)
		}
		s.closeTerminalAsync(ts)
	}
	if run != nil {
		if r.URL.Query().Get("stop") != "1" {
			s.mu.Unlock()
			fail(w, 409, s.lang(r).T("Stop the run first", "请先停止执行"))
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
			fail(w, 504, s.lang(r).T("Timed out stopping the run; try again later", "停止执行超时，请稍后重试"))
			return
		case <-r.Context().Done():
			return
		}
	}
	s.mu.Lock()
	x.Online = false
	s.decorate(&x)
	if s.runs[id] != nil || x.Online {
		s.mu.Unlock()
		fail(w, 409, s.lang(r).T("The session started running again or a terminal connected; try again", "会话已重新开始执行或终端已连接，请重试"))
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
	for cid, c := range s.channels {
		if c.sessionID == id {
			delete(s.channels, cid)
		}
	}
	err = s.store.Delete(id)
	s.mu.Unlock()
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not delete the session", "无法删除会话"))
		return
	}
	s.files.forget(id)
	s.mu.Lock()
	rt := s.detachLocked(id) // an idle Claude process may still hold the session
	s.mu.Unlock()
	closeRuntime(rt)
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
		fail(w, 403, s.lang(r).T("Only local bridge connections are allowed", "仅允许本机桥接"))
		return
	}
	s.mu.Lock()
	rt := s.bridgeRuntimeLocked(r.Header.Get("Authorization"))
	var run *execution
	if rt != nil {
		run = rt.run
	}
	s.mu.Unlock()
	if run == nil {
		fail(w, 401, s.lang(r).T("The run credential is invalid or expired", "执行凭证无效或已失效"))
		return
	}
	defer func() {
		s.mu.Lock()
		s.bridgeDoneLocked(rt)
		s.mu.Unlock()
	}()
	var req struct {
		Kind     string          `json:"kind"`
		ToolName string          `json:"toolName"`
		Input    json.RawMessage `json:"input"`
	}
	if !decode(w, r, &req) {
		return
	}
	if (req.Kind != "approval" && req.Kind != "question" && req.Kind != "plan") || !json.Valid(req.Input) {
		fail(w, 400, s.lang(r).T("Invalid interaction kind", "无效的交互类型"))
		return
	}
	x := store.Interaction{ID: store.ID(), SessionID: run.sessionID, Kind: req.Kind, ToolName: req.ToolName, Input: req.Input}
	p := &pending{sessionID: run.sessionID, ctx: run.ctx, channel: make(chan answer, 1)}
	s.mu.Lock()
	if run.ctx.Err() != nil || s.runs[run.sessionID] != run {
		s.mu.Unlock()
		fail(w, 409, s.lang(r).T("The run has stopped", "执行已停止"))
		return
	}
	if err := s.store.AddInteraction(x); err != nil {
		s.mu.Unlock()
		fail(w, 500, s.lang(r).T("Could not save the interaction request", "无法保存交互请求"))
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
		fail(w, 409, s.lang(r).T("The run has stopped", "执行已停止"))
	case <-r.Context().Done():
		return
	case <-timer.C:
		respond(w, 200, answer{Decision: "deny"})
	}
}

func (s *Server) pending(w http.ResponseWriter, r *http.Request) {
	x, e := s.store.Pending(r.URL.Query().Get("sessionId"))
	if e != nil {
		fail(w, 500, s.lang(r).T("Could not read interaction requests", "无法读取交互请求"))
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
		fail(w, 400, s.lang(r).T("Invalid approval decision", "无效的审批结果"))
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.requests[id]
	if !ok || p.ctx.Err() != nil {
		fail(w, 409, s.lang(r).T("The request was already handled or has expired", "请求已处理或已失效"))
		return
	}
	status := "resolved"
	if p.connection != nil {
		if msg := checkSharedAnswer(s.lang(r), p, &a); msg != "" {
			fail(w, 400, msg)
			return
		}
		if a.Decision == "terminal" {
			status = "terminal"
		}
	} else if a.Decision == "terminal" || a.Mode != "" || a.Message != "" && a.Decision != "deny" || len(a.Message) > 64<<10 {
		// A managed session's denial may say why ("chat about this" for a question).
		fail(w, 400, s.lang(r).T("Invalid approval decision", "无效的审批结果"))
		return
	}
	if err := s.store.Resolve(id, status); err != nil {
		fail(w, 409, i18n.Text(s.lang(r), err))
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
		fail(w, 500, s.lang(r).T("Could not read Claude Code's native sessions: ", "无法读取 Claude Code 原生会话：")+e.Error())
		return
	}
	respond(w, 200, x)
}
func (s *Server) nativeGet(w http.ResponseWriter, r *http.Request) {
	x, ev, e := claude.NativeHistory(s.cfg.ClaudeConfigDir, r.PathValue("id"))
	if e != nil {
		fail(w, 404, s.lang(r).T("Could not read the native session: ", "无法读取原生会话：")+e.Error())
		return
	}
	q := r.URL.Query()
	out := nativeEvents(x, ev)
	older := false
	if tail := q.Get("tail"); tail != "" {
		limit, ok := pageLimit(tail)
		if !ok {
			fail(w, 400, s.lang(r).T("Invalid paging parameters", "无效的分页参数"))
			return
		}
		out, older = pageSlice(out, 0, limit, pageReach)
	}
	if q.Get("compact") == "1" {
		out = trimEvents(out)
	}
	respond(w, 200, map[string]any{"session": x, "events": out, "hasMore": older})
}
func (s *Server) nativeImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConfirmStopped bool `json:"confirmStopped"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !req.ConfirmStopped {
		fail(w, 400, s.lang(r).T("Stop the native session before handing it over", "请先停止原生会话再交接"))
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	if x, e := s.store.FindClaude(id); e == nil {
		respond(w, 200, x)
		return
	} else if !errors.Is(e, sql.ErrNoRows) {
		fail(w, 500, s.lang(r).T("Could not read sessions", "无法读取会话"))
		return
	}
	x, ev, e := claude.NativeHistory(s.cfg.ClaudeConfigDir, id)
	if e != nil {
		fail(w, 404, s.lang(r).T("Could not read the native session: ", "无法读取原生会话：")+e.Error())
		return
	}
	cwd, e := validateCwd(s.lang(r), x.Cwd)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	managed, e := s.store.Create(x.Title, cwd, x.ID, true)
	if e != nil {
		fail(w, 500, s.lang(r).T("Could not import the session", "无法导入会话"))
		return
	}
	for _, v := range ev {
		if _, e = s.store.Append(managed.ID, v.Kind, v.Text, v.Payload); e != nil {
			fail(w, 500, s.lang(r).T("Could not save the history", "无法保存历史"))
			return
		}
	}
	respond(w, 201, managed)
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		fail(w, 404, s.lang(r).T("API endpoint not found", "接口不存在"))
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, s.lang(r).T("Method not allowed", "方法不支持"))
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
	if _, err := fs.Stat(s.cfg.Assets, path); err != nil && path != "host-icon.svg" {
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
	// The page starts in the host's language, name and colour.
	if s.identityAsset(w, r) {
		return
	}
	http.FileServer(http.FS(s.cfg.Assets)).ServeHTTP(w, r)
}

func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	// Waiting terminal session hooks hold long polls; release them so shutdown does not wait.
	for id, p := range s.requests {
		if p.connection != nil {
			s.finishSharedLocked(id, p, "expired")
		}
	}
	runs := []*execution{}
	for _, r := range s.runs {
		runs = append(runs, r)
	}
	if !s.reaperStopped {
		s.reaperStopped = true
		close(s.stopReaper)
	}
	// Terminal sessions end with the host; the next host starts the running
	// ones again (RestoreTerminals), and others resume (--resume) on next use.
	if !s.closedOnce {
		s.closedOnce = true
		s.saveRunningTerminalsLocked()
	}
	for _, ts := range s.terminals {
		s.closeTerminalAsync(ts)
	}
	s.mu.Unlock()
	if err := s.closeProcesses(ctx); err != nil {
		return err
	}
	for _, r := range runs {
		select {
		case <-r.done:
		case <-ctx.Done():
			return fmt.Errorf("waiting for Claude processes: %w", ctx.Err())
		}
	}
	return nil
}

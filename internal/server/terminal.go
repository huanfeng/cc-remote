package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
	"cc-remote/internal/terminal"

	"github.com/coder/websocket"
)

// Terminal sessions run an interactive Claude Code in a pseudo-terminal the
// host owns (see docs/design/terminal-sessions.md). Terminal front ends
// (cc-remote attach) stream its screen; the web shows the transcript, like a
// shared session, and sends messages as pasted text. A terminal session
// registers a channelConnection so the shared-session transcript sync and the
// AskUserQuestion / ExitPlanMode hook bridge work for it unchanged.

const (
	terminalSync      = time.Second
	terminalStopGrace = 15 * time.Second // a second stop within this presses Ctrl+C
	terminalCloseWait = 5 * time.Second
)

// firstPromptWait is how long the first message may take to be submitted.
var firstPromptWait = 15 * time.Second

type terminalSession struct {
	id, claudeID string
	runner       *terminal.Runner
	conn         *channelConnection
	dir          string // per-session config (hook settings, ask.json)
	send         chan string
	queued       []terminalQueued // web messages not yet in the transcript
	scan         int              // transcript user messages already matched
	lastStop     time.Time
	closing      bool
	prompts      int           // UserPromptSubmit hooks seen
	confirmed    bool          // a web message was seen submitted since Claude Code started
	wake         chan struct{} // a hook asks for a transcript sync now
}

type terminalQueued struct {
	text    string // as written to the terminal
	message store.QueuedMessage
}

// terminalConfig is read by `cc-remote terminal-hook` (CC_REMOTE_TERMINAL_FILE).
type terminalConfig struct {
	Host    string `json:"host"`
	Token   string `json:"token"`
	Session string `json:"session"`
}

func (s *Server) hookHost() string {
	return strings.TrimSuffix(s.cfg.BridgeURL, "/api/bridge/request")
}

// terminalLocked returns the running terminal of a session, starting Claude
// Code when it is not running.
func (s *Server) terminalLocked(x store.Session) (*terminalSession, error) {
	if ts := s.terminals[x.ID]; ts != nil {
		select {
		case <-ts.runner.Done(): // exited; terminalExited cleans up shortly
			return nil, errors.New("Claude Code 刚刚退出，请稍后重试")
		default:
			return ts, nil
		}
	}
	if s.closing {
		return nil, errors.New("宿主正在关闭")
	}
	if _, err := validateCwd(x.Cwd); err != nil {
		return nil, err
	}
	if s.cfg.DataDir == "" || s.cfg.HostExecutable == "" {
		return nil, errors.New("宿主没有配置数据目录或可执行文件路径")
	}
	dir := filepath.Join(s.cfg.DataDir, "terminal-config", x.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ts := &terminalSession{id: x.ID, claudeID: strings.ToLower(x.ClaudeSessionID), dir: dir, send: make(chan string, maxQueuedMessages+1), wake: make(chan struct{}, 1)}
	if err := s.writeTerminalConfig(ts); err != nil {
		return nil, err
	}
	args := []string{"--settings", filepath.Join(dir, "settings.json")}
	// Claude Code writes the transcript with the first message: a session
	// that never got one has nothing to resume.
	if _, events, err := claude.NativeHistory(s.cfg.ClaudeConfigDir, ts.claudeID); err == nil {
		args = append(args, "--resume", ts.claudeID)
		ts.scan = countUsers(events) // web messages match only new transcript entries
	} else {
		args = append(args, "--session-id", ts.claudeID)
	}
	p := s.effectivePreferences(x)
	args = append(args, claude.LaunchFlags(p.PermissionMode, p.Model, p.Effort)...)
	env := terminal.CleanEnv(os.Environ())
	env = append(env, "CC_REMOTE_ASK_FILE="+filepath.Join(dir, "ask.json"), "CC_REMOTE_TERMINAL_FILE="+filepath.Join(dir, "terminal.json"))
	if s.cfg.ClaudeConfigDir != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+s.cfg.ClaudeConfigDir)
	}
	exe := s.cfg.ClaudeExecutable
	if exe == "" {
		exe = "claude"
	}
	// The user confirmed trusting the directory when creating the session.
	r, err := terminal.Start(terminal.Options{Executable: exe, Args: args, Dir: x.Cwd, Env: env, TrustFolder: true, AwaitStart: true})
	if err != nil {
		_ = os.RemoveAll(dir) // holds the access token
		return nil, err
	}
	ts.runner = r
	ts.conn = &channelConnection{sessionID: x.ID, key: store.ID() + store.ID(), seen: time.Now(), queue: []channelCommand{}, keyPrefix: ts.claudeID + ":"}
	s.terminals[x.ID] = ts
	s.channels[ts.claudeID] = ts.conn
	if !x.Resume {
		_ = s.store.SetResume(x.ID)
	}
	if x.Status == "running" || x.Status == "waiting" {
		_ = s.store.SetStatus(x.ID, "idle")
	}
	s.closers.Add(2)
	go s.terminalWriter(ts)
	go s.terminalLoop(ts)
	return ts, nil
}

// writeTerminalConfig writes the hook settings and the files the hooks read.
func (s *Server) writeTerminalConfig(ts *terminalSession) error {
	command := func(arg string) map[string]any {
		return map[string]any{"type": "command", "command": s.cfg.HostExecutable, "args": []string{arg}}
	}
	ask := command("channel-ask-hook")
	ask["timeout"] = 1800
	state := []any{map[string]any{"hooks": []any{command("terminal-hook")}}}
	settings, _ := json.Marshal(map[string]any{"hooks": map[string]any{
		"PermissionRequest": []any{map[string]any{"matcher": "AskUserQuestion|ExitPlanMode", "hooks": []any{ask}}},
		"SessionStart":      state,
		"UserPromptSubmit":  state,
		"Stop":              state,
		"Notification":      state,
		"SubagentStop":      state,
		"PostToolUse":       state,
	}})
	host := s.hookHost()
	askFile, _ := json.Marshal(map[string]string{"host": host, "token": s.cfg.Token, "sessionId": ts.claudeID, "bindingFile": ""})
	termFile, _ := json.Marshal(terminalConfig{Host: host, Token: s.cfg.Token, Session: ts.id})
	for name, data := range map[string][]byte{"settings.json": settings, "ask.json": askFile, "terminal.json": termFile} {
		if err := os.WriteFile(filepath.Join(ts.dir, name), data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// terminalWriter pastes web messages one after another.
func (s *Server) terminalWriter(ts *terminalSession) {
	defer s.closers.Done()
	for {
		select {
		case text := <-ts.send:
			if !s.awaitNoDialog(ts) {
				return
			}
			s.mu.Lock()
			before, first := ts.prompts, !ts.confirmed
			s.mu.Unlock()
			if err := ts.runner.Paste(text); err != nil {
				log.Printf("terminal %s: %v", ts.id, err)
				continue
			}
			if first && !strings.HasPrefix(text, "/") { // local commands run no UserPromptSubmit
				s.confirmFirstPrompt(ts, text, before)
			}
		case <-ts.runner.Done():
			return
		}
	}
}

// awaitNoDialog waits while Claude Code shows a permission or question
// dialog: a pasted message and its Enter would answer the dialog instead.
// It reports false when Claude Code exited.
func (s *Server) awaitNoDialog(ts *terminalSession) bool {
	for {
		s.mu.Lock()
		open := false
		if x, err := s.store.Get(ts.id); err == nil && x.Status == "waiting" {
			open = true
		}
		for _, p := range s.requests {
			if p.connection == ts.conn {
				open = true
			}
		}
		s.mu.Unlock()
		if !open {
			return true
		}
		select {
		case <-ts.runner.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// confirmFirstPrompt checks that the first message after Claude Code started
// was submitted (input typed while it starts can be dropped): without a
// UserPromptSubmit it presses Enter if the text sits in the input box, or
// pastes it again.
func (s *Server) confirmFirstPrompt(ts *terminalSession, text string, before int) {
	submitted := func(wait time.Duration) bool {
		for end := time.Now().Add(wait); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
			if isDone(ts.runner.Done()) {
				return true
			}
			s.mu.Lock()
			n := ts.prompts
			s.mu.Unlock()
			if n > before {
				return true
			}
		}
		return false
	}
	if !submitted(firstPromptWait) {
		if ts.runner.Shows(text) {
			log.Printf("terminal %s: first message not submitted; pressing Enter", ts.id)
			_ = ts.runner.Submit()
		} else {
			log.Printf("terminal %s: first message lost while Claude Code started; sending again", ts.id)
			_ = ts.runner.Paste(text)
		}
		submitted(firstPromptWait)
	}
	s.mu.Lock()
	ts.confirmed = true
	s.mu.Unlock()
}

// terminalLoop keeps the connection alive and the transcript in sync until
// Claude Code exits.
func (s *Server) terminalLoop(ts *terminalSession) {
	defer s.closers.Done()
	tick := time.NewTicker(terminalSync)
	defer tick.Stop()
	for {
		select {
		case <-ts.runner.Done():
			s.syncTerminal(ts)
			s.terminalExited(ts)
			return
		case <-tick.C:
		case <-ts.wake:
		}
		s.syncTerminal(ts)
	}
}

func (s *Server) syncTerminal(ts *terminalSession) {
	s.mu.Lock()
	claudeID := ts.claudeID
	ts.conn.seen = time.Now()
	s.mu.Unlock()
	n, events, err := claude.NativeHistory(s.cfg.ClaudeConfigDir, claudeID)
	if err != nil {
		return // no transcript before the first message
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ts.claudeID != claudeID || s.terminals[ts.id] != ts {
		return
	}
	s.applyTranscriptLocked(ts.conn, n, events)
	// Web messages leave the queue once the transcript shows them.
	var users []string
	for _, e := range events {
		if e.Kind == "user" {
			users = append(users, normalizePrompt(e.Text))
		}
	}
	if ts.scan < 0 { // switched transcripts (/resume): only new entries count
		ts.scan = len(users)
	}
	for len(ts.queued) > 0 {
		found := -1
		for i := ts.scan; i < len(users); i++ {
			if users[i] == normalizePrompt(ts.queued[0].text) {
				found = i
				break
			}
		}
		if found < 0 {
			break
		}
		ts.queued, ts.scan = ts.queued[1:], found+1
	}
}

func countUsers(events []claude.Event) int {
	n := 0
	for _, e := range events {
		if e.Kind == "user" {
			n++
		}
	}
	return n
}

func isDone(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// popQueuedLocked drops the queued web message Claude Code just submitted.
func (ts *terminalSession) popQueuedLocked(prompt string) {
	want := normalizePrompt(prompt)
	for i, q := range ts.queued {
		if normalizePrompt(q.text) == want {
			ts.queued = append(ts.queued[:i:i], ts.queued[i+1:]...)
			return
		}
	}
}

func normalizePrompt(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

func (s *Server) terminalExited(ts *terminalSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminals[ts.id] == ts {
		delete(s.terminals, ts.id)
	}
	if s.channels[ts.claudeID] == ts.conn {
		delete(s.channels, ts.claudeID)
	}
	for id, p := range s.requests {
		if p.connection == ts.conn {
			s.finishSharedLocked(id, p, "expired")
		}
	}
	if x, err := s.store.Get(ts.id); err == nil {
		if !ts.closing {
			note := "Claude Code 已退出"
			if len(ts.queued) > 0 {
				note += "，以上排队消息未执行，请重新发送"
			}
			if err := ts.runner.Err(); err != nil {
				note += "（" + err.Error() + "）"
			}
			_, _ = s.store.Append(ts.id, "error", note+"。下次发送消息时会恢复会话。", nil)
		}
		if x.Status == "running" || x.Status == "waiting" {
			_ = s.store.SetStatus(ts.id, "interrupted")
		}
	}
	_ = os.RemoveAll(ts.dir)
}

// closeTerminalAsync ends a session's Claude Code (delete, host shutdown).
func (s *Server) closeTerminalAsync(ts *terminalSession) {
	ts.closing = true
	s.closers.Add(1)
	go func() {
		defer s.closers.Done()
		ts.runner.Close(terminalCloseWait)
	}()
}

// terminalMessageLocked sends a web message to a terminal session.
func (s *Server) terminalMessageLocked(w http.ResponseWriter, x store.Session, prompt string, message store.QueuedMessage) {
	ts, err := s.terminalLocked(x)
	if err != nil {
		fail(w, 500, "无法启动 Claude Code："+err.Error())
		return
	}
	if len(ts.queued) >= maxQueuedMessages {
		fail(w, 429, "排队的消息过多，请等待 Claude 处理")
		return
	}
	status := "queued"
	if latest, e := s.store.Get(x.ID); e == nil && latest.Status != "running" && latest.Status != "waiting" && len(ts.queued) == 0 {
		status = "running"
	}
	prompt = terminal.CleanText(prompt)
	select {
	case ts.send <- prompt:
	default:
		fail(w, 429, "排队的消息过多，请等待 Claude 处理")
		return
	}
	// Local commands (/compact, /clear) never show up as a prompt: not queued.
	if !strings.HasPrefix(prompt, "/") {
		ts.queued = append(ts.queued, terminalQueued{text: prompt, message: message})
	}
	respond(w, 202, map[string]string{"status": status})
}

// terminalStopLocked presses Esc; a second stop soon after presses Ctrl+C.
func (s *Server) terminalStopLocked(w http.ResponseWriter, id string) {
	ts := s.terminals[id]
	if ts == nil {
		fail(w, 409, "Claude Code 没有运行")
		return
	}
	var err error
	if time.Since(ts.lastStop) < terminalStopGrace {
		err = ts.runner.Cancel()
		ts.lastStop = time.Time{} // a second Ctrl+C would quit Claude Code
	} else {
		err = ts.runner.Interrupt()
		ts.lastStop = time.Now()
	}
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	// Claude Code runs no Stop hook for an interrupted turn, and Esc takes
	// effect at once; the next prompt's hook marks the session running again.
	if x, e := s.store.Get(id); e == nil && (x.Status == "running" || x.Status == "waiting") {
		_ = s.store.SetStatus(id, "interrupted")
	}
	respond(w, 200, map[string]string{"status": "stopping"})
}

// terminalHook receives Claude Code's hook events from `cc-remote terminal-hook`.
func (s *Server) terminalHook(w http.ResponseWriter, r *http.Request) {
	if !s.channelLocal(w, r) {
		return
	}
	var req struct {
		Event     string `json:"hook_event_name"`
		SessionID string `json:"session_id"`
		Source    string `json:"source"`
		Message   string `json:"message"`
		Type      string `json:"notification_type"`
		Prompt    string `json:"prompt"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, "无效的 hook 内容")
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	ts := s.terminals[id]
	if ts == nil {
		s.mu.Unlock()
		fail(w, 404, "终端会话没有运行")
		return
	}
	switch req.Event {
	case "SessionStart":
		ts.runner.MarkStarted()
		// /clear and /resume switch Claude's session; follow it.
		if next := strings.ToLower(req.SessionID); next != "" && next != ts.claudeID && channelUUID.MatchString(next) && s.rebindFreeLocked(id, next, ts) {
			if s.channels[ts.claudeID] == ts.conn {
				delete(s.channels, ts.claudeID)
			}
			ts.claudeID, ts.scan = next, -1
			ts.conn.keyPrefix = next + ":"
			s.channels[next] = ts.conn
			if err := s.store.SetClaudeID(id, next); err != nil {
				log.Printf("terminal %s: rebind: %v", id, err)
			}
			if err := s.writeTerminalConfig(ts); err != nil {
				log.Printf("terminal %s: config: %v", id, err)
			}
		}
	case "UserPromptSubmit":
		ts.prompts++
		ts.popQueuedLocked(req.Prompt)
		_ = s.store.SetStatus(id, "running")
	case "PostToolUse":
		// A dialog answered in the terminal: the tool ran.
		if x, err := s.store.Get(id); err == nil && x.Status == "waiting" {
			_ = s.store.SetStatus(id, "running")
		}
	case "Stop":
		_ = s.store.SetStatus(id, "completed")
	case "Notification":
		if req.Type == "permission_prompt" || strings.Contains(strings.ToLower(req.Message), "permission") {
			_ = s.store.SetStatus(id, "waiting")
		}
	}
	s.mu.Unlock()
	select {
	case ts.wake <- struct{}{}:
	default:
	}
	respond(w, 200, map[string]string{"status": "ok"})
}

// rebindFreeLocked reports whether a terminal session may follow Claude Code
// to another Claude session id (/resume): not one another session owns.
func (s *Server) rebindFreeLocked(id, next string, ts *terminalSession) bool {
	if other, err := s.store.FindClaude(next); err == nil && other.ID != id {
		_, _ = s.store.Append(id, "error", "终端里切换到了另一个网页会话正在使用的 Claude 会话，网页不会跟随；请回到原会话或在该会话中继续。", nil)
		return false
	}
	if c := s.channels[next]; c != nil && c != ts.conn {
		return false
	}
	return true
}

// terminalStream connects a terminal front end: binary frames carry keystrokes
// and screen output; a text frame {"type":"resize","cols":..,"rows":..} sets the size.
func (s *Server) terminalStream(w http.ResponseWriter, r *http.Request) {
	x, err := s.store.Get(r.PathValue("id"))
	if err != nil || x.Mode != "terminal" {
		fail(w, 404, "终端会话不存在")
		return
	}
	s.mu.Lock()
	ts, err := s.terminalLocked(x)
	s.mu.Unlock()
	if err != nil {
		fail(w, 500, "无法启动 Claude Code："+err.Error())
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	sub := ts.runner.Subscribe()
	defer ts.runner.Unsubscribe(sub)
	sized := make(chan struct{}, 1)
	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				_ = ts.runner.Keys(data)
				continue
			}
			var m struct {
				Type       string `json:"type"`
				Cols, Rows int
			}
			if json.Unmarshal(data, &m) == nil && m.Type == "resize" {
				_ = ts.runner.Resize(m.Cols, m.Rows)
				select {
				case sized <- struct{}{}:
				default:
				}
			}
		}
	}()
	// Paint the whole screen for the new front end once it reported its size.
	go func() {
		select {
		case <-sized:
		case <-time.After(time.Second):
		case <-ctx.Done():
			return
		}
		ts.runner.Redraw()
	}()
	for {
		select {
		case chunk, ok := <-sub.C:
			if !ok {
				reason := "已断开"
				select {
				case <-ts.runner.Done():
					reason = "Claude Code 已退出"
				default:
				}
				_ = c.Close(websocket.StatusNormalClosure, reason)
				return
			}
			if err := c.Write(ctx, websocket.MessageBinary, chunk); err != nil {
				return
			}
		case <-ctx.Done():
			_ = c.Close(websocket.StatusNormalClosure, "")
			return
		}
	}
}

// decorateTerminalLocked fills a terminal session's live fields.
func (s *Server) decorateTerminalLocked(x *store.Session) {
	ts := s.terminals[x.ID]
	x.Online = ts != nil
	if ts == nil {
		return
	}
	if len(ts.queued) > 0 {
		x.Queued = make([]store.QueuedMessage, len(ts.queued))
		for i, q := range ts.queued {
			x.Queued[i] = q.message
		}
	}
}

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
	"cc-remote/internal/terminal"

	"github.com/coder/websocket"
)

// Terminal sessions run an interactive Claude Code in a pseudo-terminal the
// host owns (see docs/design/terminal-sessions.md). Terminal front ends
// (cc-remote attach) stream its screen; the web shows the transcript and sends
// messages as pasted text. A terminal session registers a channelConnection for
// the transcript sync and the AskUserQuestion / ExitPlanMode hook bridge
// (channels.go).

const (
	terminalSync      = time.Second
	terminalStopGrace = 15 * time.Second // a second stop within this presses Ctrl+C
	terminalCloseWait = 5 * time.Second
)

// firstPromptWait is how long the first message may take to be submitted,
// each of firstPromptTries times.
var firstPromptWait = 12 * time.Second

const firstPromptTries = 3

type terminalSession struct {
	id, claudeID string
	// hookToken is what its hooks authenticate with, for their own endpoints
	// only: Claude Code and every command it runs can read the hook files.
	hookToken string
	runner    *terminal.Runner
	conn      *channelConnection
	cleaned   chan struct{}
	dir       string // per-session config (hook settings, ask.json)
	send      chan string
	withdraw  chan chan []string // the writer takes the queued messages back (see withdrawQueued)
	queued    []terminalQueued   // web messages not yet in the transcript
	scan      int                // transcript user messages already matched
	lastStop  time.Time
	closing   bool
	prompts   int           // UserPromptSubmit hooks seen
	confirmed bool          // a web message was seen submitted since Claude Code started
	wake      chan struct{} // a hook asks for a transcript sync now
	cwd       string
	live      *store.LiveStatus // from the status line
	viewedAt  time.Time         // the web last showed this session (polling with viewing=1)
	away      bool              // Claude Code was told nobody looks (focus out)
	started   time.Time         // tasks recorded before this belong to an earlier Claude Code
}

type terminalQueued struct {
	text    string // as written to the terminal
	message store.QueuedMessage
	held    bool // not pasted yet, editable in the host queue
}

// terminalConfig is read by `cc-remote terminal-hook` (CC_REMOTE_TERMINAL_FILE).
type terminalConfig struct {
	Host    string `json:"host"`
	Token   string `json:"token"`
	Session string `json:"session"`
	// StatusCommand is the user's own status line, run by terminal-status.
	StatusCommand string `json:"statusCommand,omitempty"`
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
			return nil, i18n.Errorf("Claude Code just exited; try again shortly", "Claude Code 刚刚退出，请稍后重试")
		default:
			return ts, nil
		}
	}
	if s.switching[x.ID] {
		return nil, fmt.Errorf("正在切换配置")
	}
	if s.closing {
		return nil, i18n.Errorf("The host is shutting down", "宿主正在关闭")
	}
	if _, err := validateCwd(i18n.Current(), x.Cwd); err != nil {
		return nil, err
	}
	if s.cfg.DataDir == "" || s.cfg.HostExecutable == "" {
		return nil, i18n.Errorf("The host has no data directory or executable path configured", "宿主没有配置数据目录或可执行文件路径")
	}
	dir := filepath.Join(s.cfg.DataDir, "terminal-config", x.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ts := &terminalSession{cleaned: make(chan struct{}), id: x.ID, claudeID: strings.ToLower(x.ClaudeSessionID), hookToken: newHookToken(), dir: dir, cwd: x.Cwd, started: time.Now(), send: make(chan string, maxQueuedMessages+1), withdraw: make(chan chan []string), wake: make(chan struct{}, 1)}
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
	opts := claude.Options{PermissionMode: p.PermissionMode, Model: p.Model, Effort: p.Effort}
	if err := profileOptions(x, &opts); err != nil {
		return nil, err
	}
	args = append(args, claude.LaunchFlags(opts.PermissionMode, opts.Model, opts.Effort)...)
	args = append(args, x.ClaudeArgs...)
	env := claude.ProfileEnvironment(terminal.CleanEnv(os.Environ()), opts.ProfileEnv)
	env = append(env, "CC_REMOTE_ASK_FILE="+filepath.Join(dir, "ask.json"), "CC_REMOTE_TERMINAL_FILE="+filepath.Join(dir, "terminal.json"))
	if s.cfg.ClaudeConfigEnv != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+s.cfg.ClaudeConfigEnv)
	}
	exe := s.cfg.ClaudeExecutable
	if exe == "" {
		exe = "claude"
	}
	// The user confirmed trusting the directory when creating the session.
	r, err := terminal.Start(terminal.Options{Executable: exe, Args: args, Dir: x.Cwd, Env: env, TrustFolder: true, AwaitStart: true})
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	ts.runner = r
	ts.conn = &channelConnection{sessionID: x.ID, seen: time.Now(), keyPrefix: ts.claudeID + ":"}
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

func newHookToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail
	}
	return hex.EncodeToString(b)
}

// hookRequest reports whether r is a running terminal session's hook calling
// one of its own endpoints with the session's hook token.
func (s *Server) hookRequest(r *http.Request) bool {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
	if r.Method != http.MethodPost || len(parts) != 3 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var ts *terminalSession
	switch {
	case parts[0] == "terminals" && (parts[2] == "hook" || parts[2] == "status"):
		ts = s.terminals[parts[1]]
	case parts[0] == "channels" && parts[2] == "interactions":
		for _, t := range s.terminals {
			if t.claudeID == strings.ToLower(parts[1]) {
				ts = t
				break
			}
		}
	}
	return ts != nil && equalToken(r.Header.Get("Authorization"), ts.hookToken)
}

// writeTerminalConfig writes the hook settings and the files the hooks read.
func (s *Server) writeTerminalConfig(ts *terminalSession) error {
	command := func(arg string) map[string]any {
		return map[string]any{"type": "command", "command": s.cfg.HostExecutable, "args": []string{arg}}
	}
	ask := command("channel-ask-hook")
	ask["timeout"] = claude.AskHookTimeout
	state := []any{map[string]any{"hooks": []any{command("terminal-hook")}}}
	config := map[string]any{"hooks": map[string]any{
		"PermissionRequest": []any{map[string]any{"matcher": "*", "hooks": []any{ask}}},
		"SessionStart":      state,
		"UserPromptSubmit":  state,
		"Stop":              state,
		"Notification":      state,
		"SubagentStop":      state,
		"PostToolUse":       state,
	}}
	// The status line reports model, context and cost to the host, then shows
	// the user's own status line unchanged.
	own := userStatusLine(s.cfg.ClaudeConfigDir, ts.cwd)
	status := map[string]any{"type": "command", "command": `"` + filepath.ToSlash(s.cfg.HostExecutable) + `" terminal-status`}
	if p, ok := own["padding"]; ok {
		status["padding"] = p
	}
	config["statusLine"] = status
	x, e := s.store.Get(ts.id)
	if e != nil {
		return e
	}
	p, e := sessionProfile(x)
	if e != nil {
		return e
	}
	if p.ID != "" {
		config["env"] = claude.SupplierEnv(p.Settings.Env)
	}
	settings, _ := json.Marshal(config)
	host := s.hookHost()
	askFile, _ := json.Marshal(map[string]string{"host": host, "token": ts.hookToken, "sessionId": ts.claudeID, "bindingFile": ""})
	ownCommand, _ := own["command"].(string)
	termFile, _ := json.Marshal(terminalConfig{Host: host, Token: ts.hookToken, Session: ts.id, StatusCommand: ownCommand})
	for name, data := range map[string][]byte{"settings.json": settings, "ask.json": askFile, "terminal.json": termFile} {
		if err := os.WriteFile(filepath.Join(ts.dir, name), data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// userStatusLine is the status line Claude Code would use without
// cc-remote's settings: the project's local, then project, then user setting.
func userStatusLine(configDir, cwd string) map[string]any {
	if configDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			configDir = filepath.Join(home, ".claude")
		}
	}
	for _, path := range []string{filepath.Join(cwd, ".claude", "settings.local.json"), filepath.Join(cwd, ".claude", "settings.json"), filepath.Join(configDir, "settings.json")} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var settings struct {
			StatusLine map[string]any `json:"statusLine"`
		}
		if json.Unmarshal(raw, &settings) == nil && settings.StatusLine != nil {
			if settings.StatusLine["type"] == "command" {
				return settings.StatusLine
			}
			return nil
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
			// The web asked before switching (the prompt cache no longer
			// applies); Claude Code's own "Switch model?" would block the terminal.
			if strings.HasPrefix(text, "/model ") {
				ts.runner.ConfirmDialog("Switch model?", 3*time.Second)
			}
			if first && !strings.HasPrefix(text, "/") { // local commands run no UserPromptSubmit
				s.confirmFirstPrompt(ts, text, before)
			}
		case reply := <-ts.withdraw:
			reply <- s.withdrawQueued(ts)
		case <-ts.runner.Done():
			return
		}
	}
}

// withdrawQueued takes the web's queued messages back for editing, as pressing
// Up does in the terminal: messages not typed yet leave the send queue, and
// Claude Code's own queue moves into its input box (Up) and is cleared
// (Ctrl+U per line). The running turn goes on. Run by the writer, so no
// message is being typed meanwhile.
func (s *Server) withdrawQueued(ts *terminalSession) []string {
	var commands []string
	for drained := false; !drained; {
		select {
		case text := <-ts.send:
			if strings.HasPrefix(text, "/") {
				commands = append(commands, text)
			}
		default:
			drained = true
		}
	}
	s.mu.Lock()
	var texts []string
	lines := 0
	for _, q := range ts.queued {
		texts = append(texts, q.text)
		if !q.held {
			lines += strings.Count(q.text, "\n") + 1
		}
	}
	ts.queued = nil
	s.mu.Unlock()
	if lines > 0 {
		// Clear a draft first: Up puts the queue in front of it. Ctrl+U deletes a
		// line (an empty input ignores it); Up with nothing queued recalls the
		// previous prompt, which the clearing removes again.
		clear := bytes.Repeat([]byte{0x15}, 2*lines+4)
		_ = ts.runner.Keys(clear[:4])
		_ = ts.runner.Keys([]byte("\x1b[A"))
		time.Sleep(300 * time.Millisecond)
		_ = ts.runner.Keys(clear)
	}
	return append(texts, commands...)
}

// terminalWithdrawLocked is POST /api/sessions/{id}/queue/withdraw for a
// terminal session; it unlocks s.mu while the writer works.
func (s *Server) terminalWithdrawLocked(w http.ResponseWriter, r *http.Request, ts *terminalSession) {
	l := s.lang(r)
	if x, err := s.store.Get(ts.id); err == nil && x.Status == "waiting" {
		s.mu.Unlock()
		fail(w, 409, l.T("Answer the open dialog first", "请先处理当前的确认对话框"))
		return
	}
	for _, p := range s.requests {
		if p.connection == ts.conn {
			s.mu.Unlock()
			fail(w, 409, l.T("Answer the open question first", "请先回答当前的提问"))
			return
		}
	}
	s.mu.Unlock()
	reply := make(chan []string, 1)
	select {
	case ts.withdraw <- reply:
	case <-ts.runner.Done():
		fail(w, 409, l.T("Claude Code is not running", "Claude Code 没有运行"))
		return
	case <-time.After(10 * time.Second):
		fail(w, 409, l.T("A message is still being typed; try again", "仍有消息正在输入，请稍后再试"))
		return
	}
	texts := <-reply
	respond(w, 200, map[string]string{"text": strings.Join(texts, "\n\n")})
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
// UserPromptSubmit, the transcript showing it or Claude working, it presses
// Enter if the text sits in the input box, or pastes it again when the box is
// empty or holds part of it. After firstPromptTries the message leaves the
// queue and the web is told, rather than showing it queued forever.
func (s *Server) confirmFirstPrompt(ts *terminalSession, text string, before int) {
	submitted := func(wait time.Duration) bool {
		for end := time.Now().Add(wait); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
			if isDone(ts.runner.Done()) {
				return true
			}
			s.mu.Lock()
			n, waiting := ts.prompts, ts.queuedLocked(text)
			s.mu.Unlock()
			if n > before || !waiting || ts.runner.Activity() != "" {
				return true
			}
		}
		return false
	}
	for try := 1; !submitted(firstPromptWait); try++ {
		box, shown := ts.runner.Input()
		if try == firstPromptTries {
			s.firstPromptFailed(ts, text, box)
			return
		}
		switch {
		case ts.runner.Shows(text):
			log.Printf("terminal %s: first message not submitted (try %d); pressing Enter", ts.id, try)
			_ = ts.runner.Submit()
		case shown && (box == "" || strings.Contains(flatText(text), flatText(box))):
			log.Printf("terminal %s: first message lost while Claude Code started (try %d); sending again", ts.id, try)
			if box != "" {
				_ = ts.runner.ClearInput(strings.Count(box, "\n") + 1)
				time.Sleep(200 * time.Millisecond)
			}
			_ = ts.runner.Paste(text)
		default: // a dialog, or someone typing in a terminal: leave it alone
			log.Printf("terminal %s: first message not submitted (try %d); the input box is not free", ts.id, try)
		}
	}
	s.mu.Lock()
	ts.confirmed = true
	s.mu.Unlock()
}

// firstPromptFailed gives up on a first message that never got submitted: it
// is cleared from the input box (so it cannot run later unnoticed), leaves the
// queue, and the web shows it in an error to send again. The screen goes to
// the log to tell why.
func (s *Server) firstPromptFailed(ts *terminalSession, text, box string) {
	log.Printf("terminal %s: first message was not submitted; giving up. Screen:\n%s", ts.id, ts.runner.Screen())
	if box != "" && (ts.runner.Shows(text) || strings.Contains(flatText(text), flatText(box))) {
		_ = ts.runner.ClearInput(strings.Count(box, "\n") + 1)
	}
	s.mu.Lock()
	message, ok := ts.popQueuedLocked(text)
	s.mu.Unlock()
	if !ok {
		return // taken back meanwhile, or matched in the transcript after all
	}
	quoted := "> " + strings.ReplaceAll(strings.TrimSpace(message.Text), "\n", "\n> ")
	_, _ = s.store.Append(ts.id, "error", i18n.T("This message did not reach Claude Code. Attach a terminal (ccr) to see what it shows, or send it again:", "这条消息没有送达 Claude Code。可以接入终端（ccr）查看它当前的状态，或重新发送：")+"\n\n"+quoted, nil)
}

// flatText is text without whitespace, for comparing it with the screen.
func flatText(s string) string { return strings.Join(strings.Fields(s), "") }

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
			s.updateFocus(ts)
			s.watchDialogs(ts)
		case <-ts.wake:
		}
		s.syncTerminal(ts)
	}
}

// dialogGrace is how long an approval's dialog must be off the screen before
// the web card is withdrawn: a redraw, or the next queued dialog, comes sooner.
const dialogGrace = 2 * time.Second

// watchDialogs withdraws the web card of a main-agent tool approval once its
// dialog was on the terminal's screen and left it: the terminal answered.
// Approvals of subagents (whose dialogs may wait behind the main screen) are
// left to their hooks.
func (s *Server) watchDialogs(ts *terminalSession) {
	shown := dialogShown(ts.runner.Screen())
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.requests {
		if p.connection == ts.conn && p.onScreen && p.dialogStep(shown, now) {
			s.finishSharedLocked(id, p, "terminal")
		}
	}
}

// dialogShown reports whether a dialog is at the bottom of the screen, where
// its footer is ("Esc to cancel"): the same words in Claude's output above it
// do not count.
func dialogShown(screen string) bool {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	lines = lines[max(0, len(lines)-dialogFooterLines):]
	return strings.Contains(strings.ToLower(flatText(strings.Join(lines, " "))), "esctocancel")
}

// dialogFooterLines is how far up from the bottom the footer may be: under it
// come the input hints and status line.
const dialogFooterLines = 8

// dialogStep advances an approval's view of its dialog and reports whether
// the terminal answered: the dialog was shown and has been gone dialogGrace.
func (p *pending) dialogStep(shown bool, now time.Time) bool {
	switch {
	case shown:
		p.dialogSeen, p.dialogGone = true, time.Time{}
	case !p.dialogSeen:
	case p.dialogGone.IsZero():
		p.dialogGone = now
	default:
		return now.Sub(p.dialogGone) >= dialogGrace
	}
	return false
}

// webViewWindow is how long after the web's last poll a session counts as viewed.
const webViewWindow = 20 * time.Second

// updateFocus reports focus to Claude Code: in while a terminal front end is
// attached or the web shows the session, out when nobody does.
func (s *Server) updateFocus(ts *terminalSession) {
	s.mu.Lock()
	watched := ts.runner.Viewers() > 0 || time.Since(ts.viewedAt) < webViewWindow
	change := watched == ts.away
	ts.away = !watched
	s.mu.Unlock()
	if change {
		ts.runner.Focus(watched)
	}
}

// markViewed records that the web shows a session (its active tab polls with viewing=1).
func (s *Server) markViewed(r *http.Request, id string) {
	if r.URL.Query().Get("viewing") != "1" {
		return
	}
	s.mu.Lock()
	ts := s.terminals[id]
	if ts != nil {
		ts.viewedAt = time.Now()
	}
	s.mu.Unlock()
	if ts != nil {
		s.updateFocus(ts)
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
	// Messages are pasted in order: once one shows, those before it went in
	// too, even if their text does not match (it must not hold up the rest).
	for i := ts.scan; i < len(users) && len(ts.queued) > 0; i++ {
		for j, q := range ts.queued {
			if !q.held && users[i] == normalizePrompt(q.text) {
				// Older native pastes are consumed too, but host-held
				// messages before an explicitly promoted item have not run.
				remaining := make([]terminalQueued, 0, len(ts.queued))
				for _, before := range ts.queued[:j] {
					if before.held {
						remaining = append(remaining, before)
					}
				}
				ts.queued = append(remaining, ts.queued[j+1:]...)
				ts.scan = i + 1
				break
			}
		}
	}
	if x, err := s.store.Get(ts.id); err == nil && x.Status != "running" && x.Status != "waiting" {
		s.drainTerminalQueueLocked(ts)
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
func (ts *terminalSession) popQueuedLocked(prompt string) (store.QueuedMessage, bool) {
	want := normalizePrompt(prompt)
	for i, q := range ts.queued {
		if !q.held && normalizePrompt(q.text) == want {
			ts.queued = append(ts.queued[:i:i], ts.queued[i+1:]...)
			return q.message, true
		}
	}
	return store.QueuedMessage{}, false
}

// queuedLocked reports whether a web message still waits to be submitted.
func (ts *terminalSession) queuedLocked(prompt string) bool {
	want := normalizePrompt(prompt)
	for _, q := range ts.queued {
		if !q.held && normalizePrompt(q.text) == want {
			return true
		}
	}
	return false
}

// normalizePrompt is a prompt for matching: a long paste comes back from
// Claude Code wrapped in <pasted_content> tags.
// normalizePrompt is a prompt as the transcript shows it: pasted text unwrapped
// and the attachment list left out (it shows as attachments).
func normalizePrompt(s string) string {
	text, _ := claude.SplitAttachments(claude.UnwrapPastes(strings.ReplaceAll(s, "\r\n", "\n")))
	return strings.TrimSpace(text)
}

func (s *Server) terminalExited(ts *terminalSession) {
	if ts.cleaned != nil {
		defer close(ts.cleaned)
	}
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
		// These messages never left the host. Preserve text and attachments
		// on crashes and normal shutdown alike; do not replay them on resume.
		held := 0
		for _, q := range ts.queued {
			if q.held {
				_, _ = s.store.Append(ts.id, "user", q.message.Text, q.message.Payload)
				held++
			}
		}
		if held > 0 {
			_, _ = s.store.Append(ts.id, "error", i18n.T("The queued messages above were not executed because the terminal closed. They were preserved and will not be resent automatically.", "终端已关闭，以上待发送消息未执行。内容已保留，不会自动重复发送。"), nil)
		}
		if !ts.closing {
			note := i18n.T("Claude Code exited", "Claude Code 已退出")
			if len(ts.queued) > 0 {
				note += i18n.T("; the queued messages above did not run, send them again", "，以上排队消息未执行，请重新发送")
			}
			if err := ts.runner.Err(); err != nil {
				note += i18n.F(" (%s)", "（%s）", err.Error())
			}
			_, _ = s.store.Append(ts.id, "error", note+i18n.T(". The session resumes with your next message.", "。下次发送消息时会恢复会话。"), nil)
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
// It answers the request, and says what it answered when the message was taken.
func (s *Server) terminalMessageLocked(w http.ResponseWriter, r *http.Request, x store.Session, prompt string, message store.QueuedMessage) (string, bool) {
	ts, err := s.terminalLocked(x)
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not start Claude Code: ", "无法启动 Claude Code：")+err.Error())
		return "", false
	}
	if len(ts.queued) >= maxQueuedMessages {
		fail(w, 429, s.lang(r).T("Too many queued messages; wait for Claude to process them", "排队的消息过多，请等待 Claude 处理"))
		return "", false
	}
	status := "queued"
	if latest, e := s.store.Get(x.ID); e == nil && latest.Status != "running" && latest.Status != "waiting" && len(ts.queued) == 0 {
		status = "running"
	}
	prompt = terminal.CleanText(prompt)
	if message.ID != "" && status == "queued" && !strings.HasPrefix(prompt, "/") {
		message.Editable, message.CanSendNow = true, true
		ts.queued = append(ts.queued, terminalQueued{text: prompt, message: message, held: true})
		respond(w, 202, map[string]string{"status": status})
		return status, true
	}
	select {
	case ts.send <- prompt:
	default:
		fail(w, 429, s.lang(r).T("Too many queued messages; wait for Claude to process them", "排队的消息过多，请等待 Claude 处理"))
		return "", false
	}
	// Local commands (/compact, /clear) never show up as a prompt: not queued.
	if !strings.HasPrefix(prompt, "/") {
		ts.queued = append(ts.queued, terminalQueued{text: prompt, message: message})
	}
	respond(w, 202, map[string]string{"status": status})
	return status, true
}

// terminalStopLocked presses Esc; a second stop soon after presses Ctrl+C.
func (s *Server) terminalStopLocked(w http.ResponseWriter, r *http.Request, id string) {
	ts := s.terminals[id]
	if ts == nil {
		fail(w, 409, s.lang(r).T("Claude Code is not running", "Claude Code 没有运行"))
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
		s.drainTerminalQueueLocked(ts)
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
		fail(w, 400, s.lang(r).T("Invalid hook payload", "无效的 hook 内容"))
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	ts := s.terminals[id]
	if ts == nil {
		s.mu.Unlock()
		fail(w, 404, s.lang(r).T("The terminal session is not running", "终端会话没有运行"))
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
			// /clear leaves no mark in either transcript: show where the context restarted.
			if req.Source == "clear" {
				_, _ = s.store.Append(id, "compact", "", json.RawMessage(`{"clear":true}`))
			}
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
		// While Claude works, the hook runs as a message joins Claude Code's own
		// queue: it is still queued (and can be withdrawn) until the transcript
		// shows it taken in (syncTerminal).
		if x, err := s.store.Get(id); err == nil && (x.Status == "running" || x.Status == "waiting") {
			break
		}
		ts.popQueuedLocked(req.Prompt)
		_ = s.store.SetStatus(id, "running")
	case "PostToolUse":
		// A dialog answered in the terminal: the tool ran.
		if x, err := s.store.Get(id); err == nil && x.Status == "waiting" {
			_ = s.store.SetStatus(id, "running")
		}
	case "Stop":
		if ts.runner.Viewers() > 0 {
			go func() { // clears what a terminal drew differently from ConPTY
				time.Sleep(500 * time.Millisecond)
				ts.runner.Redraw()
			}()
		}
		_ = s.store.SetStatus(id, "completed")
		s.drainTerminalQueueLocked(ts)
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
		_, _ = s.store.Append(id, "error", i18n.T("The terminal switched to a Claude session another web session is using; the web does not follow it. Go back to the original session or continue in that one.", "终端里切换到了另一个网页会话正在使用的 Claude 会话，网页不会跟随；请回到原会话或在该会话中继续。"), nil)
		return false
	}
	if c := s.channels[next]; c != nil && c != ts.conn {
		return false
	}
	return true
}

// terminalStatus receives Claude Code's status line data from
// `cc-remote terminal-status`.
func (s *Server) terminalStatus(w http.ResponseWriter, r *http.Request) {
	if !s.channelLocal(w, r) {
		return
	}
	var in struct {
		Model struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"model"`
		Cost struct {
			TotalCostUSD    float64 `json:"total_cost_usd"`
			TotalDurationMS int64   `json:"total_duration_ms"`
		} `json:"cost"`
		Context struct {
			Size           int     `json:"context_window_size"`
			UsedPercentage float64 `json:"used_percentage"`
			Current        struct {
				Input         int `json:"input_tokens"`
				CacheCreation int `json:"cache_creation_input_tokens"`
				CacheRead     int `json:"cache_read_input_tokens"`
			} `json:"current_usage"`
		} `json:"context_window"`
		Effort any `json:"effort"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, 400, s.lang(r).T("Invalid status payload", "无效的状态内容"))
		return
	}
	effort, _ := in.Effort.(string)
	if m, ok := in.Effort.(map[string]any); ok {
		effort, _ = m["level"].(string)
	}
	live := &store.LiveStatus{
		Model:          in.Model.DisplayName,
		ModelID:        in.Model.ID,
		Effort:         effort,
		CostUSD:        in.Cost.TotalCostUSD,
		DurationMS:     in.Cost.TotalDurationMS,
		ContextTokens:  in.Context.Current.Input + in.Context.Current.CacheCreation + in.Context.Current.CacheRead,
		ContextSize:    in.Context.Size,
		ContextPercent: in.Context.UsedPercentage,
	}
	s.mu.Lock()
	if ts := s.terminals[r.PathValue("id")]; ts != nil {
		ts.live = live
	}
	s.mu.Unlock()
	respond(w, 200, map[string]string{"status": "ok"})
}

// statusExited closes a web terminal stream when Claude Code exited.
const statusExited websocket.StatusCode = 4001

// streamPing keeps a quiet terminal stream alive through proxies that close
// idle connections (nginx after 60 s by default); Server.streamPing overrides it.
const streamPing = 30 * time.Second

// streamTicketTTL is how long the web has to open the stream it asked a ticket for.
const streamTicketTTL = 30 * time.Second

// streamTicket reports whether r opens a terminal stream with a ticket, which
// terminalStream checks in place of the token.
func streamTicket(r *http.Request) bool {
	id, ok := strings.CutPrefix(r.URL.Path, "/api/terminals/")
	id, ok2 := strings.CutSuffix(id, "/stream")
	return ok && ok2 && id != "" && !strings.Contains(id, "/") && r.URL.Query().Get("ticket") != ""
}

// terminalTicket is POST /api/terminals/{id}/ticket: a ticket the web opens the
// session's terminal stream with, once, as /api/terminals/{id}/stream?ticket=….
func (s *Server) terminalTicket(w http.ResponseWriter, r *http.Request) {
	x, err := s.store.Get(r.PathValue("id"))
	if err != nil || x.Mode != "terminal" {
		fail(w, 404, s.lang(r).T("Terminal session not found", "终端会话不存在"))
		return
	}
	ticket := s.streamTickets.add(downloadTicket{session: x.ID, expires: time.Now().Add(streamTicketTTL)})
	respond(w, 200, map[string]string{"ticket": ticket})
}

// terminalStream connects a terminal front end: binary frames carry keystrokes
// and screen output; a text frame {"type":"resize","cols":..,"rows":..} sets the size.
// A web front end (opened with a ticket) also gets {"type":"size","cols":..,"rows":..,"held":..}
// whenever the size or held changes, and is not counted as an attached terminal.
// It may resize only while no terminal is attached (held false): a terminal
// keeps its size.
func (s *Server) terminalStream(w http.ResponseWriter, r *http.Request) {
	web := false
	if ticket := r.URL.Query().Get("ticket"); ticket != "" {
		t, ok := s.streamTickets.take(ticket)
		if !ok || t.session != r.PathValue("id") {
			fail(w, 401, s.lang(r).T("The terminal link has expired; open the terminal again", "终端链接已过期，请重新打开终端"))
			return
		}
		web = true
	}
	x, err := s.store.Get(r.PathValue("id"))
	if err != nil || x.Mode != "terminal" {
		fail(w, 404, s.lang(r).T("Terminal session not found", "终端会话不存在"))
		return
	}
	s.mu.Lock()
	ts, err := s.terminalLocked(x)
	s.mu.Unlock()
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not start Claude Code: ", "无法启动 Claude Code：")+err.Error())
		return
	}
	// The handler's middleware already checked the Origin, allowing a reverse
	// proxy's public host and --allow-origin sites, which the library's own check refuses.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	subscribe := ts.runner.Subscribe
	if web {
		subscribe = ts.runner.SubscribeWeb
	}
	sub := subscribe()
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
				_ = ts.runner.KeysFrom(sub, data)
				continue
			}
			var m struct {
				Type       string `json:"type"`
				Cols, Rows int
			}
			if json.Unmarshal(data, &m) == nil && m.Type == "redraw" {
				go func() {
					time.Sleep(150 * time.Millisecond) // after Claude Code handled the key
					ts.runner.Redraw()
				}()
				continue
			}
			if json.Unmarshal(data, &m) == nil && m.Type == "resize" {
				_ = ts.runner.ResizeFrom(sub, m.Cols, m.Rows)
				select {
				case sized <- struct{}{}:
				default:
				}
			}
		}
	}()
	every := streamPing
	if s.streamPing > 0 {
		every = s.streamPing
	}
	go func() {
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				// Ping waits for the pong, which the reading goroutine above receives.
				pctx, done := context.WithTimeout(ctx, every)
				err := c.Ping(pctx)
				done()
				if err != nil {
					cancel()
					return
				}
			case <-ctx.Done():
				return
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
				s.mu.Lock()
				closing := s.closing
				s.mu.Unlock()
				if closing { // the host stops: attach waits and attaches again
					_ = c.Close(websocket.StatusGoingAway, "host restarting")
					return
				}
				reason, status := i18n.T("Disconnected", "已断开"), websocket.StatusNormalClosure
				if ts.runner.Exited() {
					reason = i18n.T("Claude Code exited", "Claude Code 已退出")
					if web { // the web reconnects after any other close
						status = statusExited
					}
				}
				_ = c.Close(status, reason)
				return
			}
			if err := c.Write(ctx, websocket.MessageBinary, chunk); err != nil {
				return
			}
		case size := <-sub.Size: // nil, never ready, for a terminal
			b, _ := json.Marshal(map[string]any{"type": "size", "cols": size.Cols, "rows": size.Rows, "held": size.Held})
			if err := c.Write(ctx, websocket.MessageText, b); err != nil {
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
	x.Attached = ts.runner.Attached()
	live := store.LiveStatus{}
	if ts.live != nil {
		live = *ts.live
	}
	if x.Status == "running" {
		live.Activity = ts.runner.Activity()
	} else if len(ts.queued) == 0 {
		live.Suggestion = ts.runner.Suggestion()
	}
	if ts.live != nil || live.Activity != "" || live.Suggestion != "" {
		x.Live = &live
	}
	if len(ts.queued) > 0 {
		x.Queued = make([]store.QueuedMessage, len(ts.queued))
		for i, q := range ts.queued {
			x.Queued[i] = q.message
		}
	}
}

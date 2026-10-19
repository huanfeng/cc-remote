package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/store"

	"github.com/coder/websocket"
	"golang.org/x/term"
)

// The server test binary doubles as a fake interactive Claude Code for
// terminal sessions (CC_REMOTE_FAKE_TERMINAL=1): in raw mode it answers each
// pasted message by writing a transcript turn and calling the hooks, logs
// Esc / Ctrl+C, and echoes other keys to the screen.
func TestMain(m *testing.M) {
	if os.Getenv("CC_REMOTE_FAKE_TERMINAL") == "1" {
		fakeTerminal()
		return
	}
	os.Exit(m.Run())
}

func fakeTerminal() {
	if old, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), old)
	}
	id := ""
	for i, a := range os.Args {
		if (a == "--session-id" || a == "--resume") && i+1 < len(os.Args) {
			id = os.Args[i+1]
		}
	}
	dir := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "fake")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "args.log"), []byte(strings.Join(os.Args[1:], " ")), 0o600)
	for _, a := range os.Args {
		if _, err := os.Stat(filepath.Join(dir, id+".jsonl")); a == "--resume" && err != nil {
			os.Stdout.WriteString("No conversation found\r\n") // as Claude Code does
			return
		}
	}
	logKey := func(k string) {
		f, _ := os.OpenFile(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "keys.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		fmt.Fprintln(f, k)
		f.Close()
	}
	hook := func(event string, extra map[string]string) {
		raw, err := os.ReadFile(os.Getenv("CC_REMOTE_TERMINAL_FILE"))
		if err != nil {
			return
		}
		var cfg struct{ Host, Token, Session string }
		_ = json.Unmarshal(raw, &cfg)
		body := map[string]string{"hook_event_name": event, "session_id": id}
		for k, v := range extra {
			body[k] = v
		}
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", cfg.Host+"/api/terminals/"+cfg.Session+"/hook", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
		if resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req); err == nil {
			resp.Body.Close()
		}
	}
	turn := func(text string) {
		f, _ := os.OpenFile(filepath.Join(dir, id+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		user, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"content": text}})
		reply, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "echo: " + text}}}})
		f.Write(append(append(user, '\n'), append(reply, '\n')...))
		f.Close()
	}
	// The input box as Claude Code draws it: a "❯" row between two rules.
	rule := strings.Repeat("─", 40)
	box := func(text string) {
		os.Stdout.WriteString("\x1b[3;1H\x1b[K❯ " + strings.ReplaceAll(text, "\n", " "))
	}
	os.Stdout.WriteString("\x1b[H\x1b[2Jfake claude ready\r\n" + rule + "\r\n❯ \r\n" + rule + "\r\n")
	hook("SessionStart", map[string]string{"source": "startup"}) // as Claude Code does on start
	buf := make([]byte, 8192)
	var pending []byte
	var pasted string
	ctrlC := 0
	dropped := false
	ignored := false
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 {
			switch {
			case bytes.HasPrefix(pending, []byte("\x1b[200~")):
				end := bytes.Index(pending, []byte("\x1b[201~"))
				if end < 0 {
					goto more
				}
				pasted = string(pending[6:end])
				pending = pending[end+6:]
				if os.Getenv("CC_REMOTE_FAKE_DROP_FIRST") == "1" && !dropped {
					dropped = true // input typed while Claude Code starts is lost
					pasted = ""
					continue
				}
				box(pasted)
			case pending[0] == '\r':
				pending = pending[1:]
				// The text stays in the box: the first Enter, or every one, is not taken.
				if pasted != "" && (os.Getenv("CC_REMOTE_FAKE_DEAF") == "1" || os.Getenv("CC_REMOTE_FAKE_IGNORE_ENTER") == "1" && !ignored) {
					ignored = true
					logKey("ENTER-IGNORED")
					continue
				}
				text := pasted
				pasted = ""
				if text == "" {
					continue // an empty box submits nothing
				}
				box("")
				hook("UserPromptSubmit", map[string]string{"prompt": text})
				if text == "/clear" {
					id = "00000000-0000-4000-8000-00000000c1ea"
					hook("SessionStart", map[string]string{"source": "clear"})
				} else {
					turn(text)
				}
				hook("Stop", nil)
			case pending[0] == 0x1b && (len(pending) == 1 || pending[1] != '['):
				logKey("ESC")
				pending = pending[1:]
			case pending[0] == 0x03:
				logKey("CTRL-C")
				pending = pending[1:]
				if ctrlC++; ctrlC >= 2 {
					return
				}
			case pending[0] == 0x15: // Ctrl+U clears the box
				pending = pending[1:]
				if pasted != "" {
					logKey("CLEAR")
				}
				pasted = ""
				box("")
			case pending[0] == 0x1b:
				goto more
			default:
				os.Stdout.WriteString(fmt.Sprintf("\x1b[6;1HKEY %c\r\n", pending[0]))
				pending = pending[1:]
			}
		}
	more:
	}
}

func setupTerminal(t *testing.T) (*Server, string, *store.Store, func(string) string) {
	t.Helper()
	s, ts, st := setup(t, noRun)
	exe, _ := os.Executable()
	s.cfg.ClaudeExecutable = exe
	s.cfg.HostExecutable = exe
	s.cfg.DataDir = t.TempDir()
	t.Setenv("CC_REMOTE_FAKE_TERMINAL", "1")
	keys := func(string) string {
		b, _ := os.ReadFile(filepath.Join(s.cfg.ClaudeConfigDir, "keys.log"))
		return string(b)
	}
	return s, ts.URL, st, keys
}

func createTerminal(t *testing.T, base string) store.Session {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"title": "term", "cwd": t.TempDir(), "mode": "terminal", "trustFolder": true})
	req, _ := http.NewRequest("POST", base+"/api/sessions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var x store.Session
	if resp.StatusCode != 201 || json.NewDecoder(resp.Body).Decode(&x) != nil || x.Mode != "terminal" {
		t.Fatalf("create terminal session: %d %+v", resp.StatusCode, x)
	}
	return x
}

func call(t *testing.T, base, method, path string, body any) (int, []byte) {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req, _ := http.NewRequest(method, base+path, bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

// awaitLong is await with room for a process to start and paste.
func awaitLong(t *testing.T, what func() string, fn func() bool) {
	t.Helper()
	for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if fn() {
			return
		}
	}
	t.Fatalf("condition timed out: %s", what())
}

func eventTexts(st *store.Store, id string) []string {
	events, _ := st.Events(id, 0)
	var out []string
	for _, e := range events {
		if e.Kind == "user" || e.Kind == "assistant" {
			out = append(out, e.Kind+":"+e.Text)
		}
	}
	return out
}

// A terminal session needs the trust confirmation; web messages reach Claude
// Code as one paste, enter the history from the transcript, and drive the
// status through the hooks; stop presses Esc, then Ctrl+C.
func TestTerminalSessionMessagesStatusAndStop(t *testing.T) {
	s, base, st, keys := setupTerminal(t)
	if code, _ := call(t, base, "POST", "/api/sessions", map[string]any{"title": "x", "cwd": t.TempDir(), "mode": "terminal"}); code != 400 {
		t.Fatalf("terminal session without trust: %d", code)
	}
	x := createTerminal(t, base)
	if code, b := call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "hello\nworld"}); code != 202 {
		t.Fatalf("send: %d %s", code, b)
	}
	awaitLong(t, func() string { return fmt.Sprintf("%q keys=%q", eventTexts(st, x.ID), keys("")) }, func() bool {
		return strings.Join(eventTexts(st, x.ID), "|") == "user:hello\nworld|assistant:echo: hello\nworld"
	})
	awaitLong(t, func() string { y, _ := st.Get(x.ID); return y.Status }, func() bool { y, _ := st.Get(x.ID); return y.Status == "completed" })
	s.mu.Lock()
	queued := len(s.terminals[x.ID].queued)
	s.mu.Unlock()
	if queued != 0 {
		t.Fatalf("message still queued after the transcript showed it")
	}
	code, b := call(t, base, "GET", "/api/sessions", nil)
	if code != 200 || !strings.Contains(string(b), `"online":true`) {
		t.Fatalf("list: %s", b)
	}
	if code, _ := call(t, base, "POST", "/api/sessions/"+x.ID+"/stop", nil); code != 200 {
		t.Fatalf("stop: %d", code)
	}
	await(t, func() bool { return strings.Contains(keys(""), "ESC") })
	// An interrupted turn runs no Stop hook: stopping a running turn marks it interrupted.
	_ = st.SetStatus(x.ID, "running")
	s.mu.Lock() // a fresh stop (Esc), not a second one
	s.terminals[x.ID].lastStop = time.Time{}
	s.mu.Unlock()
	if code, _ := call(t, base, "POST", "/api/sessions/"+x.ID+"/stop", nil); code != 200 {
		t.Fatalf("stop running turn: %d", code)
	}
	if y, _ := st.Get(x.ID); y.Status != "interrupted" {
		t.Fatalf("status after stop: %s", y.Status)
	}
	_, _ = call(t, base, "POST", "/api/sessions/"+x.ID+"/stop", nil)
	await(t, func() bool { return strings.Contains(keys(""), "CTRL-C") })
}

// Commands go to Claude Code's terminal; /clear moves the session to Claude's
// new session ID and the new transcript is recorded too.
func TestTerminalSessionFollowsClear(t *testing.T) {
	s, base, st, _ := setupTerminal(t)
	x := createTerminal(t, base)
	code, b := call(t, base, "POST", "/api/sessions/"+x.ID+"/commands", map[string]string{"command": "/clear"})
	if code != 200 || !strings.Contains(string(b), `"sendMessage":true`) {
		t.Fatalf("command: %d %s", code, b)
	}
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "first"})
	await(t, func() bool { return len(eventTexts(st, x.ID)) == 2 })
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "/clear"})
	await(t, func() bool { y, _ := st.Get(x.ID); return y.ClaudeSessionID == "00000000-0000-4000-8000-00000000c1ea" })
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "first"})
	await(t, func() bool { return len(eventTexts(st, x.ID)) == 4 })
	s.mu.Lock()
	_, rebound := s.channels["00000000-0000-4000-8000-00000000c1ea"]
	s.mu.Unlock()
	if !rebound {
		t.Fatal("connection not moved to the new Claude session")
	}
	s.mu.Lock()
	queued := len(s.terminals[x.ID].queued)
	s.mu.Unlock()
	if queued != 0 {
		t.Fatalf("%d messages left queued after /clear", queued)
	}
}

// While Claude Code shows a dialog, web messages wait: pasted, their Enter
// would answer it. Control characters never reach the terminal.
func TestTerminalHoldsMessagesDuringDialog(t *testing.T) {
	_, base, st, _ := setupTerminal(t)
	x := createTerminal(t, base)
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "one"})
	awaitLong(t, func() string { return fmt.Sprintf("%q", eventTexts(st, x.ID)) }, func() bool { return len(eventTexts(st, x.ID)) == 2 })
	awaitLong(t, func() string { y, _ := st.Get(x.ID); return y.Status }, func() bool { y, _ := st.Get(x.ID); return y.Status == "completed" })
	_ = st.SetStatus(x.ID, "waiting")
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "two\x1b[201~\r\x1b[Bend"})
	time.Sleep(1500 * time.Millisecond)
	if n := len(eventTexts(st, x.ID)); n != 2 {
		t.Fatalf("message pasted while a dialog was open: %q", eventTexts(st, x.ID))
	}
	_ = st.SetStatus(x.ID, "running")
	awaitLong(t, func() string { return fmt.Sprintf("%q", eventTexts(st, x.ID)) }, func() bool {
		texts := eventTexts(st, x.ID)
		return len(texts) == 4 && texts[2] == "user:two[201~[Bend"
	})
}

// A session that never got a message has no transcript: after Claude Code
// exits it starts fresh again instead of failing to resume.
func TestTerminalRestartsWithoutTranscript(t *testing.T) {
	s, base, st, _ := setupTerminal(t)
	x := createTerminal(t, base)
	s.mu.Lock()
	y, _ := st.Get(x.ID)
	ts, err := s.terminalLocked(y)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	ts.runner.Close(2 * time.Second)
	await(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.terminals[x.ID] == nil })
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "again"})
	awaitLong(t, func() string { return fmt.Sprintf("%q", eventTexts(st, x.ID)) }, func() bool {
		return strings.Join(eventTexts(st, x.ID), "|") == "user:again|assistant:echo: again"
	})
}

// A terminal front end streams the screen and sends keys; leaving does not
// end Claude Code; deleting the session does.
func TestTerminalStreamAttachDetachAndDelete(t *testing.T) {
	s, base, _, _ := setupTerminal(t)
	x := createTerminal(t, base)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ws := "ws" + strings.TrimPrefix(base, "http") + "/api/terminals/" + x.ID + "/stream"
	conn, _, err := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + testToken}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":90,"rows":30}`))
	_ = conn.Write(ctx, websocket.MessageBinary, []byte("q"))
	var screen strings.Builder
	for !strings.Contains(screen.String(), "KEY q") {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("stream: %v (%q)", err, screen.String())
		}
		screen.Write(data)
	}
	conn.Close(websocket.StatusNormalClosure, "detach")
	time.Sleep(300 * time.Millisecond)
	s.mu.Lock()
	ts := s.terminals[x.ID]
	s.mu.Unlock()
	if ts == nil {
		t.Fatal("Claude Code ended when the front end left")
	}
	select {
	case <-ts.runner.Done():
		t.Fatal("process exited on detach")
	default:
	}
	if code, b := call(t, base, "DELETE", "/api/sessions/"+x.ID, nil); code != 204 && code != 200 {
		t.Fatalf("delete: %d %s", code, b)
	}
	select {
	case <-ts.runner.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("delete did not end Claude Code")
	}
	// Unauthenticated front ends are refused.
	if _, _, err := websocket.Dial(ctx, ws, nil); err == nil {
		t.Fatal("stream without token accepted")
	}
}

// The first message after Claude Code started is sent again when it was lost.
func TestTerminalResendsLostFirstMessage(t *testing.T) {
	old := firstPromptWait
	firstPromptWait = 2 * time.Second
	t.Cleanup(func() { firstPromptWait = old })
	t.Setenv("CC_REMOTE_FAKE_DROP_FIRST", "1")
	_, base, st, _ := setupTerminal(t)
	x := createTerminal(t, base)
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "are you there"})
	awaitLong(t, func() string { return fmt.Sprintf("%q", eventTexts(st, x.ID)) }, func() bool {
		return strings.Join(eventTexts(st, x.ID), "|") == "user:are you there|assistant:echo: are you there"
	})
}

// A first message left in the input box (its Enter not taken) is submitted.
func TestTerminalSubmitsFirstMessageLeftInBox(t *testing.T) {
	old := firstPromptWait
	firstPromptWait = 2 * time.Second
	t.Cleanup(func() { firstPromptWait = old })
	t.Setenv("CC_REMOTE_FAKE_IGNORE_ENTER", "1")
	_, base, st, keys := setupTerminal(t)
	x := createTerminal(t, base)
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "are you there"})
	awaitLong(t, func() string { return fmt.Sprintf("%q keys=%q", eventTexts(st, x.ID), keys("")) }, func() bool {
		return strings.Join(eventTexts(st, x.ID), "|") == "user:are you there|assistant:echo: are you there"
	})
	if k := keys(""); strings.Count(k, "ENTER-IGNORED") != 1 || strings.Contains(k, "CLEAR") {
		t.Fatalf("keys = %q", k)
	}
}

// A first message that never gets submitted leaves the queue and the input
// box, and the web shows it in an error instead of queued forever.
func TestTerminalGivesUpOnUnsubmittedFirstMessage(t *testing.T) {
	old := firstPromptWait
	firstPromptWait = time.Second
	t.Cleanup(func() { firstPromptWait = old })
	t.Setenv("CC_REMOTE_FAKE_DEAF", "1")
	s, base, st, keys := setupTerminal(t)
	x := createTerminal(t, base)
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "are you there\nhello"})
	errorText := func() string {
		events, _ := st.Events(x.ID, 0)
		for _, e := range events {
			if e.Kind == "error" {
				return e.Text
			}
		}
		return ""
	}
	awaitLong(t, func() string { return fmt.Sprintf("events=%q keys=%q", eventTexts(st, x.ID), keys("")) }, func() bool { return errorText() != "" })
	if text := errorText(); !strings.Contains(text, "> are you there\n> hello") {
		t.Fatalf("error = %q", text)
	}
	if k := keys(""); !strings.Contains(k, "CLEAR") {
		t.Fatalf("input box not cleared: %q", k)
	}
	s.mu.Lock()
	queued := len(s.terminals[x.ID].queued)
	s.mu.Unlock()
	if queued != 0 {
		t.Fatalf("message still queued: %d", queued)
	}
}

// The status line reports to the host and keeps running the user's own one;
// the session shows the reported model, context and cost.
func TestTerminalStatusLine(t *testing.T) {
	s, base, _, _ := setupTerminal(t)
	_ = os.MkdirAll(s.cfg.ClaudeConfigDir, 0o700)
	_ = os.WriteFile(filepath.Join(s.cfg.ClaudeConfigDir, "settings.json"), []byte(`{"statusLine":{"type":"command","command":"my-hud --x","padding":1}}`), 0o600)
	x := createTerminal(t, base)
	s.mu.Lock()
	y, _ := s.store.Get(x.ID)
	ts, err := s.terminalLocked(y)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := os.ReadFile(filepath.Join(ts.dir, "settings.json"))
	hook, _ := os.ReadFile(filepath.Join(ts.dir, "terminal.json"))
	if !strings.Contains(string(settings), `terminal-status`) || !strings.Contains(string(settings), `"padding":1`) || !strings.Contains(string(hook), `"statusCommand":"my-hud --x"`) {
		t.Fatalf("status line config: %s / %s", settings, hook)
	}
	status := `{"model":{"id":"claude-opus-5-5","display_name":"Opus 5.5"},"cost":{"total_cost_usd":1.5,"total_duration_ms":90000},"context_window":{"context_window_size":200000,"used_percentage":34,"current_usage":{"input_tokens":10,"cache_creation_input_tokens":2000,"cache_read_input_tokens":66000}}}`
	if code, b := call(t, base, "POST", "/api/terminals/"+x.ID+"/status", json.RawMessage(status)); code != 200 {
		t.Fatalf("status: %d %s", code, b)
	}
	_, b := call(t, base, "GET", "/api/sessions/"+x.ID, nil)
	if !strings.Contains(string(b), `"model":"Opus 5.5"`) || !strings.Contains(string(b), `"contextTokens":68010`) || !strings.Contains(string(b), `"contextPercent":34`) {
		t.Fatalf("live status not shown: %s", b)
	}
}

// ccr --resume: a terminal session continues a Claude Code session in its own
// directory, and resuming it again finds that session instead of a second one.
func TestTerminalResumesNativeSession(t *testing.T) {
	s, ts, _ := setup(t, nil)
	cwd := t.TempDir()
	id := store.ID()
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "resume-test")
	_ = os.MkdirAll(dir, 0700)
	user, _ := json.Marshal(map[string]any{"type": "user", "cwd": cwd, "message": map[string]string{"content": "fix the login page"}})
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), append(user, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"mode": "terminal", "trustFolder": true, "resume": strings.ToUpper(id), "cwd": t.TempDir()}
	code, b := request(t, ts, "POST", "/api/sessions", body, testToken)
	var x store.Session
	_ = json.Unmarshal(b, &x)
	if code != 201 || x.Mode != "terminal" || x.ClaudeSessionID != id || !samePathForTest(x.Cwd, cwd) {
		t.Fatalf("resume %d %s", code, b)
	}
	code, b = request(t, ts, "POST", "/api/sessions", body, testToken)
	var again store.Session
	_ = json.Unmarshal(b, &again)
	if code != 200 || again.ID != x.ID {
		t.Fatalf("second resume %d %s", code, b)
	}
	if code, b = request(t, ts, "POST", "/api/sessions", map[string]any{"mode": "terminal", "trustFolder": true, "resume": store.ID()}, testToken); code != 404 {
		t.Fatalf("unknown session %d %s", code, b)
	}
	if code, b = request(t, ts, "POST", "/api/sessions", map[string]any{"resume": id}, testToken); code != 400 {
		t.Fatalf("managed resume %d %s", code, b)
	}
}

func samePathForTest(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// ccr [claude options]: the session's Claude Code starts with them; host options are refused.
func TestTerminalClaudeArgs(t *testing.T) {
	s, base, _, _ := setupTerminal(t)
	body := map[string]any{"title": "args", "cwd": t.TempDir(), "mode": "terminal", "trustFolder": true}
	for _, bad := range [][]string{{"--print"}, {"--settings", "x.json"}, {"fix the bug"}} {
		body["claudeArgs"] = bad
		if code, b := call(t, base, "POST", "/api/sessions", body); code != 400 {
			t.Fatalf("%v accepted: %d %s", bad, code, b)
		}
	}
	body["claudeArgs"] = []string{"--add-dir", "../lib", "--dangerously-skip-permissions"}
	code, b := call(t, base, "POST", "/api/sessions", body)
	var x store.Session
	if code != 201 || json.Unmarshal(b, &x) != nil || len(x.ClaudeArgs) != 3 {
		t.Fatalf("create %d %s", code, b)
	}
	if code, b = call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "hi"}); code != 202 {
		t.Fatalf("send %d %s", code, b)
	}
	args := func() string {
		b, _ := os.ReadFile(filepath.Join(s.cfg.ClaudeConfigDir, "args.log"))
		return string(b)
	}
	awaitLong(t, args, func() bool { return strings.HasSuffix(args(), "--add-dir ../lib --dangerously-skip-permissions") })
}

// The terminal sessions running when the host stops start again with the next
// host (an update restarts it), once.
func TestTerminalSessionsRestoredAfterRestart(t *testing.T) {
	s, base, _, _ := setupTerminal(t)
	x := createTerminal(t, base)
	path := filepath.Join(s.cfg.DataDir, restoreFile)
	data, _ := json.Marshal([]string{x.ID, "missing"})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	s.RestoreTerminals()
	s.mu.Lock()
	running := s.terminals[x.ID] != nil
	s.mu.Unlock()
	if _, err := os.Stat(path); !running || !os.IsNotExist(err) {
		t.Fatalf("restored %v, list left: %v", running, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var ids []string
	if b, err := os.ReadFile(path); err != nil || json.Unmarshal(b, &ids) != nil || len(ids) != 1 || ids[0] != x.ID {
		t.Fatalf("saved %q %v", ids, err)
	}
}

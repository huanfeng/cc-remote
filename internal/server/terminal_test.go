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
	os.Stdout.WriteString("fake claude ready\r\n")
	hook("SessionStart", map[string]string{"source": "startup"}) // as Claude Code does on start
	buf := make([]byte, 8192)
	var pending []byte
	var pasted string
	ctrlC := 0
	dropped := false
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
			case pending[0] == '\r':
				pending = pending[1:]
				text := pasted
				pasted = ""
				if os.Getenv("CC_REMOTE_FAKE_DROP_FIRST") == "1" && !dropped {
					dropped = true // input typed while Claude Code starts is lost
					continue
				}
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
			case pending[0] == 0x1b:
				goto more
			default:
				os.Stdout.WriteString(fmt.Sprintf("KEY %c\r\n", pending[0]))
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

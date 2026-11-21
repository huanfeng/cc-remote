package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/store"

	"github.com/coder/websocket"
	"golang.org/x/term"
)

type codexTerminalLaunch struct {
	PID  int
	Cwd  string
	Args []string
}

func fakeCodexTerminal() {
	if old, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), old)
	}
	cwd, _ := os.Getwd()
	data, _ := json.Marshal(codexTerminalLaunch{PID: os.Getpid(), Cwd: cwd, Args: os.Args[1:]})
	_ = os.WriteFile(filepath.Join(os.Getenv("CODEX_HOME"), "launch.json"), data, 0600)
	fmt.Print("\x1b[?1049h")
	var b [1]byte
	for {
		if _, err := os.Stdin.Read(b[:]); err != nil || b[0] == 3 {
			return
		}
		if b[0] == 'q' {
			fmt.Printf("CODEX KEY %c\r\n", b[0])
		}
	}
}

func TestCodexTerminalLaunchDetachAndResume(t *testing.T) {
	s, server, st := setup(t, nil)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.CodexExecutable = exe
	s.cfg.CodexConfigDir = t.TempDir()
	s.cfg.DataDir = t.TempDir()
	t.Setenv("FAKE_CODEX_TERMINAL", "1")
	cwd := filepath.Join(t.TempDir(), "项目 with spaces")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	id := store.ID()
	dir := filepath.Join(s.cfg.CodexConfigDir, "sessions")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{
		"id": id, "cwd": cwd, "timestamp": "2026-10-24T00:00:00Z",
	}})
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-10-24T00-00-00-"+id+".jsonl"), append(meta, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	post := func(body map[string]any) (int, []byte) {
		body["provider"], body["mode"] = "codex", "terminal"
		return call(t, server.URL, http.MethodPost, "/api/sessions", body)
	}
	code, raw := post(map[string]any{"cwd": cwd, "codexArgs": []string{"-m", "my model"}, "prompt": "--version; 查"})
	var fresh store.Session
	if code != 201 || json.Unmarshal(raw, &fresh) != nil || !fresh.Online || fresh.Mode != "terminal" || fresh.ClaudeSessionID != "" {
		t.Fatalf("create Codex terminal: %d %s", code, raw)
	}
	launch := func(want []string) int {
		t.Helper()
		path := filepath.Join(s.cfg.CodexConfigDir, "launch.json")
		var got codexTerminalLaunch
		awaitLong(t, func() string { return fmt.Sprintf("%+v", got) }, func() bool {
			b, _ := os.ReadFile(path)
			return json.Unmarshal(b, &got) == nil && slices.Equal(got.Args, want)
		})
		if !samePathForTest(got.Cwd, cwd) {
			t.Fatalf("wrong working directory: %q", got.Cwd)
		}
		return got.PID
	}
	pid := launch([]string{"--no-daemon", "-m=my model", "--", "--version; 查"})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	attach := func() {
		t.Helper()
		ws := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/terminals/" + fresh.ID + "/stream?attach=1"
		c, _, err := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + testToken}}})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close(websocket.StatusNormalClosure, "detach")
		if err := c.Write(ctx, websocket.MessageBinary, []byte("q")); err != nil {
			t.Fatal(err)
		}
		var output strings.Builder
		for !strings.Contains(output.String(), "CODEX KEY q") {
			_, data, err := c.Read(ctx)
			if err != nil {
				t.Fatalf("Codex terminal: %v (%q)", err, output.String())
			}
			output.Write(data)
		}
	}
	attach()
	attach()
	if busy := s.busySessions(); !slices.Contains(busy, fresh.Title) {
		t.Fatalf("host update would interrupt the Codex terminal: %v", busy)
	}
	if again := launch([]string{"--no-daemon", "-m=my model", "--", "--version; 查"}); again != pid {
		t.Fatal("reattach replaced Codex process")
	}
	if code, b := post(map[string]any{"resume": id}); code != 409 || !strings.Contains(string(b), "ccr session attach "+fresh.ID) {
		t.Fatalf("resume while an unbound TUI runs: %d %s", code, b)
	}
	if code, b := call(t, server.URL, http.MethodPost, "/api/sessions/"+fresh.ID+"/messages", map[string]string{"text": "hi"}); code != 409 {
		t.Fatalf("sent a structured message to Codex TUI: %d %s", code, b)
	}
	if code, b := call(t, server.URL, http.MethodPost, "/api/sessions/"+fresh.ID+"/exit", nil); code != 200 {
		t.Fatalf("exit Codex terminal: %d %s", code, b)
	}
	awaitLong(t, func() string { return fresh.ID }, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.terminals[fresh.ID] == nil
	})
	ws := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/terminals/" + fresh.ID + "/stream?attach=1"
	if c, resp, err := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + testToken}}}); err == nil {
		c.CloseNow()
		t.Fatal("offline attach restarted Codex")
	} else if resp == nil || resp.StatusCode != 409 {
		t.Fatalf("offline attach: %v", err)
	}
	code, raw = post(map[string]any{"resume": strings.ToUpper(id)})
	var resumed store.Session
	if code != 201 || json.Unmarshal(raw, &resumed) != nil || !resumed.Online || resumed.ClaudeSessionID != "" {
		t.Fatalf("resume Codex terminal: %d %s", code, raw)
	}
	if stored, err := st.Get(resumed.ID); err != nil || stored.ClaudeSessionID != id {
		t.Fatalf("Codex launch identity was not stored: %+v %v", stored, err)
	}
	launch([]string{"resume", "--no-daemon", "--", id})
	if code, b := post(map[string]any{"resume": id}); code != 409 || !strings.Contains(string(b), "ccr session attach "+resumed.ID) {
		t.Fatalf("resume while a known TUI runs: %d %s", code, b)
	}
	if code, b := call(t, server.URL, http.MethodPost, "/api/native/sessions/"+id+"/import?provider=codex", map[string]bool{"confirmStopped": true}); code != 409 {
		t.Fatalf("import silently crossed terminal mode: %d %s", code, b)
	}
	if code, b := call(t, server.URL, http.MethodDelete, "/api/sessions/"+resumed.ID, nil); code != 200 {
		t.Fatalf("delete Codex terminal: %d %s", code, b)
	}
	if sessions, err := st.List(); err != nil || len(sessions) != 1 || sessions[0].ID != fresh.ID {
		t.Fatalf("unexpected sessions: %+v %v", sessions, err)
	}
}

func TestClaudeCLICreateEagerAndAttachStrict(t *testing.T) {
	s, base, _, _ := setupTerminal(t)
	offline := createTerminal(t, base)
	code, raw := call(t, base, http.MethodPost, "/api/sessions", map[string]any{
		"provider": "claude", "mode": "terminal", "cwd": t.TempDir(), "trustFolder": true, "start": true,
	})
	var online store.Session
	if code != 201 || json.Unmarshal(raw, &online) != nil || !online.Online {
		t.Fatalf("eager Claude terminal: %d %s", code, raw)
	}
	header := http.Header{"Authorization": {"Bearer " + testToken}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, x := range []store.Session{offline, online} {
		ws := "ws" + strings.TrimPrefix(base, "http") + "/api/terminals/" + x.ID + "/stream?attach=1"
		c, resp, err := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPHeader: header})
		if x.Online {
			if err != nil {
				t.Fatal(err)
			}
			c.CloseNow()
		} else if err == nil || resp == nil || resp.StatusCode != 409 {
			if c != nil {
				c.CloseNow()
			}
			t.Fatalf("offline strict attach: %v", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminals[offline.ID] != nil || s.terminals[online.ID] == nil {
		t.Fatal("strict attach unexpectedly changed the runner set")
	}
}

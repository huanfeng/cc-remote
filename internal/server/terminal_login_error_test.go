package server

import (
	"os"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/store"
	"cc-remote/internal/terminal"

	"golang.org/x/term"
)

const loginExpiredDiagnostic = "Login expired · Please run /login"

// A separate PTY child keeps this regression independent of other terminal
// dialog fixtures. Like Claude Code, it stays alive after login expires.
func TestLoginExpiredTerminalChild(t *testing.T) {
	if os.Getenv("CC_REMOTE_LOGIN_ERROR_CHILD") != "1" {
		return
	}
	if old, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), old)
	}
	drawError := func() { _, _ = os.Stdout.WriteString("\x1b[H\x1b[2J\x1b[31m" + loginExpiredDiagnostic + "\x1b[m") }
	drawError()
	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		for _, key := range buf[:n] {
			switch key {
			case 'r':
				_, _ = os.Stdout.WriteString("\x1b[H\x1b[2J✽ Thinking…")
			case 'e':
				drawError()
			case 'm':
				_, _ = os.Stdout.WriteString("\x1b[H\x1b[2J● " + terminalModelErrorMessage)
			case 3:
				return
			}
		}
	}
}

func TestTerminalLoginExpiredLifecycle(t *testing.T) {
	s, base, st := setup(t, noRun)
	x, conn := connectTerminal(t, s, st, store.ID(), t.TempDir())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner, err := terminal.Start(terminal.Options{Executable: exe, Args: []string{"-test.run=^TestLoginExpiredTerminalChild$"}, Env: append(terminal.CleanEnv(os.Environ()), "CC_REMOTE_LOGIN_ERROR_CHILD=1"), Dir: x.Cwd, Cols: 24, Rows: 12})
	if err != nil {
		t.Fatal(err)
	}
	ts := &terminalSession{id: x.ID, claudeID: x.ClaudeSessionID, conn: conn, runner: runner, started: time.Now(), turnStarted: time.Now()}
	s.mu.Lock()
	s.terminals[x.ID] = ts
	s.mu.Unlock()
	t.Cleanup(func() {
		s.mu.Lock()
		delete(s.terminals, x.ID)
		s.mu.Unlock()
		runner.Close(time.Second)
	})
	// Even before the first submission, an expired login must be visible in
	// the web's state rather than silently appearing as an idle terminal.
	if err := st.SetStatus(x.ID, "idle"); err != nil {
		t.Fatal(err)
	}
	awaitFailed := func() {
		t.Helper()
		awaitLong(t, func() string { return "expired login did not end running state" }, func() bool {
			s.syncTerminal(ts)
			current, _ := st.Get(x.ID)
			return current.Status == "failed"
		})
	}
	errors := func() int {
		events, err := st.Events(x.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, event := range events {
			if event.Kind == "error" && strings.Contains(event.Text, "Login expired") {
				if event.Text != loginExpiredDiagnostic {
					t.Fatalf("original diagnostic changed: %q", event.Text)
				}
				count++
			}
		}
		return count
	}
	awaitFailed()
	for range 3 {
		s.syncTerminal(ts)
	}
	if errors() != 1 {
		t.Fatal("login failure was not synchronized exactly once")
	}
	if code, body := request(t, base, "POST", "/api/terminals/"+x.ID+"/hook", map[string]string{"hook_event_name": "Stop"}, testToken); code != 200 {
		t.Fatalf("late Stop: %d %s", code, body)
	}
	if current, _ := st.Get(x.ID); current.Status != "failed" {
		t.Fatalf("late Stop hid expired login: %s", current.Status)
	}
	s.mu.Lock()
	s.resetTerminalFailureLocked(ts)
	_ = st.SetStatus(x.ID, "running")
	s.mu.Unlock()
	s.syncTerminal(ts)
	if current, _ := st.Get(x.ID); current.Status != "running" {
		t.Fatal("old login message overwrote a new submission")
	}
	if err := runner.Keys([]byte("r")); err != nil {
		t.Fatal(err)
	}
	awaitLong(t, func() string { return "recovered terminal did not work" }, func() bool { return runner.Activity() == "Thinking…" })
	s.syncTerminal(ts)
	if current, _ := st.Get(x.ID); current.Status != "running" {
		t.Fatalf("recovery status: %s", current.Status)
	}
	if err := runner.Keys([]byte("e")); err != nil {
		t.Fatal(err)
	}
	awaitFailed()
	if errors() != 2 {
		t.Fatal("a fresh repeated expired login was lost")
	}
	if err := runner.Keys([]byte("m")); err != nil {
		t.Fatal(err)
	}
	awaitLong(t, func() string { return "model fixture not drawn" }, func() bool {
		message, _ := runner.ModelError()
		return message == terminalModelErrorMessage
	})
	s.mu.Lock()
	s.resetTerminalFailureLocked(ts)
	ts.ignoredModelError, ts.ignoredModelVersion = "", 0
	_ = st.SetStatus(x.ID, "idle")
	s.mu.Unlock()
	s.syncTerminal(ts)
	if current, _ := st.Get(x.ID); current.Status != "idle" {
		t.Fatalf("idle model history should not become a new failure: %s", current.Status)
	}
}

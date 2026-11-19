package terminal

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProjectShellReconnectsToOriginalPTY(t *testing.T) {
	t.Setenv("CC_PROJECT_SHELL_TEST_MARKER", "PROJECT-OK")
	t.Setenv("CC_REMOTE_TOKEN", "token-that-must-not-be-in-child-env")
	dir := t.TempDir()
	name, args := "/bin/sh", []string{"-i"}
	command := "printf 'OUTPUT-%s\\n' \"$CC_PROJECT_SHELL_TEST_MARKER\"; pwd; printf 'CLEARED-%s%s\\n' \"$CC_REMOTE_TOKEN\" yes\r"
	cleared := "CLEARED-yes"
	if runtime.GOOS == "windows" {
		name, args = "cmd.exe", []string{"/D", "/Q"}
		command = "echo OUTPUT-%CC_PROJECT_SHELL_TEST_MARKER% & cd & if not defined CC_REMOTE_TOKEN echo CLEARED-%CC_PROJECT_SHELL_TEST_MARKER%\r"
		cleared = "CLEARED-PROJECT-OK"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	shell, err := startProjectShell(dir, path, args)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shell.Close()
		waitProjectShell(t, shell)
	})

	first, _, detach := shell.Subscribe(0)
	detach()
	if err := shell.Resize(90, 24); err != nil {
		t.Fatal(err)
	}
	if err := shell.Write([]byte(command)); err != nil {
		t.Fatal(err)
	}
	catchup, live, detach := shell.Subscribe(first.Through)
	defer detach()
	if catchup.Reset || catchup.Cols != 120 || catchup.Rows != 40 {
		t.Fatalf("unexpected catch-up header: reset=%v size=%dx%d", catchup.Reset, catchup.Cols, catchup.Rows)
	}
	var output bytes.Buffer
	var resized bool
	last := first.Through
	consume := func(event ProjectShellEvent) {
		t.Helper()
		if event.Sequence != last+1 {
			t.Fatalf("sequence %d after %d", event.Sequence, last)
		}
		last = event.Sequence
		resized = resized || (event.Cols == 90 && event.Rows == 24)
		output.Write(event.Data)
	}
	for _, event := range catchup.Events {
		consume(event)
	}
	if !resized {
		t.Fatal("resize event did not come back after reconnect")
	}
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for !strings.Contains(output.String(), cleared) {
		select {
		case event, ok := <-live:
			if !ok {
				t.Fatalf("subscription ended: %q", output.String())
			}
			consume(event)
		case <-timer.C:
			t.Fatalf("timed out waiting for command output: %q", output.String())
		}
	}
	if got := output.String(); !strings.Contains(got, "OUTPUT-PROJECT-OK") || !strings.Contains(got, dir) || strings.Contains(got, "token-that-must-not-be-in-child-env") {
		t.Fatalf("wrong shell output for cwd %s: %q", dir, got)
	}
	if err := shell.Write([]byte("exit\r")); err != nil {
		t.Fatal(err)
	}
	waitProjectShell(t, shell)
}

func TestProjectShellStartsAndStopsPlatformShell(t *testing.T) {
	shell, err := StartProjectShell(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	shell.Close()
	shell.Close()
	waitProjectShell(t, shell)
	if err := shell.Write([]byte("echo late\r")); err != errProjectShellExited {
		t.Fatalf("late write returned %v", err)
	}
}

func TestProjectShellTruncatedReplayRestoresScreen(t *testing.T) {
	shell := &ProjectShell{
		done: make(chan struct{}), cols: 30, rows: 8, baseCols: 30, baseRows: 8,
		subscribers: make(map[chan ProjectShellEvent]struct{}), screen: newScreen(30, 8), modes: newModeTracker(),
	}
	for i := 0; i <= projectShellJournalEvents; i++ {
		shell.publishLocked(ProjectShellEvent{Data: []byte("\x1b[m")})
	}
	prompt := []byte("project $ 你好\x1b[?2004h")
	shell.screen.write(prompt)
	shell.modes.observe(prompt)
	shell.publishLocked(ProjectShellEvent{Data: prompt})
	replay, _, detach := shell.Subscribe(0)
	defer detach()
	if !replay.Reset || replay.Through != shell.sequence || len(replay.Events) != 0 {
		t.Fatalf("wrong reset replay: reset=%v through=%d events=%d", replay.Reset, replay.Through, len(replay.Events))
	}
	if got := string(replay.Screen); !strings.Contains(got, "project $ 你好") || !strings.Contains(got, "\x1b[1;15H\x1b[?2004h") {
		t.Fatalf("screen lost wide characters, cursor, or paste mode: %q", got)
	}
}

func waitProjectShell(t *testing.T, shell *ProjectShell) {
	t.Helper()
	select {
	case <-shell.Done():
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for shell to finish")
	}
}

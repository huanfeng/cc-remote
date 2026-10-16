package terminal

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"
)

// The test binary doubles as a fake interactive Claude: in raw mode it shows
// the folder trust dialog, then reports what it receives.
func TestMain(m *testing.M) {
	if os.Getenv("CC_REMOTE_TERMINAL_CHILD") == "1" {
		child()
		return
	}
	os.Exit(m.Run())
}

func child() {
	if old, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), old)
	}
	out := func(s string) { os.Stdout.WriteString(s) }
	out("Quick safety check \x1b[5GIs \x1b[9Gthis a project you trust?\r\n❯ No, exit\r\n  Yes, I trust \x1b[20Gthis \x1b[25Gfolder\r\n")
	buf := make([]byte, 4096)
	var pending []byte
	trusted := false
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return
		}
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 {
			switch {
			case !trusted && bytes.HasPrefix(pending, []byte("\x1b[B")):
				out("  No, exit\r\n❯ Yes, I trust this folder\r\n")
				pending = pending[3:]
			case !trusted && pending[0] == '\r':
				trusted = true
				out("TRUSTED\r\n")
				pending = pending[1:]
			case bytes.HasPrefix(pending, []byte("\x1b[200~")):
				end := bytes.Index(pending, []byte("\x1b[201~"))
				if end < 0 {
					goto more
				}
				body := string(pending[len("\x1b[200~"):end])
				out(fmt.Sprintf("PASTE[%s]\r\n", strings.ReplaceAll(body, "\n", "|")))
				pending = pending[end+len("\x1b[201~"):]
			case pending[0] == '\r':
				out("ENTER\r\n")
				pending = pending[1:]
			case pending[0] == 0x1b && len(pending) == 1:
				out("ESC\r\n")
				pending = pending[1:]
			case pending[0] == 0x1b && pending[1] != '[':
				out("ESC\r\n")
				pending = pending[1:]
			case pending[0] == 0x03:
				out("CTRL-C\r\n")
				return
			case pending[0] == 0x1b:
				goto more // an escape sequence still arriving
			default:
				out(fmt.Sprintf("KEY[%c]\r\n", pending[0]))
				pending = pending[1:]
			}
		}
	more:
	}
}

func start(t *testing.T) *Runner {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r, err := Start(Options{Executable: exe, Args: []string{"-test.run=^$"}, Dir: t.TempDir(), Env: append(CleanEnv(os.Environ()), "CC_REMOTE_TERMINAL_CHILD=1"), Cols: 100, Rows: 30, TrustFolder: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(2 * time.Second) })
	return r
}

// collect gathers a front end's output until want appears (ANSI stripped, spaces kept).
func collect(t *testing.T, sub *Sub, seen *strings.Builder, want string) {
	t.Helper()
	timeout := time.After(15 * time.Second)
	for !strings.Contains(seen.String(), want) {
		select {
		case chunk, ok := <-sub.C:
			if !ok {
				t.Fatalf("output closed before %q; got %q", want, seen.String())
			}
			seen.Write(ansi.ReplaceAll(chunk, nil))
		case <-timeout:
			t.Fatalf("timed out waiting for %q; got %q", want, seen.String())
		}
	}
}

// The trust dialog is confirmed only once the selection is on "Yes"; pasted
// messages keep their newlines in one paste and end with Enter; Esc and keys
// pass through; a front end can leave and another attach without affecting
// the process.
func TestRunnerTrustPasteAndFrontEnds(t *testing.T) {
	r := start(t)
	a := r.Subscribe()
	var seenA strings.Builder
	collect(t, a, &seenA, "TRUSTED")
	if err := r.Paste("line one\nline two"); err != nil {
		t.Fatal(err)
	}
	collect(t, a, &seenA, "PASTE[line one|line two]")
	collect(t, a, &seenA, "ENTER")
	if err := r.Interrupt(); err != nil {
		t.Fatal(err)
	}
	collect(t, a, &seenA, "ESC")
	r.Unsubscribe(a)
	if _, ok := <-a.C; ok {
		// drain: the channel is closed after Unsubscribe
		for range a.C {
		}
	}
	b := r.Subscribe()
	var seenB strings.Builder
	if err := r.Keys([]byte("x")); err != nil {
		t.Fatal(err)
	}
	collect(t, b, &seenB, "KEY[x]")
	if r.Attached() != 1 {
		t.Fatalf("attached %d", r.Attached())
	}
	select {
	case <-r.Done():
		t.Fatal("process ended when a front end left")
	default:
	}
}

// A web message waits for a pause in a front end's typing.
func TestPasteWaitsForTypingPause(t *testing.T) {
	r := start(t)
	sub := r.Subscribe()
	var seen strings.Builder
	collect(t, sub, &seen, "TRUSTED")
	_ = r.Keys([]byte("k"))
	began := time.Now()
	if err := r.Paste("hello"); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(began); waited < typingPause-200*time.Millisecond {
		t.Fatalf("paste did not wait for the typing pause: %v", waited)
	}
	collect(t, sub, &seen, "PASTE[hello]")
}

func TestCloseEndsProcessAndFrontEnds(t *testing.T) {
	r := start(t)
	sub := r.Subscribe()
	var seen strings.Builder
	collect(t, sub, &seen, "TRUSTED")
	r.Close(3 * time.Second)
	select {
	case <-r.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("process still running after Close")
	}
	for range sub.C { // closed when the process exits
	}
	if err := r.Paste("late"); err == nil {
		t.Fatal("paste after exit succeeded")
	}
}

func TestCleanEnvDropsSessionMarkers(t *testing.T) {
	got := CleanEnv([]string{"PATH=/bin", "CLAUDECODE=1", "claude_code_child_session=1", "CLAUDE_CODE_USE_POWERSHELL_TOOL=1", "HOME=/h"})
	if strings.Join(got, ",") != "PATH=/bin,CLAUDE_CODE_USE_POWERSHELL_TOOL=1,HOME=/h" {
		t.Fatal(got)
	}
}

// A message sent while Claude Code is still starting waits until it accepts
// input (and the trust dialog is answered) instead of being lost.
func TestPasteRightAfterStartWaitsForReady(t *testing.T) {
	r := start(t)
	sub := r.Subscribe()
	if err := r.Paste("early"); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	collect(t, sub, &seen, "PASTE[early]")
	if !strings.Contains(seen.String(), "TRUSTED") || strings.Index(seen.String(), "TRUSTED") > strings.Index(seen.String(), "PASTE[early]") {
		t.Fatalf("pasted before the trust dialog was answered: %q", seen.String())
	}
}

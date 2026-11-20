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
	shell := newProjectShellJournalTest()
	shell.publishLocked(ProjectShellEvent{Data: []byte("project $ 你好\x1b[?20")})
	shell.publishLocked(ProjectShellEvent{Data: []byte("04h")})
	// Put half of a UTF-8 character before the retention boundary.
	split := []byte("界")
	shell.publishLocked(ProjectShellEvent{Data: split[:1]})
	shell.publishLocked(ProjectShellEvent{Data: split[1:]})
	for range projectShellJournalEvents - 1 {
		shell.publishLocked(ProjectShellEvent{Data: []byte("\x1b[m")})
	}
	replay, _, detach := shell.Subscribe(0)
	defer detach()
	if !replay.Reset || replay.ScreenSequence != 3 || replay.Through != shell.sequence || len(replay.Events) != projectShellJournalEvents {
		t.Fatalf("wrong reset replay: reset=%v through=%d events=%d", replay.Reset, replay.Through, len(replay.Events))
	}
	if got := string(replay.Screen); !strings.Contains(got, "project $ 你好") || !strings.Contains(got, "\x1b[1;15H\x1b[?2004h") {
		t.Fatalf("screen lost wide characters, cursor, or paste mode: %q", got)
	}
	if !bytes.HasSuffix(replay.Screen, split[:1]) || !bytes.Equal(replay.Events[0].Data, split[1:]) || replay.Events[0].Sequence != 4 {
		t.Fatalf("snapshot and retained output lost a split character")
	}
}

func TestProjectShellRetainsBurstOfSmallOutputFrames(t *testing.T) {
	shell := newProjectShellJournalTest()
	for range 12_000 {
		shell.publishLocked(ProjectShellEvent{Data: []byte("log line\r\n")})
	}
	replay, _, detach := shell.Subscribe(0)
	defer detach()
	if replay.Reset || len(replay.Events) != 12_000 {
		t.Fatalf("a small log burst should be fully retained: reset=%v events=%d", replay.Reset, len(replay.Events))
	}
}

func TestProjectShellReadsOutputInBoundedSegments(t *testing.T) {
	shell := newProjectShellJournalTest()
	updates, detach := shell.WatchOutput()
	defer detach()
	for range 3_000 {
		shell.publishLocked(ProjectShellEvent{Data: []byte("log\r\n")})
	}
	shell.cols, shell.rows = 45, 15
	shell.publishLocked(ProjectShellEvent{Cols: 45, Rows: 15})
	shell.publishLocked(ProjectShellEvent{Data: []byte("exit prompt")})
	select {
	case _, open := <-updates:
		if !open {
			t.Fatal("slow reader was disconnected")
		}
	default:
		t.Fatal("burst produced no wake-up")
	}
	first := shell.ReadOutput(0, 58) // Two five-byte events and their framing.
	if first.Reset || len(first.Events) != 2 || first.Through != 2 || first.Latest != 3002 || !first.More {
		t.Fatalf("wrong first output segment: %+v", first)
	}
	last := shell.ReadOutput(3001, 100)
	if last.Reset || len(last.Events) != 1 || last.Through != 3002 || last.More || last.Cols != 45 || last.Rows != 15 {
		t.Fatalf("wrong last output segment: %+v", last)
	}
}

func TestProjectShellHistoryByteBudget(t *testing.T) {
	shell := newProjectShellJournalTest()
	const line = "dense log output\r\n"
	chunk := bytes.Repeat([]byte(line), (32<<10)/len(line))
	retained := projectShellJournalBytes / len(chunk)
	for range retained + 4 {
		shell.publishLocked(ProjectShellEvent{Data: chunk})
	}
	segment := shell.ReadOutput(0, 2*(len(chunk)+24))
	if shell.bytes > projectShellJournalBytes || !segment.Reset || segment.ScreenSequence != 4 ||
		segment.Through != 6 || segment.Latest != uint64(retained+4) || len(segment.Events) != 2 || !segment.More {
		t.Fatalf("wrong bounded history: bytes=%d reset=%v checkpoint=%d through=%d latest=%d events=%d more=%v",
			shell.bytes, segment.Reset, segment.ScreenSequence, segment.Through, segment.Latest, len(segment.Events), segment.More)
	}
}

func TestProjectShellScreenScrollReusesRows(t *testing.T) {
	screen := newScreen(20, 3)
	screen.write([]byte("\x1b[2moverwritten content\x1b[0m\r\n"))
	log := []byte("long output\r\nshort\r\nlast")
	if allocs := testing.AllocsPerRun(30, func() { screen.write(log) }); allocs != 0 {
		t.Errorf("screen scrolling allocated %.1f times per write", allocs)
	}
	screen.write([]byte("\r\nfirst\r\nsecond\r\nthird"))
	if got := strings.Join(screen.lines(), "|"); got != "first|second|third" {
		t.Fatalf("scroll retained overwritten cells: %q", got)
	}
}

func BenchmarkProjectShellScreenLongOutput(b *testing.B) {
	screen := newScreen(120, 40)
	log := bytes.Repeat([]byte("11-20 08:45:12: example continuous log output with more text here\r\n"), 500)
	b.ReportAllocs()
	b.SetBytes(int64(len(log)))
	for b.Loop() {
		screen.write(log)
	}
}

func newProjectShellJournalTest() *ProjectShell {
	return &ProjectShell{
		done: make(chan struct{}), cols: 30, rows: 8,
		subscribers: make(map[chan ProjectShellEvent]struct{}), screen: newScreen(30, 8), modes: newModeTracker(),
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

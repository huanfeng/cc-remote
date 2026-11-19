package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestRemoteProjectCommandIO(t *testing.T) {
	s, host, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	t.Setenv("CC_REMOTE_TOKEN", "child-must-not-inherit-this")
	path := "/api/project-shells/run"
	if status, _ := request(t, host, http.MethodGet, path, nil, ""); status != http.StatusUnauthorized {
		t.Fatalf("missing authentication: %d", status)
	}
	addr := "ws" + strings.TrimPrefix(host.URL, "http") + path
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	ws := dialProjectCommand(t, ctx, addr)
	workingDir := t.TempDir()
	canonical, _, err := projectShellDir(workingDir)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.Repeat([]byte("stdin\x00参数\n"), 10_000)
	args := []string{"two words", `literal; "quote" & 参数`, "", "--help"}
	request := projectCommandRequest{Cwd: workingDir, Argv: append([]string{exe, "-test.run=^TestProjectCommandHelper$", "--", "io"}, args...)}
	startProjectCommandForTest(t, ctx, ws, request)
	for chunk := range slices.Chunk(input, 32<<10) {
		if err := ws.Write(ctx, websocket.MessageBinary, chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := ws.Write(ctx, websocket.MessageText, []byte(`{"type":"stdin-eof"}`)); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, result := readProjectCommandForTest(t, ctx, ws)
	wantArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte(fmt.Sprintf("%s\n%s\n", canonical, wantArgs))
	expected = append(expected, input...)
	if runtime.GOOS == "windows" {
		// The OS can return a different spelling for the same Windows path.
		line, rest, ok := bytes.Cut(stdout, []byte{'\n'})
		if !ok || !strings.EqualFold(string(line), canonical) {
			t.Fatalf("cwd = %q, want %q", line, canonical)
		}
		stdout = append(append([]byte(canonical), '\n'), rest...)
	}
	if result.Type != "exit" || result.Code == nil || *result.Code != 37 || !bytes.Equal(stdout, expected) || string(stderr) != "stderr:CCTOKEN=" {
		t.Fatalf("result: frame=%+v stdout bytes=%d/%d stderr=%q", result, len(stdout), len(expected), stderr)
	}
	zero := dialProjectCommand(t, ctx, addr)
	startProjectCommandForTest(t, ctx, zero, projectCommandRequest{Cwd: canonical, Argv: []string{exe, "-test.run=^TestProjectCommandHelper$", "--", "zero"}})
	_, _, result = readProjectCommandForTest(t, ctx, zero)
	if result.Type != "exit" || result.Code == nil || *result.Code != 0 {
		t.Fatalf("expected explicit exit code 0: %+v", result)
	}
	invalid := dialProjectCommand(t, ctx, addr)
	startProjectCommandForTest(t, ctx, invalid, projectCommandRequest{Cwd: filepath.Join(workingDir, "missing"), Argv: []string{exe}})
	_, _, result = readProjectCommandForTest(t, ctx, invalid)
	if result.Type != "error" || result.Detail == "" {
		t.Fatalf("expected unavailable directory error: %+v", result)
	}
}

func TestRemoteProjectCommandDisconnectAndShutdown(t *testing.T) {
	s, host, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	addr := "ws" + strings.TrimPrefix(host.URL, "http") + "/api/project-shells/run"
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	start := func() *websocket.Conn {
		t.Helper()
		ws := dialProjectCommand(t, ctx, addr)
		startProjectCommandForTest(t, ctx, ws, projectCommandRequest{Cwd: t.TempDir(), Argv: []string{exe, "-test.run=^TestProjectCommandHelper$", "--", "wait"}})
		kind, b, err := ws.Read(ctx)
		if kind != websocket.MessageBinary || err != nil || !bytes.Equal(b, []byte{projectCommandStdout, '1'}) {
			t.Fatalf("child did not start: type=%v frame=%q err=%v", kind, b, err)
		}
		return ws
	}
	ws := start()
	_ = ws.CloseNow()
	await(t, func() bool {
		s.projectCommands.mu.Lock()
		defer s.projectCommands.mu.Unlock()
		return len(s.projectCommands.active) == 0
	})
	ws = start()
	if err := ws.Write(ctx, websocket.MessageBinary, bytes.Repeat([]byte{'x'}, 128<<10)); err != nil {
		t.Fatal(err)
	}
	shutdown, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	s.projectCommands.shutdown(shutdown)
	s.projectCommands.mu.Lock()
	active := len(s.projectCommands.active)
	s.projectCommands.mu.Unlock()
	if active != 0 {
		t.Fatalf("shutdown left %d active project commands", active)
	}
	if _, _, err := ws.Read(ctx); err == nil {
		t.Fatal("command stream survived shutdown")
	}
}

func dialProjectCommand(t *testing.T, ctx context.Context, addr string) *websocket.Conn {
	t.Helper()
	ws, _, err := websocket.Dial(ctx, addr, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + testToken}}})
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(projectCommandFrameLimit)
	t.Cleanup(func() { _ = ws.CloseNow() })
	return ws
}

func startProjectCommandForTest(t *testing.T, ctx context.Context, ws *websocket.Conn, req projectCommandRequest) {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func readProjectCommandForTest(t *testing.T, ctx context.Context, ws *websocket.Conn) (stdout, stderr []byte, result projectCommandFrame) {
	t.Helper()
	for {
		kind, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.MessageText {
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			return
		}
		if len(data) == 0 {
			t.Fatal("empty output frame")
		}
		switch data[0] {
		case projectCommandStdout:
			stdout = append(stdout, data[1:]...)
		case projectCommandStderr:
			stderr = append(stderr, data[1:]...)
		default:
			t.Fatalf("unknown output stream: %d", data[0])
		}
	}
}

// Run the test binary as a portable child, without invoking a command shell.
func TestProjectCommandHelper(t *testing.T) {
	i := slices.Index(os.Args, "--")
	if i < 0 || len(os.Args) < i+2 {
		return
	}
	switch os.Args[i+1] {
	case "io":
		cwd, _ := os.Getwd()
		args, _ := json.Marshal(os.Args[i+2:])
		fmt.Fprintf(os.Stdout, "%s\n%s\n", cwd, args)
		fmt.Fprint(os.Stderr, "stderr:CCTOKEN="+os.Getenv("CC_REMOTE_TOKEN"))
		_, _ = io.Copy(os.Stdout, os.Stdin)
		os.Exit(37)
	case "zero":
		os.Exit(0)
	case "wait":
		fmt.Fprint(os.Stdout, "1")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

package terminal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/hostauth"

	pty "github.com/aymanbagabas/go-pty"
	"github.com/coder/websocket"
)

func TestProjectShellCLIOptions(t *testing.T) {
	o, err := parseProjectShellCLI([]string{"--data-dir", "fixture", "attach", "abc", "--host=https://test.invalid"}, "default")
	if err != nil || o.command != "attach" || o.query != "abc" || o.dataDir != "fixture" || o.host != "https://test.invalid" {
		t.Fatalf("options = %+v, %v", o, err)
	}
	o, err = parseProjectShellCLI([]string{"exit", "--cwd", "my project"}, "default")
	if err != nil || o.command != "stop" || o.cwd != "my project" || o.dataDir != "default" {
		t.Fatalf("options = %+v, %v", o, err)
	}
	if _, err := parseProjectShellCLI([]string{"list", "unexpected"}, "default"); err == nil {
		t.Fatal("unexpected extra list argument was accepted")
	}
	shells := []cliProjectShell{{ID: "ABC123"}, {ID: "ABD456"}}
	if entry, err := pickProjectShell(shells, "abc"); err != nil || entry.ID != "ABC123" {
		t.Fatalf("select prefix: %+v, %v", entry, err)
	}
	if _, err := pickProjectShell(shells, "ab"); err == nil {
		t.Fatal("ambiguous ID was accepted")
	}
}

func TestProjectShellCLIStop(t *testing.T) {
	const token = "project-shell-cli-test-auth"
	t.Setenv("CC_REMOTE_TOKEN", token)
	cwd := t.TempDir()
	deleted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == hostauth.Path:
			_ = json.NewEncoder(w).Encode(map[string]string{"proof": hostauth.Proof(token, r.URL.Query().Get("nonce"), r.Host)})
		case r.Header.Get("Authorization") != "Bearer "+token:
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == projectShellAPI && r.Method == http.MethodGet:
			if filepath.Clean(r.URL.Query().Get("cwd")) != cwd {
				t.Errorf("lookup cwd = %q, want %q", r.URL.Query().Get("cwd"), cwd)
			}
			_ = json.NewEncoder(w).Encode([]cliProjectShell{
				{ID: "UNRELATED", Cwd: filepath.Join(cwd, "other"), State: "running", RunID: "OTHER"},
				{ID: "SHELL", Cwd: filepath.Join(cwd, "canonical"), Aliases: []string{cwd}, State: "running", RunID: "RUN"},
			})
		case r.URL.Path == projectShellAPI+"/SHELL/runs/RUN" && r.Method == http.MethodDelete:
			deleted <- struct{}{}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := ProjectShellCLI([]string{"stop", "--cwd", cwd, "--host", server.URL}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-deleted:
	default:
		t.Fatal("stop never attempted to end the run")
	}
}

func TestProjectShellCLIAttach(t *testing.T) {
	const token = "project-shell-cli-test-auth"
	const ticketPath = projectShellAPI + "/SHELL/runs/RUN/ticket"
	const streamPath = projectShellAPI + "/SHELL/runs/RUN/stream"
	gotInput := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == hostauth.Path:
			_ = json.NewEncoder(w).Encode(map[string]string{"proof": hostauth.Proof(token, r.URL.Query().Get("nonce"), r.Host)})
		case r.URL.Path == projectShellAPI && r.Method == http.MethodGet && r.Header.Get("Authorization") == "Bearer "+token:
			_ = json.NewEncoder(w).Encode([]cliProjectShell{{ID: "SHELL", State: "running", RunID: "RUN"}})
		case r.URL.Path == ticketPath && r.Method == http.MethodPost && r.Header.Get("Authorization") == "Bearer "+token:
			_ = json.NewEncoder(w).Encode(map[string]string{"ticket": "fixture"})
		case r.URL.Path == streamPath && r.URL.Query().Get("ticket") == "fixture" && r.URL.Query().Get("after") == "0" && r.Header.Get("Authorization") == "":
			ws, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer ws.CloseNow()
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			for _, frame := range []string{`{"type":"ready","seq":0}`, `{"type":"control","held":true}`} {
				if err := ws.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
					t.Error(err)
					return
				}
			}
			// A maximal PTY chunk plus the 8-byte sequence prefix exceeds the
			// WebSocket's default message-size limit.
			data := make([]byte, 8+(32<<10))
			for i := 8; i < len(data); i++ {
				data[i] = ' '
			}
			copy(data[len(data)-len("ready-marker"):], "ready-marker")
			if err := ws.Write(ctx, websocket.MessageBinary, data); err != nil {
				t.Error(err)
				return
			}
			var input strings.Builder
			for {
				kind, data, err := ws.Read(ctx)
				if err != nil {
					return
				}
				if kind != websocket.MessageBinary {
					continue
				}
				input.Write(data)
				if input.Len() > len("hello") {
					t.Errorf("unexpected project shell input: %q", input.String())
					return
				}
				if input.Len() == len("hello") {
					gotInput <- input.String()
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	p, err := pty.New()
	if err != nil {
		t.Skipf("PTY unavailable: %v", err)
	}
	defer p.Close()
	cmd := p.Command(os.Args[0], "-test.run=^TestProjectShellCLIProcess$")
	cmd.Env = append(CleanEnv(os.Environ()), "CC_PROJECT_SHELL_TEST_HOST="+server.URL, "CC_REMOTE_TOKEN="+token)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	if unix, ok := p.(pty.UnixPty); ok {
		_ = unix.Slave().Close()
	}
	output := make(chan string, 16)
	go func() {
		defer close(output)
		buf := make([]byte, 8192)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				output <- string(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	var screen strings.Builder
	timeout := time.After(12 * time.Second)
	waitOutput := func(marker string) {
		t.Helper()
		for !strings.Contains(screen.String(), marker) {
			select {
			case part, ok := <-output:
				if !ok {
					t.Fatalf("terminal closed before marker %q: %q", marker, outputTail(screen.String()))
				}
				screen.WriteString(part)
			case <-timeout:
				t.Fatalf("timed out before marker %q: %q", marker, outputTail(screen.String()))
			}
		}
	}
	waitOutput("ready-marker")
	if _, err := io.WriteString(p, "hello"); err != nil {
		t.Fatal(err)
	}
	select {
	case input := <-gotInput:
		if input != "hello" {
			t.Fatalf("relayed input = %q", input)
		}
	case <-timeout:
		t.Fatal("timed out awaiting input")
	}
	if _, err := io.WriteString(p, "\x1d"); err != nil {
		t.Fatal(err)
	}
	waitOutput("ccr shell attach SHELL")
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func outputTail(s string) string {
	if len(s) > 300 {
		return s[len(s)-300:]
	}
	return s
}

func TestProjectShellCLIProcess(t *testing.T) {
	if host := os.Getenv("CC_PROJECT_SHELL_TEST_HOST"); host != "" {
		if err := ProjectShellCLI([]string{"attach", "SHELL", "--host", host}, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
}

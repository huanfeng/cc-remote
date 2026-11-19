package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/hostauth"

	"github.com/coder/websocket"
)

func TestProjectCommandStreamsAndExitStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wantReq := projectCommandRequest{Cwd: "/srv/project", Argv: []string{"script", "a b", "--host", ";"}}
	stdin := bytes.Repeat([]byte("abc\x00"), 9000)
	const token = "command-test-token"
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/project-shells/run" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("invalid command endpoint or authorization: %s", r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer ws.CloseNow()
		kind, b, err := ws.Read(ctx)
		var gotReq projectCommandRequest
		if err != nil || kind != websocket.MessageText || json.Unmarshal(b, &gotReq) != nil || !reflect.DeepEqual(gotReq, wantReq) {
			t.Errorf("command request: kind=%s body=%q err=%v", kind, b, err)
			return
		}
		var gotInput bytes.Buffer
		for {
			kind, b, err := ws.Read(ctx)
			if err != nil {
				t.Error(err)
				return
			}
			if kind == websocket.MessageBinary {
				gotInput.Write(b)
				continue
			}
			if string(b) != `{"type":"stdin-eof"}` || !bytes.Equal(gotInput.Bytes(), stdin) {
				t.Errorf("unexpected stdin EOF or content: %q (%d bytes)", b, gotInput.Len())
				return
			}
			break
		}
		for _, frame := range []struct {
			kind websocket.MessageType
			data string
		}{{websocket.MessageBinary, "\x01before\x00after"}, {websocket.MessageBinary, "\x02remote stderr"}, {websocket.MessageText, `{"type":"exit","code":23}`}} {
			if err := ws.Write(ctx, frame.kind, []byte(frame.data)); err != nil {
				t.Error(err)
				return
			}
		}
	}))
	defer host.Close()

	api := &client{host: host.URL, token: token}
	var stdout, stderr bytes.Buffer
	err := api.projectCommand(ctx, wantReq, bytes.NewReader(stdin), &stdout, &stderr)
	var exit ProjectCommandExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("exit status: %v", err)
	}
	if stdout.String() != "before\x00after" || stderr.String() != "remote stderr" {
		t.Errorf("stdout=%q stderr=%q", &stdout, &stderr)
	}
}

func TestProjectCommandCLIUsesVerifiedHostAndPreservesArguments(t *testing.T) {
	const token = "command-proof-test-token"
	t.Setenv("CC_REMOTE_TOKEN", token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mux := http.NewServeMux()
	mux.HandleFunc(hostauth.Path, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("credentials sent before host proof")
		}
		addr := r.Context().Value(http.LocalAddrContextKey).(net.Addr).String()
		fmt.Fprintf(w, `{"proof":%q}`, hostauth.Proof(token, r.URL.Query().Get("nonce"), addr))
	})
	mux.HandleFunc("/api/project-shells/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer ws.CloseNow()
		_, b, err := ws.Read(ctx)
		var got projectCommandRequest
		if err != nil || json.Unmarshal(b, &got) != nil || got.Cwd != "/remote/project" || !reflect.DeepEqual(got.Argv, []string{"program", "--host", "argument", ""}) {
			t.Errorf("command arguments: %q, err=%v", b, err)
			return
		}
		_ = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"exit","code":0}`))
	})
	host := httptest.NewServer(mux)
	defer host.Close()
	dataDir := t.TempDir()
	started := false
	ensure := func(gotDataDir, gotHost string) error {
		if gotDataDir != dataDir || gotHost != host.URL {
			t.Errorf("ensure options: %q %q", gotDataDir, gotHost)
		}
		started = true
		return nil
	}
	if err := ProjectCommandCLI([]string{"--host", host.URL, "--cwd=/remote/project", "--", "program", "--host", "argument", ""}, dataDir, ensure); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("host setup was skipped")
	}
	if err := ProjectCommandCLI(nil, dataDir, ensure); err == nil || !strings.Contains(err.Error(), "ccr run") {
		t.Fatalf("missing command: %v", err)
	}
}

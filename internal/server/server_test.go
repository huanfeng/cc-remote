package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

const testToken = "test-access-token-at-least-16"

func setup(t *testing.T, runner Runner) (*Server, *httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	config := t.TempDir()
	s := New(st, Config{Token: testToken, Runner: runner, Cwd: t.TempDir(), ClaudeConfigDir: config, ClaudeConfigEnv: config})
	ts := httptest.NewServer(s.Handler())
	s.cfg.BridgeURL = ts.URL + "/api/bridge/request"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
		ts.Close()
		st.Close()
	})
	return s, ts, st
}
func request(t *testing.T, ts *httptest.Server, method, path string, body any, token string) (int, []byte) {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, b
}
func createSession(t *testing.T, ts *httptest.Server) store.Session {
	t.Helper()
	code, b := request(t, ts, "POST", "/api/sessions", map[string]string{"title": "test", "cwd": t.TempDir()}, testToken)
	if code != 201 {
		t.Fatalf("create: %d %s", code, b)
	}
	var s store.Session
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func await(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}

func TestAuthenticationAndValidation(t *testing.T) {
	_, ts, _ := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	if code, _ := request(t, ts, "GET", "/api/sessions", nil, ""); code != 401 {
		t.Fatalf("unauthenticated code %d", code)
	}
	if code, _ := request(t, ts, "POST", "/api/sessions", map[string]string{"cwd": "relative"}, testToken); code != 400 {
		t.Fatalf("invalid cwd code %d", code)
	}
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions", bytes.NewBufferString(`{"cwd":"invalid"} trailing`))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatal("malformed trailing JSON accepted")
	}
	req, _ = http.NewRequest("GET", ts.URL+"/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Origin", "https://other.example")
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("cross-origin request accepted")
	}
}

// TestOriginBehindReverseProxy covers proxies that rewrite Host to the
// upstream address and pass the public host in X-Forwarded-Host.
func TestOriginBehindReverseProxy(t *testing.T) {
	_, ts, _ := setup(t, nil)
	cases := []struct {
		origin, fwd string
		want        int
	}{
		{"https://cc.example.com", "cc.example.com", 200},
		{"https://cc.example.com", "cc.example.com:443", 200},
		{"https://cc.example.com:8443", "cc.example.com:8443", 200},
		{"https://cc.example.com", "cc.example.com, internal", 200},
		{"https://evil.example", "cc.example.com", 403},
		{"https://cc.example.com", "", 403},
		{"https://cc.example.com:8443", "cc.example.com", 403},
		{"null", "cc.example.com", 403},
	}
	for _, c := range cases {
		req, _ := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Origin", c.origin)
		if c.fwd != "" {
			req.Header.Set("X-Forwarded-Host", c.fwd)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("Origin %q X-Forwarded-Host %q: status %d, want %d", c.origin, c.fwd, resp.StatusCode, c.want)
		}
	}
}

func TestConcurrentSessionsAndStop(t *testing.T) {
	started := make(chan claude.Options, 3)
	_, ts, st := setup(t, func(ctx context.Context, o claude.Options, emit func(claude.Event) error) error {
		if err := emit(claude.Event{Kind: "session"}); err != nil {
			return err
		}
		started <- o
		<-ctx.Done()
		return ctx.Err()
	})
	a, b := createSession(t, ts), createSession(t, ts)
	for _, x := range []store.Session{a, b} {
		if code, body := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "hello"}, testToken); code != 202 {
			t.Fatalf("send %d %s", code, body)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case o := <-started:
			if o.Resume {
				t.Fatal("new session resumed")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("runner not started")
		}
	}
	// A message sent while a turn runs is queued and runs next.
	if code, body := request(t, ts, "POST", "/api/sessions/"+a.ID+"/messages", map[string]string{"text": "again"}, testToken); code != 202 || !strings.Contains(string(body), "queued") {
		t.Fatalf("queued send %d %s", code, body)
	}
	if code, _ := request(t, ts, "POST", "/api/sessions/"+a.ID+"/stop", nil, testToken); code != 200 {
		t.Fatal("stop failed")
	}
	select {
	case o := <-started:
		if o.Message != "again" || !o.Resume {
			t.Fatalf("queued turn %q resume=%v", o.Message, o.Resume)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued message not run after stop")
	}
	if x, _ := st.Get(a.ID); x.Status != "running" {
		t.Fatalf("status while queued turn runs: %s", x.Status)
	}
	if code, _ := request(t, ts, "POST", "/api/sessions/"+a.ID+"/stop", nil, testToken); code != 200 {
		t.Fatal("second stop failed")
	}
	await(t, func() bool { x, _ := st.Get(a.ID); return x.Status == "interrupted" })
	x, _ := st.Get(b.ID)
	if x.Status != "running" {
		t.Fatalf("other session changed: %s", x.Status)
	}
	if code, _ := request(t, ts, "POST", "/api/sessions/"+a.ID+"/messages", map[string]string{"text": "continue"}, testToken); code != 202 {
		t.Fatal("resume send failed")
	}
	select {
	case o := <-started:
		if !o.Resume {
			t.Fatal("initialized interrupted session did not resume")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("resume not started")
	}
}

func TestApprovalBridgeRoundtrip(t *testing.T) {
	got := make(chan answer, 1)
	_, ts, st := setup(t, func(ctx context.Context, o claude.Options, emit func(claude.Event) error) error {
		data := []byte(`{"kind":"approval","toolName":"Bash","input":{"command":"git status"}}`)
		req, err := http.NewRequestWithContext(ctx, "POST", o.BridgeURL, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+o.BridgeToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("bridge: %d", resp.StatusCode)
		}
		var a answer
		if err = json.NewDecoder(resp.Body).Decode(&a); err != nil {
			return err
		}
		got <- a
		return emit(claude.Event{Kind: "result", Text: "done"})
	})
	x := createSession(t, ts)
	code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "check"}, testToken)
	if code != 202 {
		t.Fatalf("send %d %s", code, b)
	}
	var pending []store.Interaction
	await(t, func() bool { pending, _ = st.Pending(x.ID); return len(pending) == 1 })
	current, _ := st.Get(x.ID)
	if current.Status != "waiting" {
		t.Fatal("not waiting")
	}
	path := "/api/requests/" + pending[0].ID
	if code, b = request(t, ts, "POST", path, map[string]string{"decision": "allow"}, testToken); code != 200 {
		t.Fatalf("resolve %d %s", code, b)
	}
	if code, _ = request(t, ts, "POST", path, map[string]string{"decision": "deny"}, testToken); code != 409 {
		t.Fatal("double approval accepted")
	}
	select {
	case a := <-got:
		if a.Decision != "allow" {
			t.Fatal(a)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("answer not delivered")
	}
	await(t, func() bool { v, _ := st.Get(x.ID); return v.Status == "completed" })
	if code, _ = request(t, ts, "POST", "/api/bridge/request", map[string]any{"kind": "approval", "toolName": "Bash", "input": map[string]string{}}, testToken); code != 401 {
		t.Fatal("external token accepted for process bridge")
	}
}

func TestNativeHistoryImportRequiresStoppedConfirmation(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	cwd := t.TempDir()
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "test-project")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	user, _ := json.Marshal(map[string]any{"type": "user", "cwd": cwd, "message": map[string]any{"content": "Native conversation"}})
	assistant := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Native reply"}]}}`)
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), append(append(user, '\n'), append(assistant, '\n')...), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _ := request(t, ts, "GET", "/api/native/sessions/"+id, nil, testToken); code != 200 {
		t.Fatal("native history unavailable")
	}
	path := "/api/native/sessions/" + id + "/import"
	if code, _ := request(t, ts, "POST", path, map[string]bool{"confirmStopped": false}, testToken); code != 400 {
		t.Fatal("import without confirmation accepted")
	}
	code, b := request(t, ts, "POST", path, map[string]bool{"confirmStopped": true}, testToken)
	if code != 201 {
		t.Fatalf("import %d %s", code, b)
	}
	x, err := st.FindClaude(id)
	if err != nil {
		t.Fatal(err)
	}
	if !x.Resume || x.Cwd != cwd {
		t.Fatalf("incorrect imported state %+v", x)
	}
	events, err := st.Events(x.ID, 0)
	if err != nil || len(events) != 2 || events[1].Text != "Native reply" {
		t.Fatalf("imported events %+v %v", events, err)
	}
	if code, _ = request(t, ts, "POST", path, map[string]bool{"confirmStopped": true}, testToken); code != 200 {
		t.Fatal("reimport should find existing managed session")
	}
	list, err := st.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("duplicate imports %+v %v", list, err)
	}
}

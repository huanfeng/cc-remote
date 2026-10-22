package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"cc-remote/internal/hostauth"
)

// A terminal session in a folder Claude Code does not trust needs trustFolder;
// in one it trusts (or below one), it starts without.
func TestTerminalTrust(t *testing.T) {
	s, base, _, _ := setupTerminal(t)
	state := t.TempDir()
	s.cfg.ClaudeConfigEnv = state
	parent := t.TempDir()
	cwd := filepath.Join(parent, "repo")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"cwd": cwd, "mode": "terminal"}
	if code, b := call(t, base, "POST", "/api/sessions", body); code != http.StatusPreconditionRequired || !strings.Contains(string(b), `"trust-required"`) {
		t.Fatalf("untrusted folder: %d %s", code, b)
	}
	other := `{"projects":{` + strconv.Quote(filepath.ToSlash(t.TempDir())) + `:{"hasTrustDialogAccepted":true},` + strconv.Quote(filepath.ToSlash(cwd)) + `:{"hasTrustDialogAccepted":false}}}`
	if err := os.WriteFile(filepath.Join(state, ".claude.json"), []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, b := call(t, base, "POST", "/api/sessions", body); code != http.StatusPreconditionRequired {
		t.Fatalf("folder trusted elsewhere only: %d %s", code, b)
	}
	trusted := `{"projects":{` + strconv.Quote(filepath.ToSlash(parent)) + `:{"hasTrustDialogAccepted":true}}}`
	if err := os.WriteFile(filepath.Join(state, ".claude.json"), []byte(trusted), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, b := call(t, base, "POST", "/api/sessions", body); code != 201 {
		t.Fatalf("folder below a trusted one: %d %s", code, b)
	}
}

// /api/hello proves the host holds the token without the token being sent.
func TestHelloProof(t *testing.T) {
	_, ts, _ := setup(t, noRun)
	if err := hostauth.Check(ts.Client(), ts.URL, testToken, ""); err != nil {
		t.Fatalf("own host: %v", err)
	}
	if err := hostauth.Check(ts.Client(), ts.URL, "another-token-0123456789", ""); !errors.Is(err, hostauth.ErrWrongToken) {
		t.Fatalf("another token: %v", err)
	}
	resp, err := http.Get(ts.URL + hostauth.Path + "?nonce=xyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("bad nonce: %d", resp.StatusCode)
	}
}

// A terminal session's hook files hold a token of its own, accepted only on
// its own hook endpoints.
func TestHookTokenOnlyForItsSession(t *testing.T) {
	s, base, _, _ := setupTerminal(t)
	x := createTerminal(t, base)
	if code, b := call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "hello"}); code != 202 {
		t.Fatalf("send: %d %s", code, b)
	}
	var claudeID string
	awaitLong(t, func() string { return "terminal not started" }, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if ts := s.terminals[x.ID]; ts != nil {
			claudeID = ts.claudeID
		}
		return claudeID != ""
	})
	var ask struct{ Token string }
	data, err := os.ReadFile(filepath.Join(s.cfg.DataDir, "terminal-config", x.ID, "ask.json"))
	if err != nil || json.Unmarshal(data, &ask) != nil || ask.Token == "" || ask.Token == testToken {
		t.Fatalf("ask.json token: %v (the access token: %v)", err, ask.Token == testToken)
	}
	post := func(method, path string) int {
		req, _ := http.NewRequest(method, base+path, bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Authorization", "Bearer "+ask.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, path := range []string{"/api/terminals/" + x.ID + "/status", "/api/terminals/" + x.ID + "/hook", "/api/channels/" + claudeID + "/interactions"} {
		if code := post("POST", path); code == 401 {
			t.Fatalf("own endpoint %s refused the hook token", path)
		}
	}
	for _, c := range []struct{ method, path string }{{"GET", "/api/sessions"}, {"POST", "/api/requests/x"}, {"POST", "/api/terminals/" + x.ID + "/ticket"}, {"POST", "/api/terminals/other/hook"}, {"POST", "/api/channels/00000000-0000-0000-0000-000000000000/interactions"}} {
		if code := post(c.method, c.path); code != 401 {
			t.Fatalf("%s %s with the hook token: %d", c.method, c.path, code)
		}
	}
}

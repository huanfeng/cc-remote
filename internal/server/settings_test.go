package server

import (
	"cc-remote/internal/claude"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Managed sessions hand custom commands to Claude verbatim so frontmatter,
// hooks and plugin skills behave natively; the menu still lists them.
func TestCustomCommandRunsNativelyAndKeepsOriginalTranscript(t *testing.T) {
	runs := make(chan claude.Options, 1)
	s, ts, st := setup(t, func(_ context.Context, o claude.Options, _ func(claude.Event) error) error { runs <- o; return nil })
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "commands")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "review-test.md"), []byte("Review $ARGUMENTS carefully"), 0600); err != nil {
		t.Fatal(err)
	}
	x := createSession(t, ts)
	path := "/api/sessions/" + x.ID
	code, b := request(t, ts, "GET", path+"/commands", nil, testToken)
	if code != 200 || !strings.Contains(string(b), "review-test") {
		t.Fatalf("discovery %d %s", code, b)
	}
	code, b = request(t, ts, "POST", path+"/commands", map[string]string{"command": "/review-test changes"}, testToken)
	if code != 200 || !strings.Contains(string(b), `"sendMessage":true`) {
		t.Fatalf("command %d %s", code, b)
	}
	code, b = request(t, ts, "POST", path+"/messages", map[string]string{"text": "/review-test changes"}, testToken)
	if code != 202 {
		t.Fatalf("send %d %s", code, b)
	}
	o := <-runs
	if o.Message != "/review-test changes" {
		t.Fatalf("not passed verbatim %s", o.Message)
	}
	events, err := st.Events(x.ID, 0)
	if err != nil || len(events) == 0 || events[0].Text != "/review-test changes" {
		t.Fatalf("transcript %v %v", events, err)
	}
}

func TestSettingsPreserveSecretsAndDetectConcurrentEdits(t *testing.T) {
	s, ts, _ := setup(t, nil)
	path := filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")
	before := []byte(`{"model":"sonnet","env":{"PRIVATE_KEY":"secret"},"hooks":{"SessionStart":[]},"permissions":{"deny":["Bash(rm *)"],"defaultMode":"default"}}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	code, b := request(t, ts, "GET", "/api/claude/settings", nil, testToken)
	if code != 200 {
		t.Fatalf("read %d %s", code, b)
	}
	var x claudeSettings
	if err := json.Unmarshal(b, &x); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	_ = json.Unmarshal(b, &response)
	if _, ok := response["env"]; ok {
		t.Fatal("exposed secret settings")
	}
	x.Model = "opus"
	x.Effort = "high"
	code, b = request(t, ts, "PUT", "/api/claude/settings", x, testToken)
	if code != 200 {
		t.Fatalf("write %d %s", code, b)
	}
	after, _ := os.ReadFile(path)
	var v map[string]any
	_ = json.Unmarshal(after, &v)
	if v["env"].(map[string]any)["PRIVATE_KEY"] != "secret" || v["hooks"] == nil || v["permissions"].(map[string]any)["deny"] == nil {
		t.Fatalf("lost unrelated values %s", after)
	}
	code, _ = request(t, ts, "PUT", "/api/claude/settings", x, testToken)
	if code != 409 {
		t.Fatalf("stale update accepted %d", code)
	}
	_ = json.Unmarshal(b, &x)
	x.Effort = "invalid"
	code, _ = request(t, ts, "PUT", "/api/claude/settings", x, testToken)
	if code != 400 {
		t.Fatalf("invalid effort %d", code)
	}
}

func TestCommandClearChangesContextWithoutDeletingHistory(t *testing.T) {
	_, ts, st := setup(t, nil)
	x := createSession(t, ts)
	_, _ = st.Append(x.ID, "user", "keep this history", nil)
	_ = st.SetResume(x.ID)
	path := "/api/sessions/" + x.ID + "/commands"
	code, b := request(t, ts, "POST", path, map[string]string{"command": "/clear"}, testToken)
	if code != 200 {
		t.Fatalf("clear %d %s", code, b)
	}
	after, _ := st.Get(x.ID)
	events, _ := st.Events(x.ID, 0)
	if after.ClaudeSessionID == x.ClaudeSessionID || after.Resume || len(events) != 2 || events[0].Text != "keep this history" {
		t.Fatalf("bad reset %+v %+v", after, events)
	}
	code, b = request(t, ts, "POST", path, map[string]string{"command": "/new"}, testToken)
	if again, _ := st.Get(x.ID); code != 200 || again.ClaudeSessionID == after.ClaudeSessionID {
		t.Fatalf("/new alias %d %s", code, b)
	}
	// Once Claude has reported its commands, only those are passed through.
	_, _ = st.Append(x.ID, "session", "init", json.RawMessage(`{"type":"system","subtype":"init","slash_commands":["context","usage","clear","model"]}`))
	code, _ = request(t, ts, "POST", path, map[string]string{"command": "/unknown"}, testToken)
	if code != 400 {
		t.Fatalf("unsupported command accepted %d", code)
	}
	code, b = request(t, ts, "POST", path, map[string]string{"command": "/context"}, testToken)
	if code != 200 || !strings.Contains(string(b), `"sendMessage":true`) {
		t.Fatalf("native /context %d %s", code, b)
	}
	code, b = request(t, ts, "GET", path, nil, testToken)
	if code != 200 || !strings.Contains(string(b), `"name":"usage"`) || strings.Count(string(b), `"name":"model"`) != 1 {
		t.Fatalf("menu %d %s", code, b)
	}
}

// TestCompactRunsAsManagedTurn: /compact is passed to Claude verbatim on a
// resumed managed session; fresh sessions and attachments are refused.
func TestCompactRunsAsManagedTurn(t *testing.T) {
	runs := make(chan claude.Options, 1)
	_, ts, st := setup(t, func(_ context.Context, o claude.Options, _ func(claude.Event) error) error { runs <- o; return nil })
	x := createSession(t, ts)
	path := "/api/sessions/" + x.ID
	if code, b := request(t, ts, "POST", path+"/commands", map[string]string{"command": "/compact"}, testToken); code != 400 || !strings.Contains(string(b), "no context to compact") {
		t.Fatalf("fresh session %d %s", code, b)
	}
	if code, _ := request(t, ts, "POST", path+"/messages", map[string]string{"text": "/compact"}, testToken); code != 400 {
		t.Fatalf("fresh session message %d", code)
	}
	_ = st.SetResume(x.ID)
	if code, b := request(t, ts, "POST", path+"/commands", map[string]string{"command": "/compact keep the API notes"}, testToken); code != 200 || !strings.Contains(string(b), `"sendMessage":true`) {
		t.Fatalf("command %d %s", code, b)
	}
	if code, _ := request(t, ts, "POST", path+"/messages", map[string]any{"text": "/compact", "attachmentIds": []string{"a"}}, testToken); code != 400 {
		t.Fatalf("attachments accepted: %d", code)
	}
	if code, b := request(t, ts, "POST", path+"/messages", map[string]string{"text": "/compact keep the API notes"}, testToken); code != 202 {
		t.Fatalf("send %d %s", code, b)
	}
	if o := <-runs; o.Message != "/compact keep the API notes" || !o.Resume {
		t.Fatalf("runner got %q resume=%v", o.Message, o.Resume)
	}
}

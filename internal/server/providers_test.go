package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cc-remote/internal/claude"
	"cc-remote/internal/prefs"
	"cc-remote/internal/store"
)

func createCodexSession(t *testing.T, ts *httptest.Server) store.Session {
	t.Helper()
	code, body := request(t, ts, "POST", "/api/sessions", map[string]any{"provider": "codex", "cwd": t.TempDir()}, testToken)
	if code != 201 {
		t.Fatalf("create Codex: %d %s", code, body)
	}
	var x store.Session
	if err := json.Unmarshal(body, &x); err != nil {
		t.Fatal(err)
	}
	return x
}

func TestCodexSessionValidation(t *testing.T) {
	s, ts, st := setup(t, nil)
	x := createCodexSession(t, ts)
	stored, err := st.Get(x.ID)
	if err != nil || stored.Provider != "codex" {
		t.Fatalf("provider not persisted: %+v %v", stored, err)
	}
	for _, body := range []map[string]any{
		{"provider": "unknown", "cwd": t.TempDir()},
		{"provider": "codex", "cwd": t.TempDir(), "mode": "terminal"},
		{"provider": "codex", "cwd": t.TempDir(), "permissionMode": "bypassPermissions"},
		{"provider": "codex", "cwd": t.TempDir(), "permissionMode": "danger-full-access"},
		{"provider": "codex", "cwd": t.TempDir(), "effort": "max"},
	} {
		if code, b := request(t, ts, "POST", "/api/sessions", body, testToken); code != 400 {
			t.Fatalf("accepted invalid %+v: %d %s", body, code, b)
		}
	}
	for _, body := range []map[string]any{{"permissionMode": "workspace-write", "effort": "xhigh"}, {"permissionMode": "danger-full-access", "confirmDangerous": true}} {
		if code, b := request(t, ts, "PATCH", "/api/sessions/"+x.ID, body, testToken); code != 200 {
			t.Fatalf("settings: %d %s", code, b)
		}
	}
	if p := s.effectivePreferences(x); p.Provider != "codex" {
		t.Fatalf("wrong provider: %+v", p)
	}
	if legacy := createSession(t, ts); legacy.Provider != "claude" {
		t.Fatalf("legacy default: %+v", legacy)
	}
	if code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "/compact keep details"}, testToken); code != 400 {
		t.Fatalf("invalid compact accepted: %d %s", code, b)
	}
}

func TestCodexPreferencesIsolated(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	for _, provider := range []string{"claude", "codex"} {
		path := "/api/preferences?provider=" + provider
		code, b := request(t, ts, "GET", path, nil, testToken)
		if code != 200 {
			t.Fatalf("load: %d %s", code, b)
		}
		var p preferencesBody
		if err := json.Unmarshal(b, &p); err != nil {
			t.Fatal(err)
		}
		if provider == "claude" {
			p.Model, p.PermissionMode = "sonnet", "acceptEdits"
		} else {
			p.Model, p.PermissionMode, p.Effort = "codex-test-model", "workspace-write", "minimal"
		}
		if code, b = request(t, ts, "PUT", path, p, testToken); code != 200 {
			t.Fatalf("save: %d %s", code, b)
		}
		if code, b = request(t, ts, "PUT", path, p, testToken); code != 409 {
			t.Fatalf("stale write: %d %s", code, b)
		}
	}
	x := createCodexSession(t, ts)
	p := s.effectivePreferences(x)
	if p.Model != "codex-test-model" || p.PermissionMode != "workspace-write" || p.Effort != "minimal" {
		t.Fatalf("Codex inherited Claude: %+v", p)
	}
	cp, _, err := prefs.Load(s.cfg.DataDir)
	if err != nil || cp.Model != "sonnet" || cp.PermissionMode != "acceptEdits" {
		t.Fatalf("Claude defaults changed: %+v %v", cp, err)
	}
	if code, _ := request(t, ts, "GET", "/api/preferences?provider=bad", nil, testToken); code != 400 {
		t.Fatal("invalid provider accepted")
	}
}

func TestCodexRuntimeDispatchResumeAndClear(t *testing.T) {
	s, ts, st := setup(t, nil)
	x := createCodexSession(t, ts)
	const thread = "01900000-0000-7000-8000-000000000001"
	starts := make(chan claude.Options, 4)
	s.startProcess = func(claude.Options, claude.ProcessHooks) (claudeProcess, error) {
		return nil, errors.New("must not launch Claude")
	}
	s.cfg.StartCodexProcess = func(o claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		starts <- o
		return runnerStarter(func(_ctx context.Context, opts claude.Options, emit func(claude.Event) error) error {
			if err := emit(claude.Event{Kind: "session", Payload: json.RawMessage(`{"session_id":"` + thread + `","provider":"codex"}`)}); err != nil {
				return err
			}
			return emit(claude.Event{Kind: "assistant", Text: "Codex reply"})
		})(o, h)
	}
	send := func(text string) {
		t.Helper()
		if code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": text}, testToken); code != 202 {
			t.Fatalf("send: %d %s", code, b)
		}
		await(t, func() bool { got, _ := st.Get(x.ID); return got.Status == "completed" })
	}
	send("one")
	if first := <-starts; first.Provider != "codex" || first.Resume {
		t.Fatalf("first launch: %+v", first)
	}
	got, _ := st.Get(x.ID)
	if got.ClaudeSessionID != thread || !got.Resume {
		t.Fatalf("thread not saved: %+v", got)
	}
	s.mu.Lock()
	rt := s.detachLocked(x.ID)
	s.mu.Unlock()
	closeRuntime(rt)
	send("two")
	if second := <-starts; !second.Resume || second.SessionID != thread {
		t.Fatalf("resume: %+v", second)
	}
	if code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/commands", map[string]string{"command": "clear"}, testToken); code != 200 {
		t.Fatalf("clear %d %s", code, b)
	}
	got, _ = st.Get(x.ID)
	if got.Provider != "codex" || got.Resume || got.ClaudeSessionID == thread {
		t.Fatalf("clear: %+v", got)
	}
	ev, _ := st.Events(x.ID, 0)
	if len(ev) < 4 {
		t.Fatal("clear removed history")
	}
}

func TestCodexHistoryImport(t *testing.T) {
	s, ts, st := setup(t, nil)
	s.cfg.CodexConfigDir = t.TempDir()
	db := switchFixture(t, s)
	cfg, _ := json.Marshal(map[string]any{"auth": map[string]string{"OPENAI_API_KEY": "fake-key"}, "config": "model_provider = \"relay\"\n[model_providers.relay]\nname = \"Relay\"\nbase_url = \"https://relay.example/v1\""})
	switchProvider(t, db, "cx", "codex", string(cfg), `{}`, true)
	const id = "01900000-0000-7000-8000-000000000002"
	cwd := t.TempDir()
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": id, "cwd": cwd, "timestamp": "2026-10-24T00:00:00Z"}})
	dir := filepath.Join(s.cfg.CodexConfigDir, "sessions", "2026", "10", "24")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data := string(meta) + "\n" + `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"A Codex project"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-10-24T00-00-00-"+id+".jsonl"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	path := "/api/native/sessions/" + id + "/import?provider=codex"
	code, b := request(t, ts, "POST", path, map[string]any{"confirmStopped": true, "profileId": "cx"}, testToken)
	if code != 201 {
		t.Fatalf("import %d %s", code, b)
	}
	var x store.Session
	if err := json.Unmarshal(b, &x); err != nil {
		t.Fatal(err)
	}
	if x.ProfileID != "ccswitch:cx" || x.Provider != "codex" || !strings.Contains(x.Title, "Codex") {
		t.Fatalf("imported %+v", x)
	}
	got, err := st.Get(x.ID)
	if err != nil || !got.Resume || got.ClaudeSessionID != id {
		t.Fatalf("resume %+v %v", got, err)
	}
	if code, _ = request(t, ts, "POST", path, map[string]bool{"confirmStopped": true}, testToken); code != 200 {
		t.Fatal("reimport not idempotent")
	}
	fresh, _ := st.Get(x.ID)
	if fresh.ProfileID != "ccswitch:cx" {
		t.Fatal("reimport dropped saved supplier")
	}
}

func TestDetachedCodexStartupCannotReplaceThread(t *testing.T) {
	s, _, st := setup(t, nil)
	x, err := st.CreateProvider("Codex", t.TempDir(), store.ID(), false, "codex")
	if err != nil {
		t.Fatal(err)
	}
	old := &sessionRuntime{id: x.ID}
	current := &sessionRuntime{id: x.ID}
	s.mu.Lock()
	s.procs[x.ID] = current
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.procs, x.ID); s.mu.Unlock() }()
	if err := s.processEvent(old, claude.Event{Kind: "session", Payload: json.RawMessage(`{"session_id":"stale-thread","provider":"codex"}`)}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(x.ID)
	if err != nil || got.ClaudeSessionID != x.ClaudeSessionID || got.Resume {
		t.Fatalf("stale startup changed session: %+v %v", got, err)
	}
	current.stopping = true
	if err := s.processEvent(current, claude.Event{Kind: "session", Payload: json.RawMessage(`{"session_id":"stopped-thread","provider":"codex"}`)}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get(x.ID)
	if got.ClaudeSessionID != x.ClaudeSessionID {
		t.Fatalf("stopped startup changed thread: %+v", got)
	}
}

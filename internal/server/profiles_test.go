package server

import (
	"cc-remote/internal/claude"
	"cc-remote/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installProfiles(t *testing.T, s *Server) {
	t.Helper()
	db := switchFixture(t, s)
	switchProvider(t, db, "one", "claude", `{"env":{"ANTHROPIC_BASE_URL":"https://one.example","ANTHROPIC_AUTH_TOKEN":"secret-one"}}`, `{}`, true)
	switchProvider(t, db, "two", "claude", `{"env":{"ANTHROPIC_BASE_URL":"https://two.example","ANTHROPIC_API_KEY":"secret-two"}}`, `{}`, false)
}
func TestProfileCatalogRedactedAndDefaultPinned(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	installProfiles(t, s)
	code, b := request(t, ts, "GET", "/api/profiles", nil, testToken)
	if code != 200 || strings.Contains(string(b), "secret-") || strings.Contains(string(b), "AUTH_TOKEN") {
		t.Fatalf("catalog not redacted %d", code)
	}
	var contract struct {
		SchemaVersion int             `json:"schemaVersion"`
		Source        string          `json:"source"`
		Capabilities  map[string]bool `json:"capabilities"`
	}
	if json.Unmarshal(b, &contract) != nil || contract.SchemaVersion != 1 || contract.Source != "cc-switch" || !contract.Capabilities["resumeSelection"] || !contract.Capabilities["switchSupplier"] {
		t.Fatal("missing frontend compatibility contract")
	}
	x := createSession(t, ts)
	if x.ProfileID != "ccswitch:one" {
		t.Fatalf("missing default: %+v", x)
	}
	if x.ProfileName != "one" {
		t.Fatal("creation response must identify the supplier before any message")
	}
	db, e := sql.Open("sqlite", s.cfg.CCSwitchDBPath)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("UPDATE providers SET name='Renamed' WHERE id='one'"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("UPDATE providers SET settings_config=replace(settings_config,'secret-one','changed-key')"); e != nil {
		t.Fatal(e)
	}
	fresh, _ := st.Get(x.ID)
	if fresh.ProfileName != "one" {
		t.Fatal("supplier display must use the session snapshot")
	}
	p, e := sessionProfile(fresh)
	if e != nil || p.Settings.Env["ANTHROPIC_AUTH_TOKEN"] != "secret-one" {
		t.Fatal("existing selection did not retain snapshot", e)
	}
	code, b = request(t, ts, "GET", "/api/sessions", nil, testToken)
	if code != 200 || strings.Contains(string(b), "secret-one") {
		t.Fatal("snapshot leaked")
	}
	var listed []store.Session
	if json.Unmarshal(b, &listed) != nil || len(listed) != 1 || listed[0].ProfileName != "one" {
		t.Fatal("session list must expose saved supplier name")
	}
	code, _ = request(t, ts, "POST", "/api/sessions", map[string]string{"provider": "codex", "profileId": "one", "cwd": t.TempDir()}, testToken)
	if code != 400 {
		t.Fatalf("wrong app accepted %d", code)
	}
}
func TestProfileSwitchBusyRequiresConfirmationAndResumes(t *testing.T) {
	s, ts, st := setup(t, nil)
	installProfiles(t, s)
	starts := make(chan claude.Options, 4)
	processes := make(chan *fakeProc, 4)
	s.startProcess = func(o claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		p := &fakeProc{hooks: h, done: make(chan struct{})}
		starts <- o
		processes <- p
		return p, nil
	}
	x := createSession(t, ts)
	send := func(text string) {
		t.Helper()
		code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": text}, testToken)
		if code != 202 {
			t.Fatalf("send %d %s", code, b)
		}
	}
	send("first")
	first := <-starts
	proc := <-processes
	await(t, func() bool { proc.mu.Lock(); defer proc.mu.Unlock(); return len(proc.sent) == 1 })
	proc.replay("first")
	// Native session has begun, so subsequent starts must resume its ID.
	_ = st.SetResume(x.ID)
	code, _ := request(t, ts, "POST", "/api/sessions/"+x.ID+"/profile", map[string]any{"profileId": "two"}, testToken)
	if code != 409 {
		t.Fatalf("busy switch %d", code)
	}
	before, _ := st.Get(x.ID)
	if before.ProfileID != "ccswitch:one" {
		t.Fatal("unconfirmed switch changed state")
	}
	code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/profile", map[string]any{"profileId": "two", "force": true}, testToken)
	if code != 200 {
		t.Fatalf("confirmed switch %d %s", code, b)
	}
	var updated store.Session
	_ = json.Unmarshal(b, &updated)
	if updated.ProfileID != "ccswitch:two" || strings.Contains(string(b), "secret-two") {
		t.Fatal("bad switch response")
	}
	select {
	case <-starts:
		t.Fatal("switch replayed a request")
	default:
	}
	send("continue")
	second := <-starts
	proc2 := <-processes
	if second.SessionID != first.SessionID || !second.Resume || second.ProfileEnv["ANTHROPIC_API_KEY"] != "secret-two" {
		t.Fatalf("resume/provider incorrect: resume=%v", second.Resume)
	}
	await(t, func() bool { proc2.mu.Lock(); defer proc2.mu.Unlock(); return len(proc2.sent) == 1 })
	proc2.replay("continue")
	proc2.result(nil)
}
func TestProfileUnavailableDoesNotFallBack(t *testing.T) {
	s, ts, _ := setup(t, nil)
	installProfiles(t, s)
	for _, id := range []string{"missing", "codex-profile"} {
		code, _ := request(t, ts, "POST", "/api/sessions", map[string]string{"cwd": t.TempDir(), "profileId": id}, testToken)
		if code != 400 {
			t.Fatalf("invalid catalog accepted %d", code)
		}
	}
}

func TestProfileSharedRustFixtureAndCodexOptions(t *testing.T) {
	s, ts, st := setup(t, nil)
	source := filepath.Join("testdata", "profiles-v1.json")
	b, e := os.ReadFile(source)
	if e != nil {
		t.Fatal(e)
	}
	db := switchFixture(t, s)
	var fixture profileFile
	if e = json.Unmarshal(b, &fixture); e != nil {
		t.Fatal(e)
	}
	for _, p := range fixture.Profiles {
		raw, _ := json.Marshal(p.Settings)
		switchProvider(t, db, p.ID, p.App, string(raw), `{}`, true)
	}
	code, b := request(t, ts, "POST", "/api/sessions", map[string]string{"provider": "codex", "cwd": t.TempDir()}, testToken)
	if code != 201 {
		t.Fatalf("shared fixture rejected %d %s", code, b)
	}
	var x store.Session
	_ = json.Unmarshal(b, &x)
	x, _ = st.Get(x.ID)
	o := claude.Options{ConfigDir: "original-codex-history"}
	if e = profileOptions(x, &o); e != nil {
		t.Fatal(e)
	}
	if o.ConfigDir != "original-codex-history" || o.ModelProvider != "custom" || o.ProfileEnv["OPENAI_API_KEY"] != "fixture-key-not-real" {
		t.Fatal("Codex profile failed to preserve home or inject options")
	}
	p, e := sessionProfile(x)
	if e != nil {
		t.Fatal(e)
	}
	providers := p.Settings.ConfigOverrides["model_providers"].(map[string]any)
	selected := providers["custom"].(map[string]any)
	selected["http_headers"] = map[string]any{"Authorization": "secret-argv"}
	if p.validate() == nil {
		t.Fatal("accepted inline credential")
	}
}

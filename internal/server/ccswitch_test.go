package server

import (
	"bytes"
	"cc-remote/internal/store"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func switchFixture(t *testing.T, s *Server) *sql.DB {
	t.Helper()
	s.cfg.CCSwitchDBPath = filepath.Join(t.TempDir(), "cc-switch.db")
	db, err := sql.Open("sqlite", s.cfg.CCSwitchDBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE providers(id TEXT, app_type TEXT, name TEXT, settings_config TEXT, meta TEXT DEFAULT '{}', is_current INTEGER DEFAULT 0, sort_index INTEGER, future_column TEXT, PRIMARY KEY(id,app_type)); CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT); PRAGMA user_version=999;`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func switchProvider(t *testing.T, db *sql.DB, id, app, settings, meta string, current bool) {
	t.Helper()
	_, err := db.Exec("INSERT INTO providers(id,app_type,name,settings_config,meta,is_current) VALUES(?,?,?,?,?,?)", id, app, id, settings, meta, current)
	if err != nil {
		t.Fatal(err)
	}
}
func TestCCSwitchReadOnlyProjectionAndCommonConfig(t *testing.T) {
	s, ts, _ := setup(t, nil)
	db := switchFixture(t, s)
	_, err := db.Exec(`INSERT INTO settings VALUES('common_config_claude','{"env":{"ANTHROPIC_BASE_URL":"https://shared.example","ANTHROPIC_AUTH_TOKEN":"shared-secret","ANTHROPIC_MODEL":"shared-model"},"hooks":{"ignored":true}}')`)
	if err != nil {
		t.Fatal(err)
	}
	switchProvider(t, db, "a", "claude", `{"env":{"ANTHROPIC_API_KEY":"key-a","ANTHROPIC_AUTH_TOKEN":""},"permissions":{"defaultMode":"bypassPermissions"}}`, `{"commonConfigEnabled":true}`, true)
	switchProvider(t, db, "b", "claude", `{"env":{"ANTHROPIC_BASE_URL":"https://b.example","ANTHROPIC_API_KEY":"key-b"}}`, `{}`, false)
	switchProvider(t, db, "broken", "claude", `{"env":{}}`, `{}`, false)
	local := filepath.Join(filepath.Dir(s.cfg.CCSwitchDBPath), "settings.json")
	if err = os.WriteFile(local, []byte(`{"currentProviderClaude":"b"}`), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.cfg.CCSwitchDBPath)
	code, b := request(t, ts, "GET", "/api/profiles", nil, testToken)
	if code != 200 || strings.Contains(string(b), "key-a") || strings.Contains(string(b), "shared-secret") {
		t.Fatalf("catalog status/redaction %d", code)
	}
	var catalog struct {
		Source          string            `json:"source"`
		DefaultProfiles map[string]string `json:"defaultProfiles"`
		Unavailable     []any             `json:"unavailable"`
	}
	if err = json.Unmarshal(b, &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Source != "cc-switch" || catalog.DefaultProfiles["claude"] != "ccswitch:b" || len(catalog.Unavailable) != 1 {
		t.Fatalf("wrong catalog metadata: %s", b)
	}
	p, err := s.selectProfile("claude", "a")
	if err != nil || p.Settings.Env["ANTHROPIC_BASE_URL"] != "https://shared.example" || p.Settings.Env["ANTHROPIC_MODEL"] != "shared-model" || p.Settings.Env["ANTHROPIC_AUTH_TOKEN"] != "" {
		t.Fatal("common connection merge failed", err)
	}
	after, _ := os.ReadFile(s.cfg.CCSwitchDBPath)
	if !bytes.Equal(before, after) {
		t.Fatal("source database was modified")
	}
	if _, err = s.selectProfile("claude", "broken"); err == nil {
		t.Fatal("unsupported provider accepted")
	}
	if p, err = s.selectProfile("claude", "@native"); err != nil || p.ID != "" {
		t.Fatal("native selection failed", err)
	}
}
func TestCCSwitchMissingAndInvalidSchema(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.CCSwitchDBPath = filepath.Join(t.TempDir(), "missing.db")
	if code, _ := request(t, ts, "POST", "/api/sessions", map[string]string{"cwd": t.TempDir()}, testToken); code != 201 {
		t.Fatal("missing database broke native mode")
	}
	if _, err := os.Stat(s.cfg.CCSwitchDBPath); !os.IsNotExist(err) {
		t.Fatal("created missing CC-Switch database")
	}
	db := switchFixture(t, s)
	if _, err := db.Exec("ALTER TABLE providers RENAME COLUMN settings_config TO unknown_settings"); err != nil {
		t.Fatal(err)
	}
	if code, _ := request(t, ts, "GET", "/api/profiles", nil, testToken); code != 400 {
		t.Fatalf("missing required field accepted: %d", code)
	}
}

func TestCCSwitchResumeSelectionPreservesIdentityAndHistory(t *testing.T) {
	s, ts, st := setup(t, nil)
	installProfiles(t, s)
	cwd, id := t.TempDir(), store.ID()
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "selection-resume")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	user, _ := json.Marshal(map[string]any{"type": "user", "cwd": cwd, "message": map[string]string{"content": "original context"}})
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), append(user, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"mode": "terminal", "trustFolder": true, "resume": id, "profileId": "two"}
	code, raw := request(t, ts, "POST", "/api/sessions", body, testToken)
	var x store.Session
	_ = json.Unmarshal(raw, &x)
	if code != 201 || x.ProfileID != "ccswitch:two" || x.ClaudeSessionID != id {
		t.Fatalf("resume selection %d %s", code, raw)
	}
	_, _ = st.Append(x.ID, "user", "saved context", nil)
	delete(body, "profileId")
	code, raw = request(t, ts, "POST", "/api/sessions", body, testToken)
	var y store.Session
	_ = json.Unmarshal(raw, &y)
	if code != 200 || y.ID != x.ID || y.ProfileID != x.ProfileID {
		t.Fatalf("keep selection %d %s", code, raw)
	}
	body["profileId"] = "one"
	code, raw = request(t, ts, "POST", "/api/sessions", body, testToken)
	_ = json.Unmarshal(raw, &y)
	if code != 200 || y.ID != x.ID || y.ClaudeSessionID != id || y.ProfileID != "ccswitch:one" {
		t.Fatalf("change selection %d %s", code, raw)
	}
	events, err := st.Events(x.ID, 0)
	if err != nil || len(events) != 1 || events[0].Text != "saved context" {
		t.Fatal("resume changed history")
	}
	if err = st.SetStatus(x.ID, "running"); err != nil {
		t.Fatal(err)
	}
	body["profileId"] = "two"
	if code, _ = request(t, ts, "POST", "/api/sessions", body, testToken); code != 409 {
		t.Fatal("busy resume changed supplier", code)
	}
	if err = st.SetStatus(x.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	body["profileId"] = "missing"
	if code, _ = request(t, ts, "POST", "/api/sessions", body, testToken); code != 400 {
		t.Fatal("unknown supplier accepted", code)
	}
	body["profileId"] = "@native"
	code, raw = request(t, ts, "POST", "/api/sessions", body, testToken)
	_ = json.Unmarshal(raw, &y)
	if code != 200 || y.ID != x.ID || y.ProfileID != "" {
		t.Fatal("native resume failed", code)
	}
}

func TestCCSwitchCodexConnectionProjection(t *testing.T) {
	raw := `model = "demo-model"
model_provider = "relay"
instructions = """
[model_providers.injected]
base_url = "https://wrong.example"
"""
[model_providers."relay"]
name = "Relay"
base_url = "https://relay.example/v1" # comment
wire_api = "responses"
requires_openai_auth = true
[mcp_servers.ignore]
command = "ignored"
`
	settings, _ := json.Marshal(map[string]any{"auth": map[string]string{"OPENAI_API_KEY": "fake-secret"}, "config": raw})
	p, err := normalizeCCSwitch("cx", "Codex", "codex", string(settings), "{}", "")
	if err != nil {
		t.Fatal(err)
	}
	providers := p.Settings.ConfigOverrides["model_providers"].(map[string]any)
	if len(providers) != 1 || p.Settings.ConfigOverrides["mcp_servers"] != nil || p.Settings.ConfigOverrides["instructions"] != nil {
		t.Fatal("imported non-connection config")
	}
	relay := providers["relay"].(map[string]any)
	if relay["env_key"] != "OPENAI_API_KEY" || relay["requires_openai_auth"] != false {
		t.Fatal("API key auth not normalized")
	}
	if _, err := readConnectionTOML("model_provider = { key = 'secret' }"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("unsupported syntax must fail without disclosing value")
	}
}

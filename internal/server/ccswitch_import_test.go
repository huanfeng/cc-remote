package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCCSwitchImportIndependentAndHidden(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	db := switchFixture(t, s)
	switchProvider(t, db, "api", "claude", `{"env":{"ANTHROPIC_BASE_URL":"https://api.example.com","ANTHROPIC_API_KEY":"private-import-secret","ANTHROPIC_MODEL":"model-x[1M]","ANTHROPIC_DEFAULT_SONNET_MODEL":"sonnet-x[1M]"}}`, "{}", true)
	_, b := request(t, ts, "GET", "/api/supplier-configs", nil, testToken)
	var initial supplierConfigReply
	json.Unmarshal(b, &initial)
	code, _ := request(t, ts, "PUT", "/api/supplier-import/cc-switch/visibility", map[string]any{"hidden": true, "revision": initial.Revision}, testToken)
	if code != 409 {
		t.Fatalf("must migrate current first: %d", code)
	}
	code, b = request(t, ts, "POST", "/api/supplier-import/cc-switch", map[string]string{"id": "ccswitch:api", "app": "claude", "revision": initial.Revision}, testToken)
	if code != 200 || strings.Contains(string(b), "private-import-secret") {
		t.Fatalf("unsafe import response %d", code)
	}
	var reply supplierConfigReply
	json.Unmarshal(b, &reply)
	if len(reply.Profiles) != 1 || reply.Profiles[0].SourceID != "ccswitch:api" || !reply.Profiles[0].HasCredential {
		t.Fatal("missing import")
	}
	id := reply.Profiles[0].ID
	code, _ = request(t, ts, "POST", "/api/supplier-import/cc-switch", map[string]string{"id": "ccswitch:api", "app": "claude", "revision": reply.Revision}, testToken)
	if code != 409 {
		t.Fatal("duplicate accepted")
	}
	code, b = request(t, ts, "PUT", "/api/supplier-import/cc-switch/visibility", map[string]any{"hidden": true, "revision": reply.Revision}, testToken)
	if code != 200 {
		t.Fatalf("hide %d %s", code, b)
	}
	// The database can disappear; local defaults still resolve and credentials stay private.
	db.Close()
	os.Remove(s.cfg.CCSwitchDBPath)
	p, e := s.selectProfile("claude", "")
	if e != nil || p.ID != id || p.Settings.Env["ANTHROPIC_API_KEY"] != "private-import-secret" || p.Settings.Env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "sonnet-x[1M]" || p.Settings.Env["ANTHROPIC_MODEL"] != "model-x[1M]" {
		t.Fatal("not independent", e)
	}
	code, b = request(t, ts, "GET", "/api/profiles", nil, testToken)
	if code != 200 || strings.Contains(string(b), `"id":"ccswitch:`) || strings.Contains(string(b), "private-import-secret") {
		t.Fatal("hidden or secret leak")
	}
	code, b = request(t, ts, "GET", "/api/supplier-configs", nil, testToken)
	json.Unmarshal(b, &reply)
	if !reply.CCSwitchHidden {
		t.Fatal("visibility not persisted")
	}
}
func TestImportRejectsUnrepresentableConnection(t *testing.T) {
	p, e := normalizeCCSwitch("a", "a", "claude", `{"env":{"ANTHROPIC_API_KEY":"secret","CLAUDE_CODE_SUBAGENT_MODEL":"extra"}}`, "{}", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = importCCSwitchProfile(p); e == nil {
		t.Fatal("silently discarded option")
	}
}

func TestCCSwitchFableAndDisplayNames(t *testing.T) {
	p := supplierProfile{Name: "Named", App: "claude"}
	p.Settings.Env = map[string]string{"ANTHROPIC_BASE_URL": "https://api.example", "ANTHROPIC_AUTH_TOKEN": "fixture-secret", "ANTHROPIC_DEFAULT_FABLE_MODEL": "deep[1M]", "ANTHROPIC_DEFAULT_FABLE_MODEL_NAME": "Deep", "ANTHROPIC_DEFAULT_SONNET_MODEL_NAME": "Daily"}
	q, err := importCCSwitchProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if q.ModelMappings["fable"] != "deep[1M]" || q.ModelNames["fable"] != "Deep" || q.ModelNames["sonnet"] != "Daily" {
		t.Fatal("import lost model fields")
	}
}

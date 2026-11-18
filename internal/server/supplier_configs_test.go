package server

import (
	"bytes"
	"cc-remote/internal/store"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupplierCatalogSurvivesUnavailableCCSwitch(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "corrupt"}[corrupt], func(t *testing.T) {
			s, ts, _ := setup(t, nil)
			s.cfg.DataDir = t.TempDir()
			s.cfg.CCSwitchDBPath = t.TempDir()
			if corrupt {
				s.cfg.CCSwitchDBPath = filepath.Join(s.cfg.CCSwitchDBPath, "broken.db")
				if err := os.WriteFile(s.cfg.CCSwitchDBPath, []byte("secret-invalid-db"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, b := request(t, ts, "GET", "/api/supplier-configs", nil, testToken)
			var x supplierConfigReply
			json.Unmarshal(b, &x)
			code, b := request(t, ts, "POST", "/api/supplier-configs", map[string]string{"name": "Local relay", "app": "claude", "baseUrl": "https://relay.example", "authType": "apiKey", "credential": "fixture-secret", "revision": x.Revision}, testToken)
			if code != 200 {
				t.Fatalf("%d %s", code, b)
			}
			json.Unmarshal(b, &x)
			code, b = request(t, ts, "POST", "/api/profiles/oauth", map[string]string{"name": "OAuth account", "token": "sk-ant-oat01-fixture-secret"}, testToken)
			if code != 201 {
				t.Fatalf("%d %s", code, b)
			}
			code, b = request(t, ts, "GET", "/api/profiles", nil, testToken)
			if code != 200 || !bytes.Contains(b, []byte("Local relay")) || !bytes.Contains(b, []byte("OAuth account")) || bytes.Contains(b, []byte("fixture-secret")) || bytes.Contains(b, []byte("secret-invalid-db")) {
				t.Fatalf("catalog %d %s", code, b)
			}
			var catalog profileFile
			json.Unmarshal(b, &catalog)
			if len(catalog.Unavailable) == 0 {
				t.Fatal("missing unavailable source")
			}
			if _, err := s.selectProfile("claude", x.Profiles[0].ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSupplierConfigWriteOnlyLifecycle(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	read := func() supplierConfigReply {
		t.Helper()
		code, b := request(t, ts, "GET", "/api/supplier-configs", nil, testToken)
		if code != 200 {
			t.Fatalf("%d %s", code, b)
		}
		var x supplierConfigReply
		json.Unmarshal(b, &x)
		if bytes.Contains(b, []byte("fixture-secret")) {
			t.Fatal("credential leaked")
		}
		return x
	}
	x := read()
	body := map[string]string{"name": "Relay", "app": "claude", "baseUrl": "https://relay.example/v1", "model": "opus", "authType": "apiKey", "credential": "fixture-secret", "revision": x.Revision}
	code, b := request(t, ts, "POST", "/api/supplier-configs", body, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	x = read()
	if len(x.Profiles) != 1 || !x.Profiles[0].HasCredential {
		t.Fatal(x)
	}
	id := x.Profiles[0].ID
	code, catalog := request(t, ts, "GET", "/api/profiles", nil, testToken)
	if code != 200 || bytes.Contains(catalog, []byte("fixture-secret")) || !bytes.Contains(catalog, []byte(id)) {
		t.Fatalf("catalog %d %s", code, catalog)
	}
	p, err := s.selectProfile("claude", id)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains([]byte(p.snapshot()), []byte("fixture-secret")) {
		t.Fatal("snapshot contains secret")
	}
	session := store.Session{ProfileID: id, ProfileJSON: p.snapshot(), Provider: "claude"}
	resolved, err := s.sessionProfile(session)
	if err != nil || resolved.Settings.Env["ANTHROPIC_API_KEY"] != "fixture-secret" {
		t.Fatal("launch resolution", err)
	}
	body["credential"] = ""
	body["revision"] = x.Revision
	body["name"] = "Renamed"
	code, b = request(t, ts, "PUT", "/api/supplier-configs/"+id, body, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	code, _ = request(t, ts, "PUT", "/api/supplier-configs/"+id, body, testToken)
	if code != 409 {
		t.Fatal("stale write", code)
	}
	resolved, err = s.sessionProfile(session)
	if err != nil || resolved.Settings.Env["ANTHROPIC_API_KEY"] != "fixture-secret" {
		t.Fatal("blank credential lost", err)
	}
	x = read()
	code, b = request(t, ts, "DELETE", "/api/supplier-configs/"+id, map[string]string{"revision": x.Revision}, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	if _, err = s.sessionProfile(session); err == nil {
		t.Fatal("deleted profile still launched")
	}
}

func TestSupplierConfigValidationAndCodex(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	_, b := request(t, ts, "GET", "/api/supplier-configs", nil, testToken)
	var state supplierConfigReply
	json.Unmarshal(b, &state)
	body := map[string]string{"name": "Codex relay", "app": "codex", "baseUrl": "https://relay.example/v1", "model": "gpt-5", "authType": "apiKey", "credential": "fixture-secret", "revision": state.Revision}
	for _, endpoint := range []string{"https://user:secret@relay.example/v1", "https://relay.example/v1?key=secret", "https://relay.example/#secret", "file:///tmp/key"} {
		body["baseUrl"] = endpoint
		code, b := request(t, ts, "POST", "/api/supplier-configs", body, testToken)
		if code != 400 || bytes.Contains(b, []byte("fixture-secret")) || bytes.Contains(b, []byte("user:secret")) {
			t.Fatalf("endpoint accepted or leaked: %d %s", code, b)
		}
	}
	body["baseUrl"] = "https://relay.example/v1"
	code, b := request(t, ts, "POST", "/api/supplier-configs", body, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	json.Unmarshal(b, &state)
	p, err := s.selectProfile("codex", state.Profiles[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := p.snapshot()
	if bytes.Contains([]byte(snapshot), []byte("fixture-secret")) {
		t.Fatal("secret snapshot")
	}
	resolved, err := s.sessionProfile(store.Session{ProfileID: p.ID, ProfileJSON: snapshot, Provider: "codex"})
	if err != nil || resolved.Settings.Auth["OPENAI_API_KEY"] != "fixture-secret" {
		t.Fatal("codex resolution", err)
	}
	if resolved.Settings.ConfigOverrides["model"] != "gpt-5" {
		t.Fatal("model missing")
	}
}

func TestSupplierAdvancedMappingAndValidation(t *testing.T) {
	p := supplierConfig{Name: "Test", App: "claude", BaseURL: "https://api.example", AuthType: "apiKey", Credential: "secret", Website: "https://example.com", ModelMappings: map[string]string{"haiku": "fast", "sonnet": "balanced", "opus": "best"}, TimeoutMs: 60000}
	if !validSupplierConfig(p) {
		t.Fatal("valid Claude rejected")
	}
	env := p.profile().Settings.Env
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "best" || env["API_TIMEOUT_MS"] != "60000" {
		t.Fatal(env)
	}
	p.Website = "https://user:secret@example.com"
	if validSupplierConfig(p) {
		t.Fatal("credential-bearing website accepted")
	}
	p.Website = ""
	p.TimeoutMs = -1
	if validSupplierConfig(p) {
		t.Fatal("negative timeout")
	}
	p = supplierConfig{Name: "Codex", App: "codex", BaseURL: "https://api.example", AuthType: "apiKey", Credential: "secret", CodexOptions: map[string]any{"reasoningEffort": "high", "contextWindow": float64(200000), "disableResponseStorage": true}}
	if !validSupplierConfig(p) {
		t.Fatal("valid Codex rejected")
	}
	if p.profile().Settings.ConfigOverrides["model_reasoning_effort"] != "high" {
		t.Fatal("effort not mapped")
	}
	p.CodexOptions["unknown"] = "secret"
	if validSupplierConfig(p) {
		t.Fatal("unknown accepted")
	}
	delete(p.CodexOptions, "unknown")
	p.CodexOptions["contextWindow"] = 1.5
	if validSupplierConfig(p) {
		t.Fatal("fractional tokens accepted")
	}
}
func TestSupplierAdvancedOldClientPreservesAndExplicitClears(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	_, body := request(t, ts, "GET", "/api/supplier-configs", nil, testToken)
	var x supplierConfigReply
	x = supplierConfigReply{}
	json.Unmarshal(body, &x)
	req := map[string]any{"name": "Test", "app": "claude", "baseUrl": "https://api.example", "authType": "apiKey", "credential": "fixture-secret", "revision": x.Revision, "website": "https://example.com", "notes": "work", "timeoutMs": 60000, "modelMappings": map[string]string{"opus": "best"}}
	code, body := request(t, ts, "POST", "/api/supplier-configs", req, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	x = supplierConfigReply{}
	json.Unmarshal(body, &x)
	if !x.Capabilities["advanced"] || bytes.Contains(body, []byte("fixture-secret")) {
		t.Fatal(string(body))
	}
	for _, k := range []string{"website", "notes", "timeoutMs", "modelMappings", "credential"} {
		delete(req, k)
	}
	req["revision"] = x.Revision
	code, body = request(t, ts, "PUT", "/api/supplier-configs/"+x.Profiles[0].ID, req, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	x = supplierConfigReply{}
	json.Unmarshal(body, &x)
	if x.Profiles[0].TimeoutMs != 60000 || x.Profiles[0].ModelMappings["opus"] != "best" || x.Profiles[0].Website == "" {
		t.Fatal(string(body))
	}
	req["revision"] = x.Revision
	req["website"] = ""
	req["notes"] = ""
	req["timeoutMs"] = 0
	req["modelMappings"] = map[string]string{}
	code, body = request(t, ts, "PUT", "/api/supplier-configs/"+x.Profiles[0].ID, req, testToken)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	x = supplierConfigReply{}
	json.Unmarshal(body, &x)
	if x.Profiles[0].Website != "" || x.Profiles[0].TimeoutMs != 0 || len(x.Profiles[0].ModelMappings) != 0 {
		t.Fatal(string(body))
	}
}

func TestSupplierOneMContextPersistence(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	_, b := request(t, ts, "GET", "/api/supplier-configs", nil, testToken)
	var reply supplierConfigReply
	if err := json.Unmarshal(b, &reply); err != nil {
		t.Fatal(err)
	}
	config := supplierConfig{Name: "1M relay", App: "claude", BaseURL: "https://api.example", AuthType: "authToken", Credential: "fixture-secret", Model: "main[1M]", ModelMappings: map[string]string{"sonnet": "balanced[1M]", "opus": "best[1m]", "haiku": "fast"}, Revision: reply.Revision}
	code, b := request(t, ts, "POST", "/api/supplier-configs", config, testToken)
	if code != 200 || bytes.Contains(b, []byte("fixture-secret")) {
		t.Fatalf("unsafe save: %d", code)
	}
	saved, _, err := readSupplierConfigs(s.cfg.DataDir)
	if err != nil || len(saved) != 1 {
		t.Fatal("missing config", err)
	}
	env := saved[0].profile().Settings.Env
	if env["ANTHROPIC_MODEL"] != "main[1M]" || env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "balanced[1M]" || env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "best[1m]" || env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "fast" {
		t.Fatal("context declarations changed")
	}
}

func TestSupplierModelNamesAndFableLifecycle(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	_, b := request(t, ts, "GET", "/api/supplier-configs", nil, testToken)
	var reply supplierConfigReply
	json.Unmarshal(b, &reply)
	if !reply.Capabilities["modelNames"] {
		t.Fatal("missing capability")
	}
	config := supplierConfig{Name: "Named models", App: "claude", BaseURL: "https://api.example", AuthType: "authToken", Credential: "fixture-secret", ModelMappings: map[string]string{"fable": "deep[1M]"}, ModelNames: map[string]string{"fable": "Hard tasks", "sonnet": "Daily coding"}, Revision: reply.Revision}
	code, b := request(t, ts, "POST", "/api/supplier-configs", config, testToken)
	if code != 200 {
		t.Fatalf("create %d", code)
	}
	json.Unmarshal(b, &reply)
	saved := reply.Profiles[0]
	env := saved.profile().Settings.Env
	if env["ANTHROPIC_DEFAULT_FABLE_MODEL"] != "deep[1M]" || env["ANTHROPIC_DEFAULT_FABLE_MODEL_NAME"] != "Hard tasks" || env["ANTHROPIC_DEFAULT_SONNET_MODEL_NAME"] != "Daily coding" {
		t.Fatal("model fields not mapped")
	}
	req := map[string]any{"name": saved.Name, "app": saved.App, "baseUrl": saved.BaseURL, "authType": saved.AuthType, "revision": reply.Revision}
	code, b = request(t, ts, "PUT", "/api/supplier-configs/"+saved.ID, req, testToken)
	if code != 200 {
		t.Fatal(code)
	}
	json.Unmarshal(b, &reply)
	if reply.Profiles[0].ModelNames["fable"] != "Hard tasks" {
		t.Fatal("old client lost display names")
	}
	req["revision"] = reply.Revision
	req["modelNames"] = map[string]string{}
	code, b = request(t, ts, "PUT", "/api/supplier-configs/"+saved.ID, req, testToken)
	if code != 200 {
		t.Fatal(code)
	}
	reply = supplierConfigReply{}
	json.Unmarshal(b, &reply)
	if len(reply.Profiles[0].ModelNames) != 0 {
		t.Fatal("explicit clear lost")
	}
	for _, invalid := range []map[string]string{{"unknown": "x"}, {"fable": string([]byte{10})}, {"sonnet": strings.Repeat("x", 161)}} {
		config.ModelNames = invalid
		if validSupplierConfig(config) {
			t.Fatal("invalid display name accepted")
		}
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cc-remote/internal/i18n"
	"cc-remote/internal/prefs"
	"cc-remote/internal/store"
)

func TestModelDefaultPreviewsUseProjectAndSupplier(t *testing.T) {
	isolateModels(t)
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	cwd := t.TempDir()
	project := filepath.Join(cwd, ".claude")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(s.cfg.ClaudeConfigDir, "settings.json"), `{"model":"user-model","env":{"ANTHROPIC_DEFAULT_SONNET_MODEL":"host-sonnet"}}`)
	local := filepath.Join(project, "settings.local.json")
	write(local, `{"model":"project-model","permissions":{"defaultMode":"plan"}}`)
	q := "/api/models?cwd=" + url.QueryEscape(cwd) + "&profileId="
	get := func(path string) modelList {
		t.Helper()
		code, raw := request(t, ts, http.MethodGet, path, nil, testToken)
		var list modelList
		if code != 200 || json.Unmarshal(raw, &list) != nil || strings.Contains(string(raw), "fixture-credential") {
			t.Fatalf("catalog: %d %s", code, raw)
		}
		return list
	}
	native := get(q + "@native")
	if native.DefaultModel != "project-model" || native.DefaultPermission != "plan" || native.AliasModels["sonnet"] != "host-sonnet" {
		t.Fatal(native)
	}
	_, rev, err := prefs.LoadProvider(s.cfg.DataDir, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = prefs.SaveProvider(s.cfg.DataDir, "claude", prefs.Preferences{Model: "remote-model", PermissionMode: "acceptEdits"}, rev); err != nil {
		t.Fatal(err)
	}
	if got := get(q + "@native"); got.DefaultModel != "remote-model" || got.DefaultPermission != "acceptEdits" {
		t.Fatal(got)
	}

	_, raw := request(t, ts, http.MethodGet, "/api/supplier-configs", nil, testToken)
	var configs supplierConfigReply
	if err := json.Unmarshal(raw, &configs); err != nil {
		t.Fatal(err)
	}
	code, raw := request(t, ts, http.MethodPost, "/api/supplier-configs", supplierConfig{
		App: "claude", Name: "Relay", BaseURL: "https://relay.example", AuthType: "apiKey",
		Credential: "fixture-credential", Model: "sonnet", Revision: configs.Revision,
		ModelMappings: map[string]string{"sonnet": "relay/sonnet-v3", "fable": "relay/fable-v2"},
	}, testToken)
	if code != 200 || json.Unmarshal(raw, &configs) != nil || len(configs.Profiles) != 1 {
		t.Fatalf("save supplier: %d %s", code, raw)
	}
	id := configs.Profiles[0].ID
	code, raw = request(t, ts, http.MethodPut, "/api/profiles/default", map[string]string{"app": "claude", "profileId": id}, testToken)
	if code != 200 {
		t.Fatalf("set default supplier: %d %s", code, raw)
	}
	checkRelay := func(list modelList) {
		t.Helper()
		if list.DefaultModel != "sonnet" || list.DefaultPermission != "acceptEdits" ||
			list.AliasModels["sonnet"] != "relay/sonnet-v3" || list.AliasModels["fable"] != "relay/fable-v2" {
			t.Fatal(list)
		}
	}
	checkRelay(get(q))
	code, raw = request(t, ts, http.MethodPost, "/api/sessions", map[string]string{
		"cwd": cwd, "model": "session-model", "permissionMode": "plan",
	}, testToken)
	var session store.Session
	if code != 201 || json.Unmarshal(raw, &session) != nil {
		t.Fatalf("create session: %d %s", code, raw)
	}
	checkRelay(get("/api/models?sessionId=" + session.ID))
}

func TestClaudeDefaultPreviewsLeaveRuntimeDecisionsOpen(t *testing.T) {
	isolateModels(t)
	s, _, _ := setup(t, nil)
	path := filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"default","permissions":{"defaultMode":"plan"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var list modelList
	s.previewModelDefaults(i18n.EN, store.Session{Provider: "claude", Mode: "terminal", Resume: true}, &list)
	if list.DefaultModel != "" || list.DefaultPermission != "" || len(list.AliasModels) != 0 {
		t.Fatal(list)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.previewModelDefaults(i18n.EN, store.Session{Provider: "claude", Mode: "managed"}, &list)
	if list.DefaultModel != "" || list.DefaultPermission != "default" {
		t.Fatal(list)
	}
}

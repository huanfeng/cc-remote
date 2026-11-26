package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"cc-remote/internal/store"
)

func fakeCodexModelCatalog() {
	home := os.Getenv("CODEX_HOME")
	if home != os.Getenv("FAKE_CODEX_MODEL_HOST") {
		if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("CODEX_SQLITE_HOME") != "" ||
			!slices.Contains(os.Args, `cli_auth_credentials_store="file"`) ||
			!slices.Contains(os.Args, `forced_login_method="chatgpt"`) ||
			!slices.Contains(os.Args, `model_provider="openai"`) {
			os.Exit(4)
		}
	}
	model, err := os.ReadFile(filepath.Join(home, "model-fixture"))
	if err != nil {
		os.Exit(4)
	}
	input, output := bufio.NewScanner(os.Stdin), json.NewEncoder(os.Stdout)
	for input.Scan() {
		var msg struct {
			ID     json.RawMessage
			Method string
		}
		if json.Unmarshal(input.Bytes(), &msg) != nil {
			os.Exit(4)
		}
		reply := map[string]any{"id": msg.ID}
		switch msg.Method {
		case "initialize":
			reply["result"] = struct{}{}
		case "initialized":
			continue
		case "config/read":
			reply["result"] = map[string]any{"config": map[string]string{"sandbox_mode": "workspace-write"}}
		case "model/list":
			if string(model) == "error" {
				reply["error"] = map[string]any{"code": -1, "message": "private model RPC detail"}
			} else {
				reply["result"] = map[string]any{"data": []map[string]any{{"model": string(model), "isDefault": true}}}
			}
		default:
			os.Exit(4)
		}
		if output.Encode(reply) != nil {
			os.Exit(4)
		}
	}
}

func TestCodexAccountModelCatalogIsolation(t *testing.T) {
	f := newCodexAccountSessionFixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f.server.cfg.CodexExecutable = exe
	hostHome := f.server.cfg.CodexConfigDir
	workHome, personalHome := f.server.codexAccountHome(f.work.ID), f.server.codexAccountHome(f.private.ID)
	t.Setenv("FAKE_CODEX_MODEL_HOST", hostHome)
	t.Setenv("OPENAI_API_KEY", "synthetic-host-key")
	t.Setenv("CODEX_SQLITE_HOME", t.TempDir())
	writeModel := func(home, model string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(home, "model-fixture"), []byte(model), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeModel(hostHome, "host-model")
	writeModel(workHome, "work-model")
	writeModel(personalHome, "personal-model")
	cwd := t.TempDir()
	create := func(profileID string) string {
		t.Helper()
		code, raw := request(t, f.http, http.MethodPost, "/api/sessions", map[string]string{
			"provider": "codex", "profileId": profileID, "cwd": cwd,
		}, testToken)
		var session store.Session
		if code != http.StatusCreated || json.Unmarshal(raw, &session) != nil || session.ID == "" {
			t.Fatalf("create session: %d %s", code, raw)
		}
		return session.ID
	}
	api := func(id string) string { return "/api/models?sessionId=" + id }
	check := func(path, want string) {
		t.Helper()
		code, raw := request(t, f.http, http.MethodGet, path, nil, testToken)
		var list modelList
		if code != http.StatusOK || json.Unmarshal(raw, &list) != nil || len(list.Warnings) != 0 ||
			len(list.Models) != 1 || list.Models[0].ID != want || !list.Models[0].IsDefault ||
			list.DefaultModel != want || list.DefaultPermission != "workspace-write" {
			t.Fatalf("model catalog for %s: want %s, got %d %s", path, want, code, raw)
		}
	}
	host, work, personal := create("@native"), create(f.work.ID), create(f.private.ID)
	check(api(host), "host-model")
	check(api(work), "work-model")
	check(api(personal), "personal-model")
	check("/api/models?provider=codex&codexAccountId="+f.private.ID, "personal-model")
	check("/api/models?provider=codex&profileId="+f.work.ID, "work-model")
	check(api(work)+"&codexAccountId="+f.private.ID, "work-model")
	writeModel(workHome, "work-model-refreshed")
	check(api(work), "work-model")
	check(api(work)+"&refresh=1", "work-model-refreshed")
	check(api(personal), "personal-model")
	code, raw := request(t, f.http, http.MethodPost, "/api/sessions/"+work+"/commands", map[string]string{"command": "/model"}, testToken)
	if code != http.StatusOK || !strings.Contains(string(raw), "work-model-refreshed") {
		t.Fatalf("model command did not use the original account: %d %s", code, raw)
	}
	writeModel(personalHome, "error")
	code, raw = request(t, f.http, http.MethodGet, api(personal)+"&refresh=1", nil, testToken)
	if code != http.StatusOK || !strings.Contains(string(raw), `"warnings"`) || strings.Contains(string(raw), "private model RPC detail") {
		t.Fatalf("model catalog leaked account diagnostics: %d %s", code, raw)
	}
	f.work.Disabled = true
	if err := f.server.saveCodexAccounts([]codexAccount{f.work, f.private}); err != nil {
		t.Fatal(err)
	}
	code, raw = request(t, f.http, http.MethodGet, api(work), nil, testToken)
	if code != http.StatusOK || !strings.Contains(string(raw), `"models":[]`) || !strings.Contains(string(raw), `"warnings"`) {
		t.Fatalf("disabled account received models: %d %s", code, raw)
	}
}

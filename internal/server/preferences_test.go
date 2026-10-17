package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

// isolateModels keeps model discovery away from the machine's real policy and credentials.
func isolateModels(t *testing.T) {
	t.Helper()
	old := managedSettingsPath
	missing := filepath.Join(t.TempDir(), "managed-settings.json")
	managedSettingsPath = func() string { return missing }
	t.Cleanup(func() { managedSettingsPath = old })
	for _, key := range append([]string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL"}, modelEnvKeys...) {
		t.Setenv(key, "")
	}
}

func TestPreferencesAPIConflictAndDangerousConfirm(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	code, b := request(t, ts, "GET", "/api/preferences", nil, testToken)
	var p preferencesBody
	if code != 200 || json.Unmarshal(b, &p) != nil || p.PermissionMode != "" || p.Revision == "" {
		t.Fatalf("get %d %s", code, b)
	}
	stale := p.Revision
	p.PermissionMode, p.Model, p.Effort = "bypassPermissions", "opus", "high"
	if code, b = request(t, ts, "PUT", "/api/preferences", p, testToken); code != 400 || !strings.Contains(string(b), "confirm") {
		t.Fatalf("dangerous mode without confirm %d %s", code, b)
	}
	p.ConfirmDangerous = true
	if code, b = request(t, ts, "PUT", "/api/preferences", p, testToken); code != 200 {
		t.Fatalf("put %d %s", code, b)
	}
	var saved preferencesBody
	_ = json.Unmarshal(b, &saved)
	if saved.PermissionMode != "bypassPermissions" || saved.Revision == stale {
		t.Fatalf("saved %+v", saved)
	}
	// Unchanged dangerous mode does not need a second confirmation.
	saved.Effort = "low"
	if code, b = request(t, ts, "PUT", "/api/preferences", saved, testToken); code != 200 {
		t.Fatalf("keep mode %d %s", code, b)
	}
	if code, _ = request(t, ts, "PUT", "/api/preferences", saved, testToken); code != 409 {
		t.Fatalf("stale revision %d", code)
	}
	if code, _ = request(t, ts, "PUT", "/api/preferences", map[string]string{"permissionMode": "yolo", "revision": stale}, testToken); code != 400 {
		t.Fatalf("invalid mode %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(s.cfg.DataDir, "preferences.json"))
	if !strings.Contains(string(data), `"effort": "low"`) {
		t.Fatalf("file %s", data)
	}
}

func TestSessionOverridesResolveIntoRunOptions(t *testing.T) {
	runs := make(chan claude.Options, 4)
	s, ts, st := setup(t, func(_ context.Context, o claude.Options, _ func(claude.Event) error) error { runs <- o; return nil })
	s.cfg.DataDir = t.TempDir()
	code, b := request(t, ts, "GET", "/api/preferences", nil, testToken)
	var p preferencesBody
	_ = json.Unmarshal(b, &p)
	p.PermissionMode, p.Model, p.Effort = "acceptEdits", "sonnet", "medium"
	if code, b = request(t, ts, "PUT", "/api/preferences", p, testToken); code != 200 {
		t.Fatalf("prefs %d %s", code, b)
	}
	if code, _ = request(t, ts, "POST", "/api/sessions", map[string]string{"title": "x", "cwd": t.TempDir(), "permissionMode": "bypassPermissions"}, testToken); code != 400 {
		t.Fatalf("dangerous create without confirm %d", code)
	}
	code, b = request(t, ts, "POST", "/api/sessions", map[string]any{"title": "x", "cwd": t.TempDir(), "permissionMode": "bypassPermissions", "model": "opus", "confirmDangerous": true}, testToken)
	var x store.Session
	if code != 201 || json.Unmarshal(b, &x) != nil || x.PermissionMode != "bypassPermissions" || x.Model != "opus" || x.Effort != "" {
		t.Fatalf("create %d %s", code, b)
	}
	send := func() claude.Options {
		t.Helper()
		if code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "hi"}, testToken); code != 202 {
			t.Fatalf("send %d %s", code, b)
		}
		o := <-runs
		await(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.runs[x.ID] == nil })
		return o
	}
	if o := send(); o.PermissionMode != "bypassPermissions" || o.Model != "opus" || o.Effort != "medium" {
		t.Fatalf("effective %+v", o)
	}
	code, b = request(t, ts, "PATCH", "/api/sessions/"+x.ID, map[string]string{"permissionMode": "", "effort": "max"}, testToken)
	if code != 200 {
		t.Fatalf("patch %d %s", code, b)
	}
	if o := send(); o.PermissionMode != "acceptEdits" || o.Model != "opus" || o.Effort != "max" {
		t.Fatalf("after patch %+v", o)
	}
	if code, _ = request(t, ts, "PATCH", "/api/sessions/"+x.ID, map[string]string{"permissionMode": "dontAsk"}, testToken); code != 400 {
		t.Fatalf("dangerous patch without confirm %d", code)
	}
	if code, _ = request(t, ts, "PATCH", "/api/sessions/"+x.ID, map[string]string{"effort": "huge"}, testToken); code != 400 {
		t.Fatalf("invalid effort %d", code)
	}
	_ = st.SetMode(x.ID, "channel")
	if code, b = request(t, ts, "PATCH", "/api/sessions/"+x.ID, map[string]string{"model": "haiku"}, testToken); code != 409 || !strings.Contains(string(b), "/model") {
		t.Fatalf("channel patch %d %s", code, b)
	}
}

func TestModelAndPermissionCommandsUseSessionAndPreferences(t *testing.T) {
	isolateModels(t)
	s, ts, st := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	x := createSession(t, ts)
	path := "/api/sessions/" + x.ID + "/commands"
	code, b := request(t, ts, "POST", path, map[string]string{"command": "/model"}, testToken)
	if code != 200 || !strings.Contains(string(b), "opusplan") {
		t.Fatalf("list %d %s", code, b)
	}
	if code, b = request(t, ts, "POST", path, map[string]string{"command": "/model claude-opus-4-7"}, testToken); code != 200 {
		t.Fatalf("session model %d %s", code, b)
	}
	if code, b = request(t, ts, "POST", path, map[string]string{"command": "/effort --default high"}, testToken); code != 200 {
		t.Fatalf("default effort %d %s", code, b)
	}
	if code, b = request(t, ts, "POST", path, map[string]string{"command": "/permission-mode bypassPermissions"}, testToken); code != 200 || !strings.Contains(string(b), "approvals") {
		t.Fatalf("permission mode %d %s", code, b)
	}
	got, _ := st.Get(x.ID)
	if got.Model != "claude-opus-4-7" || got.PermissionMode != "bypassPermissions" || got.Effort != "" {
		t.Fatalf("session %+v", got)
	}
	if p := s.effectivePreferences(got); p.Effort != "high" {
		t.Fatalf("effective %+v", p)
	}
	if code, _ = request(t, ts, "POST", path, map[string]string{"command": "/permission-mode inherit"}, testToken); code != 200 {
		t.Fatal("inherit")
	}
	if got, _ = st.Get(x.ID); got.PermissionMode != "" {
		t.Fatalf("not cleared %+v", got)
	}
	if code, _ = request(t, ts, "POST", path, map[string]string{"command": "/permission-mode yolo"}, testToken); code != 400 {
		t.Fatalf("invalid mode %d", code)
	}
	// "default" is Claude Code's model alias and is passed through; inherit clears.
	if code, b = request(t, ts, "POST", path, map[string]string{"command": "/model default"}, testToken); code != 200 {
		t.Fatalf("default alias %d %s", code, b)
	}
	if got, _ = st.Get(x.ID); got.Model != "default" {
		t.Fatalf("default alias not kept %+v", got)
	}
	if code, _ = request(t, ts, "POST", path, map[string]string{"command": "/model inherit"}, testToken); code != 200 {
		t.Fatal("model inherit")
	}
	if got, _ = st.Get(x.ID); got.Model != "" {
		t.Fatalf("model not cleared %+v", got)
	}
	for _, bad := range []string{"/model -p", "/model --dangerously-skip-permissions", "/model a;b", "/model --default -x"} {
		if code, _ = request(t, ts, "POST", path, map[string]string{"command": bad}, testToken); code != 400 {
			t.Fatalf("%s accepted %d", bad, code)
		}
	}
	if code, _ = request(t, ts, "PATCH", "/api/sessions/"+x.ID, map[string]string{"model": "-p"}, testToken); code != 400 {
		t.Fatalf("patch flag-like model %d", code)
	}
	if _, err := os.Stat(filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")); !os.IsNotExist(err) {
		t.Fatal("commands wrote Claude settings.json")
	}
	_ = st.SetMode(x.ID, "channel")
	if code, _ = request(t, ts, "POST", path, map[string]string{"command": "/model haiku"}, testToken); code != 409 {
		t.Fatalf("channel model %d", code)
	}
}

func TestSettingsWriterPreservesFormattingBacksUpAndConfirmsDangerous(t *testing.T) {
	s, ts, _ := setup(t, nil)
	path := filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")
	original := "{\r\n    \"env\": {\r\n        \"PRIVATE_KEY\": \"secret\"\r\n    },\r\n    \"permissions\": {\r\n        \"allow\": [\"Bash(git status)\"],\r\n        \"defaultMode\": \"default\"\r\n    },\r\n    \"model\": \"sonnet\",\r\n    \"hooks\": {}\r\n}\r\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	_, b := request(t, ts, "GET", "/api/claude/settings", nil, testToken)
	var x claudeSettings
	_ = json.Unmarshal(b, &x)
	x.PermissionMode = "bypassPermissions"
	if code, b := request(t, ts, "PUT", "/api/claude/settings", x, testToken); code != 400 || !strings.Contains(string(b), "confirm") {
		t.Fatalf("no confirm %d %s", code, b)
	}
	x.ConfirmDangerous = true
	code, b := request(t, ts, "PUT", "/api/claude/settings", x, testToken)
	if code != 200 {
		t.Fatalf("write %d %s", code, b)
	}
	after, _ := os.ReadFile(path)
	if want := strings.Replace(original, `"defaultMode": "default"`, `"defaultMode": "bypassPermissions"`, 1); string(after) != want {
		t.Fatalf("formatting changed:\n%q\nwant\n%q", after, want)
	}
	if backup, _ := os.ReadFile(path + ".cc-remote.bak"); string(backup) != original {
		t.Fatalf("backup %q", backup)
	}
	_ = json.Unmarshal(b, &x)
	x.Model, x.ConfirmDangerous = "", false
	if code, b = request(t, ts, "PUT", "/api/claude/settings", x, testToken); code != 200 {
		t.Fatalf("clear model %d %s", code, b)
	}
	after, _ = os.ReadFile(path)
	if strings.Contains(string(after), "model") || !strings.Contains(string(after), "    },\r\n    \"hooks\": {}\r\n}") {
		t.Fatalf("remove model %q", after)
	}
	if backup, _ := os.ReadFile(path + ".cc-remote.bak"); string(backup) != original {
		t.Fatal("backup replaced within the same process")
	}
}

func TestModelsMergeSourcesRestrictAndDegrade(t *testing.T) {
	isolateModels(t)
	s, ts, _ := setup(t, nil)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" || r.Header.Get("anthropic-version") != "2023-06-01" || r.URL.Path != "/v1/models" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"data":[{"id":"claude-api-model-1","display_name":"API Model"},{"id":"sonnet","display_name":"dup"}]}`))
	}))
	defer api.Close()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", api.URL)
	t.Setenv("ANTHROPIC_DEFAULT_OPUS_MODEL", "claude-env-opus")
	t.Setenv("CC_REMOTE_UNRELATED_SECRET", "do-not-leak")
	settings := `{"model":"claude-settings-model","env":{"ANTHROPIC_DEFAULT_HAIKU_MODEL":"claude-haiku-env","SECRET_TOKEN":"hidden"}}`
	if err := os.WriteFile(filepath.Join(s.cfg.ClaudeConfigDir, "settings.json"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "p")
	_ = os.MkdirAll(project, 0700)
	_ = os.WriteFile(filepath.Join(project, "abcd1234-1234-1234-1234-123456789abc.jsonl"), []byte(`{"type":"assistant","message":{"model":"claude-recent-1"}}`+"\n"), 0600)

	list := s.modelList(i18n.EN, "", true)
	sources := map[string]string{}
	for _, m := range list.Models {
		if _, dup := sources[m.ID]; dup {
			t.Fatalf("duplicate %s", m.ID)
		}
		sources[m.ID] = m.Source
	}
	for id, src := range map[string]string{"opusplan": "alias", "sonnet": "alias", "claude-settings-model": "settings", "claude-haiku-env": "settings", "claude-env-opus": "env", "claude-recent-1": "recent", "claude-api-model-1": "api"} {
		if sources[id] != src {
			t.Fatalf("%s source %q in %+v", id, sources[id], list)
		}
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "hidden") || strings.Contains(string(raw), "do-not-leak") || strings.Contains(string(raw), "test-key") || list.Restricted || len(list.Warnings) != 0 {
		t.Fatalf("leak or warnings %s", raw)
	}

	// availableModels narrows every source to the allow-list plus default.
	cwd := t.TempDir()
	_ = os.MkdirAll(filepath.Join(cwd, ".claude"), 0700)
	_ = os.WriteFile(filepath.Join(cwd, ".claude", "settings.json"), []byte(`{"availableModels":["sonnet","claude-api-model"]}`), 0600)
	list = s.modelList(i18n.EN, cwd, false)
	ids := []string{}
	for _, m := range list.Models {
		ids = append(ids, m.ID)
	}
	if got := strings.Join(ids, ","); !list.Restricted || got != "default,sonnet,claude-api-model,claude-api-model-1" {
		t.Fatalf("restricted %s", got)
	}

	// API failure degrades to a warning; cache serves until refresh.
	t.Setenv("ANTHROPIC_API_KEY", "wrong")
	cached := s.modelList(i18n.EN, "", false)
	if len(cached.Warnings) != 0 {
		t.Fatal("cache not used")
	}
	code, b := request(t, ts, "GET", "/api/models?refresh=1", nil, testToken)
	var fresh modelList
	if code != 200 || json.Unmarshal(b, &fresh) != nil || len(fresh.Warnings) != 1 || !strings.Contains(fresh.Warnings[0], "401") {
		t.Fatalf("degrade %d %s", code, b)
	}
	for _, m := range fresh.Models {
		if m.Source == "api" {
			t.Fatal("api models after failure")
		}
	}
}

func TestModelsBaseURLAndCredentialLayers(t *testing.T) {
	for raw, ok := range map[string]bool{"": true, "https://proxy.example/anthropic/": true, "http://127.0.0.1:9000": true, "http://localhost:1": true, "http://[::1]:2": true,
		"http://proxy.example": false, "http://10.0.0.1": false, "ftp://x": false, "https://user:pw@x": false, "not a url": false} {
		if _, err := modelsBaseURL(i18n.EN, raw); (err == nil) != ok {
			t.Errorf("%q: %v", raw, err)
		}
	}
	isolateModels(t)
	user := modelSettings{Env: map[string]json.RawMessage{"ANTHROPIC_API_KEY": json.RawMessage(`"settings-key"`), "ANTHROPIC_BASE_URL": json.RawMessage(`"https://settings.example"`)}}
	if k, _, base := apiCredentials(user); k != "settings-key" || base != "https://settings.example" {
		t.Fatalf("settings layer %q %q", k, base)
	}
	// A process-env key must never be paired with the settings base URL.
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	if k, _, base := apiCredentials(user); k != "env-key" || base != "" {
		t.Fatalf("env layer %q %q", k, base)
	}
}

func TestModelsAPIDoesNotFollowRedirects(t *testing.T) {
	isolateModels(t)
	leaked := make(chan string, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked <- r.Header.Get("x-api-key") }))
	defer target.Close()
	api := httptest.NewServer(http.RedirectHandler(target.URL+"/v1/models", http.StatusTemporaryRedirect))
	defer api.Close()
	t.Setenv("ANTHROPIC_API_KEY", "secret-key")
	t.Setenv("ANTHROPIC_BASE_URL", api.URL)
	_, attempted, err := fetchAPIModels(i18n.EN, modelSettings{})
	if !attempted || err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("redirect result %v %v", attempted, err)
	}
	select {
	case k := <-leaked:
		t.Fatalf("redirect followed, key %q", k)
	default:
	}
}

func TestModelListConcurrentCallersShareOneCollection(t *testing.T) {
	isolateModels(t)
	s, _, _ := setup(t, nil)
	done := make(chan modelList, 8)
	for range 8 {
		go func() { done <- s.modelList(i18n.EN, "", false) }()
	}
	for range 8 {
		if l := <-done; len(l.Models) == 0 {
			t.Fatal("empty list")
		}
	}
}

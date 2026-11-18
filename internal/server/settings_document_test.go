package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeDocumentPreservesSecretsAndRevision(t *testing.T) {
	s, ts, _ := setup(t, nil)
	path := filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")
	before := `{"model":"sonnet","env":{"SECRET":"never-display"},"hooks":{"Start":["secret-command"]},"customArray":[1,{"secret":"private"}],"permissions":{"defaultMode":"default","customToken":"hidden-token"}}`
	if err := os.WriteFile(path, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	code, body := request(t, ts, "GET", "/api/claude/settings", nil, testToken)
	if code != 200 || strings.Contains(string(body), "never-display") || strings.Contains(string(body), "secret-command") || strings.Contains(string(body), "hidden-token") || strings.Contains(string(body), "private") {
		t.Fatalf("unsafe snapshot %d", code)
	}
	var x claudeSettings
	if err := json.Unmarshal(body, &x); err != nil {
		t.Fatal(err)
	}
	x.EditDocument = true
	x.Document["model"] = json.RawMessage(`"opus"`)
	x.Document["language"] = json.RawMessage(`"zh-CN"`)
	delete(x.Document, "env")
	code, body = request(t, ts, "PUT", "/api/claude/settings", x, testToken)
	if code != 200 {
		t.Fatalf("save %d %s", code, body)
	}
	saved, _ := os.ReadFile(path)
	for _, expected := range []string{"never-display", "secret-command", "hidden-token", "private", "opus", "zh-CN"} {
		if !strings.Contains(string(saved), expected) {
			t.Fatalf("lost %s", expected)
		}
	}
	if code, _ = request(t, ts, "PUT", "/api/claude/settings", x, testToken); code != 409 {
		t.Fatalf("revision %d", code)
	}
	json.Unmarshal(body, &x)
	x.EditDocument = true
	x.Document["model"] = json.RawMessage(`42`)
	if code, _ = request(t, ts, "PUT", "/api/claude/settings", x, testToken); code != 400 {
		t.Fatalf("invalid model %d", code)
	}
	x.Document = nil
	if code, _ = request(t, ts, "PUT", "/api/claude/settings", x, testToken); code != 400 {
		t.Fatalf("invalid object %d", code)
	}
}

func TestNativeDocumentDeletedPermissionContainerKeepsPrivateExtensions(t *testing.T) {
	old := map[string]json.RawMessage{"permissions": json.RawMessage(`{"defaultMode":"default","allow":[],"customToken":"private"}`)}
	next := restoreNativeDocument(map[string]json.RawMessage{}, old)
	if !strings.Contains(string(next["permissions"]), "private") || strings.Contains(string(next["permissions"]), "defaultMode") {
		t.Fatal("private extension was deleted or public removal ignored")
	}
}

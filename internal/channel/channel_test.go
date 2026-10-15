package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionSwitchDisconnectsBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-changed")
	c := &client{bindingFile: path}
	var out bytes.Buffer
	if err := sessionHook(strings.NewReader(`{"session_id":"expected"}`), &out, "expected", path); err != nil {
		t.Fatal(err)
	}
	if c.bindingChanged() {
		t.Fatal("initial session disconnected")
	}
	if err := sessionHook(strings.NewReader(`{"session_id":"different"}`), &out, "expected", path); err != nil {
		t.Fatal(err)
	}
	if !c.bindingChanged() {
		t.Fatal("changed conversation kept old web binding")
	}
}

func TestMCPNegotiationAndReplyPermission(t *testing.T) {
	var events []map[string]any
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing auth")
		}
		if strings.HasSuffix(r.URL.Path, "register") {
			io.WriteString(w, `{"connectionKey":"connection-secret"}`)
			return
		}
		if r.Header.Get("X-Channel-Key") != "connection-secret" {
			t.Error("missing generation key")
		}
		if strings.HasSuffix(r.URL.Path, "events") {
			var e map[string]any
			_ = json.NewDecoder(r.Body).Decode(&e)
			events = append(events, e)
		}
		io.WriteString(w, `{}`)
	}))
	defer host.Close()
	c := &client{host: host.URL, token: "test-secret", sessionID: "test", cwd: t.TempDir(), http: localClient()}
	if err := c.register(context.Background()); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2026-07-28\"}}\n" +
		"{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"reply\",\"arguments\":{\"text\":\"hello from CC\"}}}\n" +
		"{\"jsonrpc\":\"2.0\",\"method\":\"notifications/claude/channel/permission_request\",\"params\":{\"request_id\":\"abcde\",\"tool_name\":\"Write\"}}\n")
	var out bytes.Buffer
	if err := serveMCP(context.Background(), c, input, &out); err != nil {
		t.Fatal(err)
	}
	var init map[string]any
	if err := json.Unmarshal(bytes.Split(out.Bytes(), []byte("\n"))[0], &init); err != nil {
		t.Fatal(err)
	}
	result := init["result"].(map[string]any)
	if result["protocolVersion"] != "2024-11-05" || result["instructions"] == nil {
		t.Fatalf("incompatible init %+v", result)
	}
	if len(events) != 2 || events[0]["text"] != "hello from CC" || events[1]["kind"] != "channel_permission" {
		t.Fatalf("events %+v", events)
	}
}
func TestChannelConfigAndOriginBoundaries(t *testing.T) {
	for _, host := range []string{"https://example.com", "http://127.0.0.1:8787/path", "file:///tmp", "http://token@localhost", "http://localhost?secret=yes"} {
		if validateHost(host) == nil {
			t.Errorf("accepted %s", host)
		}
	}
	for _, host := range []string{"http://127.0.0.1:8787", "http://localhost:8787", "http://[::1]:8787"} {
		if err := validateHost(host); err != nil {
			t.Error(err)
		}
	}
	path, err := writeConfig(t.TempDir(), "host.exe", "http://127.0.0.1:8787", "secret", "test-session", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCP map[string]struct {
			Command string            `json:"command"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(b, &cfg) != nil || cfg.MCP["cc-remote"].Env["CC_REMOTE_TOKEN"] != "secret" {
		t.Fatal("missing config")
	}
	env := replaceEnv([]string{"claude_config_dir=old", "KEEP=yes"}, "CLAUDE_CONFIG_DIR", "new", true)
	if len(env) != 2 || env[1] != "CLAUDE_CONFIG_DIR=new" {
		t.Fatalf("env %+v", env)
	}
}

package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testID = "11111111-2222-3333-4444-555555555555"

func TestMain(m *testing.M) {
	if mode := os.Getenv("CC_REMOTE_TEST_CHILD"); mode != "" {
		if mode == "sleep" {
			time.Sleep(time.Minute)
			os.Exit(0)
		}
		if mode == "no-result" {
			fmt.Println(`{"type":"system","subtype":"init"}`)
			os.Exit(0)
		}
		prompt, _ := io.ReadAll(os.Stdin)
		if mode == "env" {
			prompt, _ = json.Marshal(map[string]string{"config": os.Getenv("CLAUDE_CONFIG_DIR"), "url": os.Getenv("CC_REMOTE_BRIDGE_URL"), "token": os.Getenv("CC_REMOTE_BRIDGE_TOKEN")})
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "system", "subtype": "init", "session_id": testID})
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": string(prompt)}}}})
		fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"done"}`)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestConfigAndBridgeEnvironment(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_REMOTE_TEST_CHILD", "env")
	t.Setenv("CLAUDE_CONFIG_DIR", "inherited-config")
	t.Setenv("CC_REMOTE_BRIDGE_URL", "inherited-url")
	t.Setenv("CC_REMOTE_BRIDGE_TOKEN", "inherited-token")
	o := Options{Executable: exe, HostExecutable: exe, SessionID: testID, Cwd: t.TempDir(), ConfigDir: t.TempDir(), Message: "hello", BridgeURL: "http://127.0.0.1:1234/api/bridge/request", BridgeToken: "run-secret"}
	var received string
	if err = Run(context.Background(), o, func(e Event) error {
		if e.Kind == "assistant" {
			received = e.Text
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err = json.Unmarshal([]byte(received), &got); err != nil {
		t.Fatal(err)
	}
	if got["config"] != o.ConfigDir || got["url"] != o.BridgeURL || got["token"] != o.BridgeToken {
		t.Fatal(got)
	}
	windowsEnv := replaceEnv([]string{"Path=keep", "claude_config_dir=old", "CLAUDE_CONFIG_DIR=duplicate"}, "CLAUDE_CONFIG_DIR", "new", true)
	if !reflect.DeepEqual(windowsEnv, []string{"Path=keep", "CLAUDE_CONFIG_DIR=new"}) {
		t.Fatal(windowsEnv)
	}
	if got := runEnvironment([]string{"CLAUDE_CONFIG_DIR=existing"}, Options{BridgeURL: "url", BridgeToken: "token"}); got[0] != "CLAUDE_CONFIG_DIR=existing" {
		t.Fatal(got)
	}
}

func TestRunPromptAndCancellation(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Executable: exe, HostExecutable: exe, SessionID: testID, Cwd: t.TempDir(), Message: "hello\nsecond line", BridgeURL: "http://127.0.0.1:1234/api/bridge/request", BridgeToken: "secret"}
	t.Setenv("CC_REMOTE_TEST_CHILD", "run")
	var got []Event
	if err := Run(context.Background(), o, func(e Event) error { got = append(got, e); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Kind != "session" || got[1].Text != o.Message || got[2].Kind != "result" {
		t.Fatalf("unexpected events: %+v", got)
	}
	t.Setenv("CC_REMOTE_TEST_CHILD", "no-result")
	if err = Run(context.Background(), o, func(Event) error { return nil }); err == nil || !strings.Contains(err.Error(), "without a result") {
		t.Fatalf("missing result error: %v", err)
	}
	t.Setenv("CC_REMOTE_TEST_CHILD", "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = Run(ctx, o, func(Event) error { return nil })
	if err != context.DeadlineExceeded {
		t.Fatalf("cancel error: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("cancel failed to stop process promptly")
	}
}

func TestCommandAndConfig(t *testing.T) {
	o := Options{SessionID: testID, HostExecutable: `C:\Program Files\CC Remote\remote.exe`, BridgeURL: "http://127.0.0.1:1/api/bridge/request", BridgeToken: "secret"}
	args := commandArgs(o, "settings.json", "mcp.json")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--session-id "+testID) || strings.Contains(joined, "--resume") || strings.Contains(joined, "bypass") || strings.Contains(joined, "--bare") {
		t.Fatal(joined)
	}
	o.Resume = true
	args = commandArgs(o, "settings.json", "mcp.json")
	if !strings.Contains(strings.Join(args, " "), "--resume "+testID) {
		t.Fatal(args)
	}
	s, m, err := writeConfigs(t.TempDir(), o)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s)
	var settings map[string]any
	if err = json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	hook := settings["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if hook["command"] != o.HostExecutable || !reflect.DeepEqual(hook["args"], []any{"hook"}) {
		t.Fatal(hook)
	}
	data, _ = os.ReadFile(m)
	if !bytes.Contains(data, []byte("permission-mcp")) || !bytes.Contains(data, []byte("CC_REMOTE_BRIDGE_TOKEN")) {
		t.Fatal(string(data))
	}
}

func TestParseEvents(t *testing.T) {
	tests := []struct {
		line, kind, text string
		fail             bool
	}{
		{`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}}`, "assistant_delta", "hi", false},
		{`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"pwd"}}]}}`, "assistant", "hello", false},
		{`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`, "tool_result", "ok", false},
		{`{"type":"result","is_error":true,"errors":["auth failed"]}`, "error", "auth failed", true},
		{`{"type":"result","is_error":false,"result":"done","permission_denials":[{"tool_name":"Bash"}]}`, "result", "done", true},
	}
	for _, tt := range tests {
		t.Run(tt.kind+tt.text, func(t *testing.T) {
			events, _, err := parseEvent([]byte(tt.line))
			if (err != nil) != tt.fail || len(events) == 0 || events[0].Kind != tt.kind || events[0].Text != tt.text {
				t.Fatalf("events=%+v err=%v", events, err)
			}
		})
	}
	e, _, err := parseEvent([]byte(`{"type":"user","message":{"content":"already displayed"}}`))
	if err != nil || len(e) != 0 {
		t.Fatal(e, err)
	}
}

func bridgeServer(t *testing.T, decision string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bridge auth")
		}
		var req bridgeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.ToolName == "" || req.Input == nil {
			t.Errorf("invalid request %+v", req)
		}
		_ = json.NewEncoder(w).Encode(bridgeResponse{Decision: decision, Answers: map[string]string{"Which framework?": "React"}})
	}))
	t.Cleanup(s.Close)
	t.Setenv("CC_REMOTE_BRIDGE_URL", s.URL)
	t.Setenv("CC_REMOTE_BRIDGE_TOKEN", "test-token")
	return s
}

func TestQuestionAndPlanHook(t *testing.T) {
	bridgeServer(t, "allow")
	for _, name := range []string{"AskUserQuestion", "ExitPlanMode"} {
		input := fmt.Sprintf(`{"tool_name":%q,"tool_input":{"questions":[{"question":"Which framework?"}],"plan":"Make it"}}`, name)
		var out bytes.Buffer
		if err := runHook(strings.NewReader(input), &out); err != nil {
			t.Fatal(err)
		}
		var v struct {
			Output struct {
				Decision string         `json:"permissionDecision"`
				Input    map[string]any `json:"updatedInput"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if v.Output.Decision != "allow" || v.Output.Input["questions"] == nil || v.Output.Input["plan"] != "Make it" {
			t.Fatal(out.String())
		}
		if name == "AskUserQuestion" && v.Output.Input["answers"].(map[string]any)["Which framework?"] != "React" {
			t.Fatal(out.String())
		}
	}
}

func TestHookFailsClosed(t *testing.T) {
	t.Setenv("CC_REMOTE_BRIDGE_URL", "https://example.com")
	t.Setenv("CC_REMOTE_BRIDGE_TOKEN", "token")
	var out bytes.Buffer
	if err := runHook(strings.NewReader(`{"tool_name":"ExitPlanMode","tool_input":{}}`), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"permissionDecision":"deny"`) {
		t.Fatal(out.String())
	}
}

func TestPermissionMCP(t *testing.T) {
	bridgeServer(t, "allow")
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"approval","arguments":{"tool_name":"Bash","input":{"command":"pwd"}}}}
`
	var out bytes.Buffer
	if err := runPermissionMCP(strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatal(out.String())
	}
	var v struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &v); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	_ = json.Unmarshal([]byte(v.Result.Content[0].Text), &result)
	if result["behavior"] != "allow" || result["updatedInput"].(map[string]any)["command"] != "pwd" {
		t.Fatal(result)
	}
}

func TestNativeHistory(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "projects", "-tmp-project")
	if err := os.MkdirAll(filepath.Join(project, testID, "subagents"), 0700); err != nil {
		t.Fatal(err)
	}
	lines := `{"type":"user","cwd":"/tmp/project","message":{"content":"Implement feature"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Sure"},{"type":"tool_use","id":"tool1","name":"Read","input":{}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool1","content":"content"}]}}
{"type":"user","isMeta":true,"message":{"content":"internal"}}
{"type":"user","isSidechain":true,"message":{"content":"subagent"}}
{"type":"custom-title","customTitle":"My feature"}
`
	if err := os.WriteFile(filepath.Join(project, testID+".jsonl"), []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(project, testID, "subagents", testID+".jsonl"), []byte(lines), 0600)
	list, err := ListNative(dir)
	if err != nil || len(list) != 1 || list[0].Title != "My feature" || list[0].Cwd != "/tmp/project" {
		t.Fatal(list, err)
	}
	s, events, err := NativeHistory(dir, testID)
	if err != nil || s.Title != "My feature" || len(events) != 4 {
		t.Fatal(s, events, err)
	}
	if _, _, err = NativeHistory(dir, "../../secrets"); err == nil {
		t.Fatal("accepted invalid ID")
	}
	if list, err = ListNative(filepath.Join(dir, "missing")); err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
}

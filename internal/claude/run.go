// Package claude runs Claude Code's structured CLI without allocating a terminal.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"cc-remote/internal/i18n"
)

type Event struct {
	CreatedAt string          `json:"createdAt,omitempty"`
	Kind      string          `json:"kind"`
	Text      string          `json:"text,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	// Parent is the Agent tool call whose subagent produced this event; the
	// payload carries it too (parent_tool_use_id) for the web to nest it.
	Parent string `json:"-"`
}

type Options struct {
	ProfileEnv                                                                             map[string]string
	ConfigOverrides                                                                        map[string]any
	ModelProvider                                                                          string
	Provider                                                                               string
	Executable, SessionID, Cwd, Message, BridgeURL, BridgeToken, HostExecutable, ConfigDir string
	Resume                                                                                 bool
	// Empty values pass nothing, leaving Claude's own settings in charge.
	PermissionMode, Model, Effort string
}

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func commandArgs(o Options, settings, mcp string) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	if o.Resume {
		args = append(args, "--resume", o.SessionID)
	} else {
		args = append(args, "--session-id", o.SessionID)
	}
	args = append(args, "--settings", settings, "--mcp-config", mcp, "--permission-prompt-tool", "mcp__cc_remote__approval")
	return append(args, LaunchFlags(o.PermissionMode, o.Model, o.Effort)...)
}

// LaunchFlags converts cc-remote preferences into Claude CLI flags.
func LaunchFlags(permissionMode, model, effort string) []string {
	var args []string
	if permissionMode != "" {
		args = append(args, "--permission-mode", permissionMode)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	return args
}

// Run executes a single turn. Browser lifetime must not be used as ctx lifetime.
// Cancellation stops the process tree; already completed tool side effects remain.
func Run(ctx context.Context, o Options, emit func(Event) error) error {
	if !uuidPattern.MatchString(o.SessionID) {
		return errors.New(i18n.T("invalid Claude session UUID", "无效的 Claude 会话 UUID"))
	}
	if o.Executable == "" {
		o.Executable = "claude"
	}
	if o.HostExecutable == "" {
		var err error
		o.HostExecutable, err = os.Executable()
		if err != nil {
			return err
		}
	}
	if o.BridgeURL == "" || o.BridgeToken == "" {
		return errors.New("permission bridge is required")
	}
	dir, err := os.MkdirTemp("", "cc-remote-claude-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	settings, mcp, err := writeConfigs(dir, o)
	if err != nil {
		return err
	}
	cmd := exec.Command(o.Executable, commandArgs(o, settings, mcp)...)
	cmd.Dir = o.Cwd
	cmd.Env = runEnvironment(os.Environ(), o)
	cmd.Stdin = strings.NewReader(o.Message)
	configureProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if err := ctx.Err(); err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return i18n.Errorf("start Claude Code: %w", "启动 Claude Code 失败：%w", err)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killProcessTree(cmd)
		case <-done:
		}
	}()
	var parseErr error
	sawResult := false
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		events, terminal, eventErr := parseEvent(line)
		if terminal {
			sawResult = true
		}
		for _, e := range events {
			if err = emit(e); err != nil {
				parseErr = err
				break
			}
		}
		if parseErr != nil {
			killProcessTree(cmd)
			break
		}
		if eventErr != nil {
			parseErr = eventErr
			killProcessTree(cmd)
			break
		}
	}
	if scanner.Err() != nil && parseErr == nil {
		parseErr = scanner.Err()
		killProcessTree(cmd)
	}
	waitErr := cmd.Wait()
	close(done)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if parseErr != nil {
		return parseErr
	}
	if waitErr != nil {
		return i18n.Errorf("Claude Code exited: %w: %s", "Claude Code 已退出：%w：%s", waitErr, strings.TrimSpace(stderr.String()))
	}
	if !sawResult {
		return errors.New(i18n.T("Claude Code exited without a result event", "Claude Code 退出时没有给出结果事件"))
	}
	return nil
}

func runEnvironment(env []string, o Options) []string {
	// The host's access token must not reach Claude or the commands it runs.
	env = slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return name == "CC_REMOTE_TOKEN" || runtime.GOOS == "windows" && strings.EqualFold(name, "CC_REMOTE_TOKEN")
	})
	env = replaceEnv(env, "CC_REMOTE_BRIDGE_URL", o.BridgeURL, runtime.GOOS == "windows")
	env = replaceEnv(env, "CC_REMOTE_BRIDGE_TOKEN", o.BridgeToken, runtime.GOOS == "windows")
	if o.ConfigDir != "" {
		env = replaceEnv(env, "CLAUDE_CONFIG_DIR", o.ConfigDir, runtime.GOOS == "windows")
	}
	env = ProfileEnvironment(env, o.ProfileEnv)
	return env
}

func replaceEnv(env []string, key, value string, caseInsensitive bool) []string {
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if name == key || (caseInsensitive && strings.EqualFold(name, key)) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, key+"="+value)
}

type limitedBuffer struct{ data []byte }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := 8192 - len(b.data); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *limitedBuffer) String() string { return string(b.data) }

func writeConfigs(dir string, o Options) (string, string, error) {
	// Exec-form hooks avoid shell interpretation of executable paths on both OSes.
	settings := map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"matcher": "AskUserQuestion|ExitPlanMode", "hooks": []any{map[string]any{"type": "command", "command": o.HostExecutable, "args": []string{"hook"}, "timeout": 1800}}}}}}
	if o.ProfileEnv != nil {
		settings["env"] = SupplierEnv(o.ProfileEnv)
	}
	mcp := map[string]any{"mcpServers": map[string]any{"cc_remote": map[string]any{"type": "stdio", "command": o.HostExecutable, "args": []string{"permission-mcp"}, "env": map[string]string{"CC_REMOTE_BRIDGE_URL": o.BridgeURL, "CC_REMOTE_BRIDGE_TOKEN": o.BridgeToken}}}}
	paths := []string{filepath.Join(dir, "settings.json"), filepath.Join(dir, "mcp.json")}
	for i, v := range []any{settings, mcp} {
		data, err := json.Marshal(v)
		if err != nil {
			return "", "", err
		}
		if err = os.WriteFile(paths[i], data, 0600); err != nil {
			return "", "", err
		}
	}
	return paths[0], paths[1], nil
}

func parseEvent(line []byte) ([]Event, bool, error) {
	var v struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		Event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		} `json:"event"`
		Parent  string            `json:"parent_tool_use_id"`
		IsError bool              `json:"is_error"`
		Result  string            `json:"result"`
		Errors  []string          `json:"errors"`
		Denials []json.RawMessage `json:"permission_denials"`
	}
	if err := json.Unmarshal(line, &v); err != nil {
		return nil, false, fmt.Errorf("invalid Claude stream JSON: %w", err)
	}
	payload := json.RawMessage(append([]byte(nil), line...))
	switch v.Type {
	case "stream_event":
		if v.Event.Delta.Type == "text_delta" && v.Parent == "" { // the main reply only
			return []Event{{Kind: "assistant_delta", Text: v.Event.Delta.Text, Payload: payload}}, false, nil
		}
	case "assistant":
		return withParent(contentEvents(v.Message.Content, "assistant"), v.Parent), false, nil
	case "user":
		events := contentEvents(v.Message.Content, "user")
		filtered := events[:0]
		for _, e := range events {
			if e.Kind == "tool_result" {
				filtered = append(filtered, e)
			}
		}
		return withParent(filtered, v.Parent), false, nil
	case "result":
		e := Event{Kind: "result", Text: v.Result, Payload: payload}
		if v.IsError {
			e.Kind = "error"
			msg := v.Result
			if len(v.Errors) > 0 {
				msg = strings.Join(v.Errors, "; ")
			}
			if msg == "" {
				msg = "Claude Code failed: " + v.Subtype
			}
			e.Text = msg
			return []Event{e}, true, errors.New(msg)
		}
		if len(v.Denials) > 0 {
			return []Event{e, {Kind: "error", Text: i18n.T("Some tool permissions were denied", "部分工具权限被拒绝"), Payload: payload}}, true, errors.New(i18n.T("Claude Code reported permission denials", "Claude Code 报告了权限拒绝"))
		}
		return []Event{e}, true, nil
	case "system":
		kind := "status"
		switch v.Subtype {
		case "init":
			kind = "session"
		case "compact_boundary": // /compact or auto-compaction; payload has compact_metadata
			kind = "compact"
		}
		return []Event{{Kind: kind, Text: v.Subtype, Payload: payload}}, false, nil
	case "error":
		return []Event{{Kind: "error", Text: string(line), Payload: payload}}, false, errors.New(i18n.T("Claude Code emitted an error", "Claude Code 报告了错误"))
	}
	return nil, false, nil
}

// withParent marks the events of a subagent with the Agent call that runs it.
func withParent(events []Event, parent string) []Event {
	if parent == "" {
		return events
	}
	field, _ := json.Marshal(parent)
	for i := range events {
		events[i].Parent = parent
		raw := bytes.TrimSpace(events[i].Payload)
		if len(raw) < 2 || raw[0] != '{' {
			events[i].Payload = json.RawMessage(`{"parent_tool_use_id":` + string(field) + `}`)
			continue
		}
		rest := bytes.TrimSpace(raw[1:])
		sep := ","
		if len(rest) > 0 && rest[0] == '}' {
			sep = ""
		}
		events[i].Payload = json.RawMessage(`{"parent_tool_use_id":` + string(field) + sep + string(rest))
	}
	return events
}

func contentEvents(raw json.RawMessage, role string) []Event {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		if strings.TrimSpace(plain) != "" {
			return []Event{{Kind: role, Text: plain}}
		}
		return nil
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"`
	}
	var originals []json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil || json.Unmarshal(raw, &originals) != nil {
		return nil
	}
	var out []Event
	var text strings.Builder
	for i, b := range blocks {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			out = append(out, Event{Kind: "tool", Payload: originals[i]})
		case "tool_result":
			var t string
			if json.Unmarshal(b.Content, &t) != nil {
				t = string(b.Content)
			}
			out = append(out, Event{Kind: "tool_result", Text: t, Payload: originals[i]})
		}
	}
	if text.Len() > 0 {
		out = append([]Event{{Kind: role, Text: text.String()}}, out...)
	}
	return out
}

// ProfileEnvironment applies only child-scoped supplier settings.
func ProfileEnvironment(env []string, values map[string]string) []string {
	if values == nil {
		return env
	}
	env = slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		k = strings.ToUpper(k)
		return strings.HasPrefix(k, "ANTHROPIC_") || k == "CLAUDE_CODE_OAUTH_TOKEN" || k == "OPENAI_API_KEY"
	})
	for k, v := range values {
		env = replaceEnv(env, k, v, runtime.GOOS == "windows")
	}
	return env
}

// --settings merges env objects with global settings. Explicit empty values
// prevent old auth/model fields from another supplier surviving that merge.
func SupplierEnv(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	env := map[string]string{}
	for _, k := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_SMALL_FAST_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL"} {
		env[k] = ""
	}
	for k, v := range values {
		env[k] = v
	}
	return env
}

// Package claude runs Claude Code's structured CLI without allocating a terminal.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type Event struct {
	Kind    string          `json:"kind"`
	Text    string          `json:"text,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Options struct {
	Executable, SessionID, Cwd, Message, BridgeURL, BridgeToken, HostExecutable, ConfigDir string
	Resume                                                                                 bool
}

var uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func commandArgs(o Options, settings, mcp string) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	if o.Resume {
		args = append(args, "--resume", o.SessionID)
	} else {
		args = append(args, "--session-id", o.SessionID)
	}
	return append(args, "--settings", settings, "--mcp-config", mcp, "--permission-prompt-tool", "mcp__cc_remote__approval")
}

// Run executes a single turn. Browser lifetime must not be used as ctx lifetime.
// Cancellation stops the process tree; already completed tool side effects remain.
func Run(ctx context.Context, o Options, emit func(Event) error) error {
	if !uuidPattern.MatchString(o.SessionID) {
		return errors.New("invalid Claude session UUID")
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
		return fmt.Errorf("start Claude Code: %w", err)
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
		return fmt.Errorf("Claude Code exited: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	if !sawResult {
		return errors.New("Claude Code exited without a result event")
	}
	return nil
}

func runEnvironment(env []string, o Options) []string {
	env = replaceEnv(env, "CC_REMOTE_BRIDGE_URL", o.BridgeURL, runtime.GOOS == "windows")
	env = replaceEnv(env, "CC_REMOTE_BRIDGE_TOKEN", o.BridgeToken, runtime.GOOS == "windows")
	if o.ConfigDir != "" {
		env = replaceEnv(env, "CLAUDE_CONFIG_DIR", o.ConfigDir, runtime.GOOS == "windows")
	}
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
		if v.Event.Delta.Type == "text_delta" {
			return []Event{{Kind: "assistant_delta", Text: v.Event.Delta.Text, Payload: payload}}, false, nil
		}
	case "assistant":
		return contentEvents(v.Message.Content, "assistant"), false, nil
	case "user":
		events := contentEvents(v.Message.Content, "user")
		filtered := events[:0]
		for _, e := range events {
			if e.Kind == "tool_result" {
				filtered = append(filtered, e)
			}
		}
		return filtered, false, nil
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
			return []Event{e, {Kind: "error", Text: "Some tool permissions were denied", Payload: payload}}, true, errors.New("Claude Code reported permission denials")
		}
		return []Event{e}, true, nil
	case "system":
		kind := "status"
		if v.Subtype == "init" {
			kind = "session"
		}
		return []Event{{Kind: kind, Text: v.Subtype, Payload: payload}}, false, nil
	case "error":
		return []Event{{Kind: "error", Text: string(line), Payload: payload}}, false, errors.New("Claude Code emitted an error")
	}
	return nil, false, nil
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

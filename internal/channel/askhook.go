package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"cc-remote/internal/claude"
)

// askConfig is written by the launcher into its per-launch config directory
// (0600/0700 on Unix; Windows does not enforce these modes, the directory only
// inherits the data directory's ACL). The hook finds it through
// CC_REMOTE_ASK_FILE. That variable is inherited by every subprocess of Claude,
// so the model's tools can read the file and its token — the same exposure as the
// MCP config with CC_REMOTE_TOKEN in that directory. It only keeps the token out
// of the environment itself.
type askConfig struct {
	Host        string `json:"host"`
	Token       string `json:"token"`
	SessionID   string `json:"sessionId"`
	BindingFile string `json:"bindingFile"`
}

type webAnswer struct {
	Decision string            `json:"decision"`
	Answers  map[string]string `json:"answers"`
	Mode     string            `json:"mode"`
	Message  string            `json:"message"`
}

// planModes are the permission modes a web approval of ExitPlanMode may switch to.
var planModes = []string{"default", "acceptEdits", "auto", "bypassPermissions"}

const askTimeout = claude.AskWait + time.Minute // after the host, before Claude Code's hook timeout

// AskHook is the PermissionRequest hook for AskUserQuestion and ExitPlanMode in a
// shared terminal. Claude Code keeps its native dialog interactive while the hook
// runs, so the web and the terminal can both answer; the first answer wins.
// The hook fails open: on any problem it prints nothing and the dialog stays.
func AskHook() error {
	if err := askHook(context.Background(), os.Stdin, os.Stdout, os.Getenv("CC_REMOTE_ASK_FILE"), 500*time.Millisecond); err != nil {
		fmt.Fprintf(os.Stderr, "cc-remote: web answer unavailable, answer in the terminal: %v\n", err)
	}
	return nil
}

func askHook(ctx context.Context, input io.Reader, output io.Writer, configPath string, interval time.Duration) error {
	var req struct {
		SessionID      string         `json:"session_id"`
		TranscriptPath string         `json:"transcript_path"`
		ToolName       string         `json:"tool_name"`
		ToolInput      map[string]any `json:"tool_input"`
	}
	if err := json.NewDecoder(io.LimitReader(input, 2<<20)).Decode(&req); err != nil {
		return err
	}
	kind := map[string]string{"AskUserQuestion": "question", "ExitPlanMode": "plan"}[req.ToolName]
	if kind == "" || configPath == "" {
		return nil
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var cfg askConfig
	if err = json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	cfg.Host = strings.TrimRight(cfg.Host, "/")
	if cfg.Token == "" || !launchUUID.MatchString(cfg.SessionID) {
		return errors.New("invalid ask hook configuration")
	}
	if err = validateHost(cfg.Host); err != nil {
		return err
	}
	// After /resume or /clear the terminal shows another conversation; never let
	// the web answer for a session it is not bound to.
	if !strings.EqualFold(req.SessionID, cfg.SessionID) || fileExists(cfg.BindingFile) {
		return nil
	}
	if req.ToolInput == nil {
		req.ToolInput = map[string]any{}
	}
	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	// The hook is not told when the terminal answers; the tool result in the
	// transcript is the signal to give up and withdraw the web prompt.
	go func() {
		if watchTranscript(ctx, req.TranscriptPath, req.ToolName, req.ToolInput, cfg.BindingFile, interval) {
			cancel()
		}
	}()
	a, err := requestAnswer(ctx, cfg, kind, req.ToolName, req.ToolInput)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	decision := askDecision(kind, req.ToolInput, a)
	if decision == nil {
		return nil
	}
	return json.NewEncoder(output).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PermissionRequest", "decision": decision}})
}

func requestAnswer(ctx context.Context, cfg askConfig, kind, toolName string, input map[string]any) (webAnswer, error) {
	var a webAnswer
	body, err := json.Marshal(map[string]any{"kind": kind, "toolName": toolName, "input": input})
	if err != nil {
		return a, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Host+"/api/channels/"+cfg.SessionID+"/interactions", bytes.NewReader(body))
	if err != nil {
		return a, err
	}
	r.Header.Set("Authorization", "Bearer "+cfg.Token)
	r.Header.Set("Content-Type", "application/json")
	client := localClient()
	client.Timeout = 0 // long poll; bounded by ctx
	resp, err := client.Do(r)
	if err != nil {
		return a, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return a, hostError(resp.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&a)
	return a, err
}

// askDecision maps a web answer to a PermissionRequest decision; nil means no
// decision (the terminal keeps the prompt).
func askDecision(kind string, input map[string]any, a webAnswer) map[string]any {
	switch a.Decision {
	case "allow":
		updated := maps.Clone(input)
		if kind == "question" {
			if len(a.Answers) == 0 {
				return nil
			}
			// "allow" alone does not answer AskUserQuestion: the answers travel in updatedInput.
			updated["answers"] = a.Answers
			return map[string]any{"behavior": "allow", "updatedInput": updated}
		}
		mode := a.Mode
		if !slices.Contains(planModes, mode) {
			mode = "default"
		}
		return map[string]any{"behavior": "allow", "updatedInput": updated, "updatedPermissions": []any{map[string]any{"type": "setMode", "mode": mode, "destination": "session"}}}
	case "deny":
		message := strings.TrimSpace(a.Message)
		if message == "" {
			message = "The user declined this in the CC Remote web UI."
			if kind == "plan" {
				message = "The user rejected the plan in the CC Remote web UI. Keep planning and ask what to change."
			}
		}
		return map[string]any{"behavior": "deny", "message": message}
	}
	return nil
}

// watchTranscript reports true once the tool call this hook is waiting for has a
// tool_result (answered or cancelled in the terminal) or the session binding
// changed. It returns false only when ctx ends.
func watchTranscript(ctx context.Context, path, toolName string, input map[string]any, bindingFile string, interval time.Duration) bool {
	var offset int64
	var partial []byte
	current, first := "", true
	results := map[string]bool{}
	want := questionTexts(input)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if fileExists(bindingFile) {
			return true
		}
		if path != "" {
			var ok bool
			offset, partial, ok = readTranscript(path, offset, partial, func(line []byte) {
				if !bytes.Contains(line, []byte(`"tool_`)) {
					return
				}
				var v struct {
					Message struct {
						Content json.RawMessage `json:"content"`
					} `json:"message"`
				}
				var blocks []struct {
					Type      string         `json:"type"`
					ID        string         `json:"id"`
					Name      string         `json:"name"`
					ToolUseID string         `json:"tool_use_id"`
					Input     map[string]any `json:"input"`
				}
				if json.Unmarshal(line, &v) != nil || json.Unmarshal(v.Message.Content, &blocks) != nil {
					return
				}
				for _, b := range blocks {
					// The call being asked is the latest matching tool_use; earlier
					// identical questions already have their own results.
					if b.Type == "tool_use" && b.Name == toolName && (toolName != "AskUserQuestion" || slices.Equal(questionTexts(b.Input), want)) {
						current = b.ID
					} else if b.Type == "tool_result" {
						results[b.ToolUseID] = true
					}
				}
			})
			// Only a successful read counts as the first look: what it finds answered
			// finished before this hook started (an earlier identical call).
			if ok {
				if current != "" && results[current] {
					if !first {
						return true
					}
					current = ""
				}
				first = false
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// transcriptTail bounds the first read: the call being asked was just written, so
// only the end of a long transcript matters.
const transcriptTail = 2 << 20

// readTranscript feeds complete lines appended since offset to fn and reports
// whether the file could be read. The first read (offset 0) covers only the last
// transcriptTail bytes, skipping the cut line; a file that shrank (rewritten) is
// read again like a new one.
func readTranscript(path string, offset int64, partial []byte, fn func([]byte)) (int64, []byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return offset, partial, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return offset, partial, false
	}
	if info.Size() < offset {
		offset, partial = 0, nil
	}
	skip := false
	start := offset
	if offset == 0 && info.Size() > transcriptTail {
		// Start one byte early: if that byte ends a line, only an empty line is skipped.
		start, skip = info.Size()-transcriptTail-1, true
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return offset, partial, false
	}
	data, err := io.ReadAll(io.LimitReader(f, 256<<20))
	if err != nil {
		return offset, partial, false
	}
	offset = start + int64(len(data))
	if skip {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return offset, nil, true
		}
		data = data[i+1:]
	}
	data = append(partial, data...)
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		fn(data[:i])
		data = data[i+1:]
	}
	return offset, append([]byte(nil), data...), true
}

// questionTexts identifies an AskUserQuestion call by its question strings; key
// order and defaulted fields may differ between hook input and transcript.
func questionTexts(input map[string]any) []string {
	list, _ := input["questions"].([]any)
	out := make([]string, 0, len(list))
	for _, q := range list {
		m, _ := q.(map[string]any)
		text, _ := m["question"].(string)
		out = append(out, text)
	}
	return out
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

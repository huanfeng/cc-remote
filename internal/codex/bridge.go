package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func validateBridge(endpoint, token string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") || token == "" {
		return errors.New("invalid local Codex permission bridge configuration")
	}
	return nil
}

type bridgeAnswer struct {
	Decision string            `json:"decision"`
	Answers  map[string]string `json:"answers"`
	Message  string            `json:"message"`
}

func (p *Process) askBridge(ctx context.Context, kind, name string, input any) (bridgeAnswer, error) {
	var answer bridgeAnswer
	b, err := json.Marshal(map[string]any{"kind": kind, "toolName": name, "input": input})
	if err != nil {
		return answer, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.options.BridgeURL, bytes.NewReader(b))
	if err != nil {
		return answer, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.options.BridgeToken)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return answer, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return answer, fmt.Errorf("Codex bridge returned HTTP %d", resp.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&answer)
	if err == nil && answer.Decision != "allow" && answer.Decision != "deny" {
		err = errors.New("invalid permission decision")
	}
	return answer, err
}

func (p *Process) handleRequest(m rpcMessage) error {
	switch m.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/tool/requestUserInput", "item/permissions/requestApproval":
	default:
		return p.rpc.reject(m.ID)
	}
	var params map[string]any
	if err := json.Unmarshal(m.Params, &params); err != nil {
		return p.rpc.write(map[string]any{"id": m.ID, "error": map[string]any{"code": -32602, "message": "Invalid request parameters"}})
	}
	p.mu.Lock()
	thread := p.threadID
	p.mu.Unlock()
	if thread == "" || stringValue(params["threadId"]) != thread {
		return p.rpc.reject(m.ID)
	}
	var permissions json.RawMessage
	if m.Method == "item/permissions/requestApproval" {
		permissions = requestedPermissions(m.Params)
		if permissions == nil {
			return p.rpc.write(map[string]any{"id": m.ID, "result": map[string]any{"permissions": map[string]any{}, "scope": "turn"}})
		}
	}
	ctx, cancel := context.WithCancel(p.rpc.ctx)
	key := string(m.ID)
	p.mu.Lock()
	p.requests[key] = cancel
	p.mu.Unlock()
	go func() {
		defer cancel()
		kind, name := "approval", "Bash"
		input := map[string]any{}
		switch m.Method {
		case "item/commandExecution/requestApproval":
			for _, k := range []string{"command", "cwd", "reason", "networkApprovalContext", "additionalPermissions"} {
				if params[k] != nil {
					input[k] = params[k]
				}
			}
		case "item/fileChange/requestApproval":
			name = "Edit"
			input = params
		case "item/permissions/requestApproval":
			name = "RequestPermissions"
		case "item/tool/requestUserInput":
			kind, name = "question", "AskUserQuestion"
			questions, _ := params["questions"].([]any)
			normalized := make([]any, 0, len(questions))
			for _, q := range questions {
				question, ok := q.(map[string]any)
				if !ok {
					continue
				}
				normalized = append(normalized, map[string]any{"question": question["question"], "header": question["header"], "options": question["options"]})
			}
			input["questions"] = normalized
		}
		var bridgeInput any = input
		if permissions != nil {
			// Preserve the entire request, including metadata and exact numbers.
			bridgeInput = m.Params
		}
		answer, err := p.askBridge(ctx, kind, name, bridgeInput)
		var result any
		if permissions != nil {
			var granted any = map[string]any{}
			if err == nil && answer.Decision == "allow" {
				granted = permissions
			}
			// The single-use bridge cannot create session or persistent grants.
			result = map[string]any{"permissions": granted, "scope": "turn"}
		} else if kind == "question" {
			answers := map[string]any{}
			if err == nil && answer.Decision == "allow" {
				questions, _ := params["questions"].([]any)
				for _, q := range questions {
					question, ok := q.(map[string]any)
					if !ok {
						continue
					}
					id := stringValue(question["id"])
					text, ok := answer.Answers[stringValue(question["question"])]
					if !ok {
						text, ok = answer.Answers[id]
					}
					if ok && strings.TrimSpace(text) != "" {
						answers[id] = map[string]any{"answers": []string{text}}
					}
				}
			}
			result = map[string]any{"answers": answers}
		} else {
			decision := "decline"
			if err == nil && answer.Decision == "allow" {
				decision = "accept"
			}
			// Do not synthesize persistent grants from a single-use bridge allow.
			if allowed, ok := params["availableDecisions"].([]any); ok && decision == "accept" {
				found := false
				for _, v := range allowed {
					if v == "accept" {
						found = true
					}
				}
				if !found {
					decision = "decline"
				}
			}
			result = map[string]any{"decision": decision}
		}
		p.mu.Lock()
		_, pending := p.requests[key]
		delete(p.requests, key)
		p.mu.Unlock()
		if !pending || ctx.Err() != nil {
			return
		}
		if writeErr := p.rpc.write(map[string]any{"id": m.ID, "result": result}); writeErr != nil {
			killProcessTree(p.rpc.cmd)
		}
	}()
	return nil
}

// requestedPermissions validates the schema emitted by codex-cli 0.164.0
// (app-server generate-json-schema --experimental). Fail closed on incomplete
// requests or unfamiliar permission fields instead of echoing opaque grants.
func requestedPermissions(raw json.RawMessage) json.RawMessage {
	var params map[string]json.RawMessage
	if json.Unmarshal(raw, &params) != nil {
		return nil
	}
	for _, key := range []string{"threadId", "turnId", "itemId", "cwd"} {
		var s string
		if json.Unmarshal(params[key], &s) != nil || s == "" {
			return nil
		}
	}
	var timestamp int64
	if len(params["startedAtMs"]) == 0 || bytes.Equal(params["startedAtMs"], []byte("null")) || json.Unmarshal(params["startedAtMs"], &timestamp) != nil {
		return nil
	}
	for _, key := range []string{"environmentId", "reason"} {
		var s *string
		if v, ok := params[key]; ok && json.Unmarshal(v, &s) != nil {
			return nil
		}
	}
	var profile any
	decoder := json.NewDecoder(bytes.NewReader(params["permissions"]))
	decoder.UseNumber()
	if decoder.Decode(&profile) != nil || !validPermissionFields(profile, map[string]func(any) bool{
		"network": func(v any) bool {
			return v == nil || validPermissionFields(v, map[string]func(any) bool{
				"enabled": func(v any) bool { _, ok := v.(bool); return v == nil || ok },
			})
		},
		"fileSystem": func(v any) bool {
			return v == nil || validPermissionFields(v, map[string]func(any) bool{
				"read": validPermissionPaths, "write": validPermissionPaths,
				"globScanMaxDepth": func(v any) bool {
					if v == nil {
						return true
					}
					n, ok := v.(json.Number)
					depth, err := strconv.ParseUint(string(n), 10, strconv.IntSize)
					return ok && err == nil && depth >= 1
				},
				"entries": func(v any) bool {
					if v == nil {
						return true
					}
					entries, ok := v.([]any)
					if !ok {
						return false
					}
					for _, entry := range entries {
						if !validPermissionFields(entry, map[string]func(any) bool{
							"access": func(v any) bool { return v == "read" || v == "write" || v == "deny" },
							"path":   validPermissionPath,
						}) {
							return false
						}
						m := entry.(map[string]any)
						if m["access"] == nil || m["path"] == nil {
							return false
						}
					}
					return true
				},
			})
		},
	}) {
		return nil
	}
	return params["permissions"]
}

// Unknown fields are deliberately rejected even where upstream tolerates them:
// a future schema needs review before its privileges can be granted here.
func validPermissionFields(v any, fields map[string]func(any) bool) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for key, value := range m {
		validate, known := fields[key]
		if !known || !validate(value) {
			return false
		}
	}
	return true
}

func validPermissionString(v any) bool { _, ok := v.(string); return ok }

func validPermissionPaths(v any) bool {
	if v == nil {
		return true
	}
	paths, ok := v.([]any)
	if !ok {
		return false
	}
	for _, path := range paths {
		if !validPermissionString(path) {
			return false
		}
	}
	return true
}

func validPermissionPath(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	fields := map[string]func(any) bool{"type": validPermissionString}
	switch m["type"] {
	case "path":
		fields["path"] = validPermissionString
		if !validPermissionString(m["path"]) {
			return false
		}
	case "glob_pattern":
		fields["pattern"] = validPermissionString
		if !validPermissionString(m["pattern"]) {
			return false
		}
	case "special":
		fields["value"] = validPermissionSpecialPath
		if !validPermissionSpecialPath(m["value"]) {
			return false
		}
	default:
		return false
	}
	return validPermissionFields(v, fields)
}

func validPermissionSpecialPath(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	fields := map[string]func(any) bool{"kind": validPermissionString}
	switch m["kind"] {
	case "root", "minimal", "tmpdir", "slash_tmp":
	case "project_roots", "unknown":
		fields["subpath"] = func(v any) bool { return v == nil || validPermissionString(v) }
		if m["kind"] == "unknown" {
			fields["path"] = validPermissionString
			if !validPermissionString(m["path"]) {
				return false
			}
		}
	default:
		return false
	}
	return validPermissionFields(v, fields)
}

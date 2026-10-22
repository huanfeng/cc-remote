package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type bridgeRequest struct {
	Kind     string         `json:"kind"`
	ToolName string         `json:"toolName"`
	Input    map[string]any `json:"input"`
}
type bridgeResponse struct {
	Decision string            `json:"decision"`
	Answers  map[string]string `json:"answers"`
	// A denial's reason for Claude, such as the user asking to talk a question over first.
	Message string `json:"message"`
}

func requestBridge(ctx context.Context, req bridgeRequest) (bridgeResponse, error) {
	var answer bridgeResponse
	endpoint, token := os.Getenv("CC_REMOTE_BRIDGE_URL"), os.Getenv("CC_REMOTE_BRIDGE_TOKEN")
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") || token == "" {
		return answer, errors.New("invalid local permission bridge configuration")
	}
	data, err := json.Marshal(req)
	if err != nil {
		return answer, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return answer, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	// A loopback bridge must never use proxy environment variables or follow redirects.
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Do(r)
	if err != nil {
		return answer, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return answer, fmt.Errorf("permission bridge returned HTTP %d", resp.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&answer); err != nil {
		return answer, err
	}
	if answer.Decision != "allow" && answer.Decision != "deny" {
		return answer, errors.New("invalid permission decision")
	}
	return answer, nil
}

func RunHook() error { return runHook(os.Stdin, os.Stdout) }
func runHook(input io.Reader, output io.Writer) error {
	var req struct {
		ToolName string         `json:"tool_name"`
		Input    map[string]any `json:"tool_input"`
	}
	if err := json.NewDecoder(io.LimitReader(input, 2*1024*1024)).Decode(&req); err != nil {
		return err
	}
	kind := ""
	switch req.ToolName {
	case "AskUserQuestion":
		kind = "question"
	case "ExitPlanMode":
		kind = "plan"
	default:
		return nil
	}
	if req.Input == nil {
		req.Input = map[string]any{}
	}
	answer, err := requestBridge(context.Background(), bridgeRequest{Kind: kind, ToolName: req.ToolName, Input: req.Input})
	decision, reason := "deny", "Rejected by remote user"
	if err != nil {
		reason = "Remote interaction unavailable: " + err.Error()
	} else {
		decision = answer.Decision
		if decision == "deny" && strings.TrimSpace(answer.Message) != "" {
			reason = answer.Message
		}
	}
	out := map[string]any{"hookEventName": "PreToolUse", "permissionDecision": decision, "permissionDecisionReason": reason}
	if decision == "allow" {
		if kind == "question" {
			if len(answer.Answers) == 0 {
				out["permissionDecision"] = "deny"
				out["permissionDecisionReason"] = "No answers received"
			} else {
				req.Input["answers"] = answer.Answers
				out["updatedInput"] = req.Input
			}
		} else {
			out["updatedInput"] = req.Input
		}
	}
	// Fail closed in valid hook JSON, rather than exiting nonzero (which older CC ignores).
	return json.NewEncoder(output).Encode(map[string]any{"hookSpecificOutput": out})
}

func RunPermissionMCP() error { return runPermissionMCP(os.Stdin, os.Stdout) }

func runPermissionMCP(input io.Reader, output io.Writer) error {
	s := bufio.NewScanner(input)
	s.Buffer(make([]byte, 64*1024), 2*1024*1024)
	enc := json.NewEncoder(output)
	for s.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(s.Bytes(), &req); err != nil {
			return err
		}
		if len(req.ID) == 0 {
			continue
		}
		var result any
		var rpcErr any
		switch req.Method {
		case "initialize":
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &params)
			version := params.ProtocolVersion
			if version == "" {
				version = "2024-11-05"
			}
			result = map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "cc-remote-permissions", "version": "0.1.0"}}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "approval", "description": "Ask the remote human to approve this tool invocation.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"tool_name": map[string]any{"type": "string"}, "input": map[string]any{"type": "object"}, "tool_use_id": map[string]any{"type": "string"}}, "required": []string{"tool_name", "input"}}}}}
		case "tools/call":
			var params struct {
				Name      string `json:"name"`
				Arguments struct {
					ToolName string         `json:"tool_name"`
					Input    map[string]any `json:"input"`
				} `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil || params.Name != "approval" || params.Arguments.ToolName == "" {
				rpcErr = map[string]any{"code": -32602, "message": "Invalid approval tool arguments"}
				break
			}
			if params.Arguments.Input == nil {
				params.Arguments.Input = map[string]any{}
			}
			answer, err := requestBridge(context.Background(), bridgeRequest{Kind: "approval", ToolName: params.Arguments.ToolName, Input: params.Arguments.Input})
			value := map[string]any{"behavior": "deny", "message": "Rejected by remote user"}
			if err != nil {
				value["message"] = "Remote approval unavailable: " + err.Error()
			} else if answer.Decision == "allow" {
				value = map[string]any{"behavior": "allow", "updatedInput": params.Arguments.Input}
			}
			text, _ := json.Marshal(value)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(text)}}}
		default:
			rpcErr = map[string]any{"code": -32601, "message": "Method not found"}
		}
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			response["error"] = rpcErr
		} else {
			response["result"] = result
		}
		if err := enc.Encode(response); err != nil {
			return err
		}
	}
	return s.Err()
}

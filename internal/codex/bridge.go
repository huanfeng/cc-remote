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
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/tool/requestUserInput":
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
		answer, err := p.askBridge(ctx, kind, name, input)
		var result any
		if kind == "question" {
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

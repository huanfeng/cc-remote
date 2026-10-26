package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"cc-remote/internal/claude"
)

// Process owns one app-server connection and serializes user turns in Send order.
type Process struct {
	rpc              *rpcClient
	options          claude.Options
	hooks            claude.ProcessHooks
	callbackMu       sync.Mutex
	mu               sync.Mutex
	threadID         string
	turnID           string
	interruptPending bool
	compacting       bool
	turnDone         chan struct{}
	queue            chan string
	queued           int
	tasks            map[string]bool
	requests         map[string]context.CancelFunc
}

// StartProcess ignores Message and the legacy SessionID placeholder for new threads.
func StartProcess(o claude.Options, h claude.ProcessHooks) (*Process, error) {
	if o.PermissionMode != "" && o.PermissionMode != "read-only" && o.PermissionMode != "workspace-write" && o.PermissionMode != "danger-full-access" {
		return nil, fmt.Errorf("unsupported Codex permission mode %q", o.PermissionMode)
	}
	if o.Resume && o.SessionID == "" {
		return nil, errors.New("Codex resume requires a thread ID")
	}
	if err := validateBridge(o.BridgeURL, o.BridgeToken); err != nil {
		return nil, err
	}
	p := &Process{options: o, hooks: h, queue: make(chan string, 256), tasks: map[string]bool{}, requests: map[string]context.CancelFunc{}}
	startupFinished := make(chan struct{})
	ready := false
	defer close(startupFinished)
	r, err := startRPC(o.Executable, o.ConfigDir, o.Cwd, p.handle, func(err error) {
		<-startupFinished
		if !ready {
			return
		}
		p.cancelRequests()
		p.callbackMu.Lock()
		defer p.callbackMu.Unlock()
		if h.Exit != nil {
			h.Exit(err)
		}
	}, o)
	if err != nil {
		return nil, err
	}
	p.rpc = r
	ctx, cancel := context.WithTimeout(r.ctx, startupTimeout)
	defer cancel()
	if err = r.initialize(ctx); err != nil {
		r.close()
		return nil, fmt.Errorf("initialize Codex: %w", err)
	}
	params := map[string]any{}
	if o.ModelProvider != "" {
		params["modelProvider"] = o.ModelProvider
	}
	method := "thread/start"
	if o.Resume {
		method = "thread/resume"
		params["threadId"] = o.SessionID
		params["excludeTurns"] = true
	}
	if o.Cwd != "" {
		params["cwd"] = o.Cwd
	}
	if o.Model != "" {
		params["model"] = o.Model
	}
	// Keep the adapter's approvals on the explicit human bridge.
	params["approvalsReviewer"] = "user"
	params["approvalPolicy"] = "on-request"
	if o.PermissionMode != "" {
		params["sandbox"] = o.PermissionMode
	}
	if o.PermissionMode == "danger-full-access" {
		params["approvalPolicy"] = "never"
	}
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err = r.call(ctx, method, params, &response); err != nil {
		r.close()
		return nil, err
	}
	if response.Thread.ID == "" {
		r.close()
		return nil, errors.New("Codex returned an empty thread ID")
	}
	p.mu.Lock()
	p.threadID = response.Thread.ID
	p.mu.Unlock()
	if err = p.emit("session", "", map[string]any{"session_id": response.Thread.ID, "provider": "codex"}); err != nil {
		r.close()
		return nil, err
	}
	go p.runQueue()
	ready = true
	return p, nil
}

func (p *Process) emit(kind, text string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	p.callbackMu.Lock()
	defer p.callbackMu.Unlock()
	if p.hooks.Event != nil {
		return p.hooks.Event(claude.Event{Kind: kind, Text: text, Payload: b})
	}
	return nil
}
func (p *Process) result(err error) {
	p.callbackMu.Lock()
	defer p.callbackMu.Unlock()
	if p.hooks.Result != nil {
		p.hooks.Result(err, false)
	}
}
func (p *Process) Send(text string) error {
	fields := strings.Fields(text)
	if len(fields) > 1 && fields[0] == "/compact" {
		return errors.New("Codex /compact does not support additional instructions")
	}
	select {
	case <-p.rpc.ctx.Done():
		return errors.New("Codex is not running")
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case p.queue <- text:
		p.queued++
		return nil
	default:
		return errors.New("Codex message queue is full")
	}
}
func (p *Process) runQueue() {
	for {
		var text string
		select {
		case <-p.rpc.ctx.Done():
			return
		case text = <-p.queue:
		}
		select {
		case <-p.rpc.ctx.Done():
			return
		default:
		}
		p.mu.Lock()
		done := make(chan struct{})
		p.turnDone = done
		p.turnID = ""
		p.compacting = strings.TrimSpace(text) == "/compact"
		p.queued--
		threadID := p.threadID
		p.mu.Unlock()
		p.callbackMu.Lock()
		if p.hooks.Replay != nil {
			p.hooks.Replay(text)
		}
		p.callbackMu.Unlock()
		method := "turn/start"
		params := map[string]any{"threadId": threadID, "input": turnInput(text)}
		if p.options.Effort != "" {
			params["effort"] = p.options.Effort
		}
		if strings.TrimSpace(text) == "/compact" {
			method = "thread/compact/start"
			params = map[string]any{"threadId": threadID}
		}
		ctx, cancel := context.WithTimeout(p.rpc.ctx, startupTimeout)
		var response struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		err := p.rpc.call(ctx, method, params, &response)
		cancel()
		if err != nil {
			p.mu.Lock()
			if p.turnDone == done {
				p.turnDone = nil
			}
			p.mu.Unlock()
			p.result(err)
			// A timed-out start may have begun execution. Never send another turn
			// on an ambiguous connection and accidentally overlap queued work.
			var rpcErr *rpcError
			if !errors.As(err, &rpcErr) {
				killProcessTree(p.rpc.cmd)
				return
			}
			continue
		}
		p.mu.Lock()
		if p.turnDone == done && p.turnID == "" {
			p.turnID = response.Turn.ID
		}
		interrupt := p.interruptPending && p.turnID != ""
		interruptTurn := p.turnID
		if interrupt {
			p.interruptPending = false
		}
		p.mu.Unlock()
		if interrupt {
			if err := p.interruptTurn(threadID, interruptTurn); err != nil {
				killProcessTree(p.rpc.cmd)
				return
			}
		}
		select {
		case <-p.rpc.ctx.Done():
			return
		case <-done:
		}
	}
}

func (p *Process) Interrupt() error {
	p.mu.Lock()
	thread, turn := p.threadID, p.turnID
	if turn == "" && (p.turnDone != nil || p.queued > 0) {
		p.interruptPending = true
	}
	if turn != "" {
		p.interruptPending = false
	}
	p.mu.Unlock()
	p.cancelRequests()
	if turn == "" {
		return nil
	}
	return p.interruptTurn(thread, turn)
}

func (p *Process) interruptTurn(thread, turn string) error {
	ctx, cancel := context.WithTimeout(p.rpc.ctx, startupTimeout)
	defer cancel()
	return p.rpc.call(ctx, "turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}, nil)
}
func (p *Process) BackgroundTasks() int   { p.mu.Lock(); defer p.mu.Unlock(); return len(p.tasks) }
func (p *Process) HasTask(id string) bool { p.mu.Lock(); defer p.mu.Unlock(); return p.tasks[id] }
func (p *Process) Done() <-chan struct{}  { return p.rpc.done }
func (p *Process) Close()                 { p.cancelRequests(); p.rpc.close() }
func (p *Process) cancelRequests() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, cancel := range p.requests {
		cancel()
		delete(p.requests, k)
	}
}

func (p *Process) handle(m rpcMessage) error {
	if len(m.ID) > 0 {
		return p.handleRequest(m)
	}
	var v struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		ItemID    string          `json:"itemId"`
		Delta     string          `json:"delta"`
		RequestID json.RawMessage `json:"requestId"`
		Item      map[string]any  `json:"item"`
		Turn      struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(m.Params, &v); err != nil {
		return err
	}
	p.mu.Lock()
	thread := p.threadID
	p.mu.Unlock()
	if thread != "" && v.ThreadID != "" && v.ThreadID != thread {
		return nil
	}
	switch m.Method {
	case "turn/started":
		p.mu.Lock()
		p.turnID = v.Turn.ID
		interrupt := p.interruptPending
		if interrupt {
			p.interruptPending = false
		}
		p.mu.Unlock()
		if interrupt {
			go func() {
				if err := p.interruptTurn(v.ThreadID, v.Turn.ID); err != nil {
					p.rpc.kill()
				}
			}()
		}
	case "turn/completed":
		p.mu.Lock()
		if p.turnID != "" && p.turnID != v.Turn.ID {
			p.mu.Unlock()
			return nil
		}
		done := p.turnDone
		p.turnDone = nil
		p.turnID = ""
		p.interruptPending = false
		p.mu.Unlock()
		p.cancelRequests()
		var err error
		if v.Turn.Status != "completed" {
			message := "Codex turn " + v.Turn.Status
			if v.Turn.Error != nil {
				message += ": " + v.Turn.Error.Message
			}
			err = errors.New(message)
		}
		if emitErr := p.emit("result", "", map[string]any{"is_error": err != nil, "status": v.Turn.Status}); emitErr != nil {
			return emitErr
		}
		p.result(err)
		if done != nil {
			close(done)
		}
	case "item/agentMessage/delta":
		return p.emit("assistant_delta", v.Delta, nil)
	case "item/started", "item/completed":
		return p.itemEvent(v.Item, m.Method == "item/completed")
	case "item/commandExecution/outputDelta", "item/fileChange/outputDelta":
		return p.emit("status", v.Delta, map[string]any{"subtype": m.Method, "item_id": v.ItemID, "delta": v.Delta})
	case "item/mcpToolCall/progress", "thread/tokenUsage/updated", "turn/diff/updated", "turn/plan/updated":
		return p.emit("status", m.Method, m.Params)
	case "serverRequest/resolved":
		p.mu.Lock()
		if cancel := p.requests[string(v.RequestID)]; cancel != nil {
			cancel()
			delete(p.requests, string(v.RequestID))
		}
		p.mu.Unlock()
	case "error":
		return p.emit("error", "Codex reported an error", m.Params)
	}
	return nil
}

func stringValue(v any) string { s, _ := v.(string); return s }
func (p *Process) itemEvent(item map[string]any, completed bool) error {
	id, kind := stringValue(item["id"]), stringValue(item["type"])
	if kind == "agentMessage" && completed {
		// Match Claude's streaming contract: deltas are followed by a complete
		// assistant message, which the consumer uses to finalize its live bubble.
		return p.emit("assistant", stringValue(item["text"]), item)
	}
	if kind == "collabAgentToolCall" {
		if states, ok := item["agentsStates"].(map[string]any); ok {
			for agent, value := range states {
				state, _ := value.(map[string]any)
				status := stringValue(state["status"])
				running := status == "running" || status == "pendingInit"
				p.mu.Lock()
				was := p.tasks[agent]
				if running {
					p.tasks[agent] = true
				} else {
					delete(p.tasks, agent)
				}
				p.mu.Unlock()
				if running && !was {
					if err := p.emit("status", "task_started", map[string]any{"subtype": "task_started", "task_id": agent, "tool_use_id": id, "is_backgrounded": true}); err != nil {
						return err
					}
				}
				if !running && was {
					if err := p.emit("status", "task_notification", map[string]any{"subtype": "task_notification", "task_id": agent, "status": status}); err != nil {
						return err
					}
				}
			}
		}
	}
	name := ""
	input := map[string]any{}
	switch kind {
	case "commandExecution":
		name = "Bash"
		input["command"] = item["command"]
		input["cwd"] = item["cwd"]
	case "fileChange":
		name = "Edit"
		input["changes"] = item["changes"]
	case "mcpToolCall":
		name = "mcp__" + stringValue(item["server"]) + "__" + stringValue(item["tool"])
		input, _ = item["arguments"].(map[string]any)
	case "dynamicToolCall":
		name = stringValue(item["tool"])
		input, _ = item["arguments"].(map[string]any)
	case "collabAgentToolCall":
		name = "Agent"
		input = item
	case "webSearch":
		name = "WebSearch"
		input["query"] = item["query"]
	default:
		return nil
	}
	if !completed {
		return p.emit("tool", "", map[string]any{"id": id, "name": name, "input": input})
	}
	status := stringValue(item["status"])
	failed := status == "failed" || status == "declined"
	var content any
	switch kind {
	case "commandExecution":
		content = item["aggregatedOutput"]
	case "fileChange":
		content = item["changes"]
	case "mcpToolCall":
		content = item["result"]
		if item["error"] != nil {
			content = item["error"]
			failed = true
		}
	case "dynamicToolCall":
		content = item["contentItems"]
		if success, ok := item["success"].(bool); ok {
			failed = !success
		}
	default:
		content = item
	}
	text, ok := content.(string)
	if !ok {
		b, _ := json.Marshal(content)
		text = string(b)
	}
	return p.emit("tool_result", text, map[string]any{"tool_use_id": id, "content": text, "is_error": failed})
}

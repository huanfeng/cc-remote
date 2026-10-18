package claude

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// A transcript records background work differently from stream-json: a
// launch is a tool result (async_launched, teammate_spawned or a background
// command's backgroundTaskId), its end a <task-notification> turn, and a
// teammate reports with <teammate-message> turns. They become the same
// status events (task_started, task_updated, task_notification) a managed
// session gets, so a terminal session shows its tasks the same way.

// launch is what a background tool call said about its task, or whom a
// SendMessage call wrote to.
type launch struct {
	description, subagentType, name string
	to                              string // SendMessage only
}

// noteLaunch remembers the input of a tool call that may start (or wake) a
// background task, until its result says whether it did.
func noteLaunch(s *NativeSession, raw json.RawMessage) {
	var b struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		Name  string `json:"name"`
		Input struct {
			Description  string `json:"description"`
			SubagentType string `json:"subagent_type"`
			Name         string `json:"name"`
			To           string `json:"to"`
			Background   bool   `json:"run_in_background"`
		} `json:"input"`
	}
	if json.Unmarshal(raw, &b) != nil || b.Type != "tool_use" || b.ID == "" {
		return
	}
	if b.Name == "Agent" || b.Name == "Task" || b.Input.Background || b.Name == "SendMessage" && b.Input.To != "" && b.Input.To != "*" {
		if s.launches == nil {
			s.launches = map[string]launch{}
		}
		s.launches[b.ID] = launch{b.Input.Description, b.Input.SubagentType, b.Input.Name, b.Input.To}
	}
}

// launchEvent is the task_started event of a tool result that launched a
// background task, if it did.
func launchEvent(s *NativeSession, raw json.RawMessage, result json.RawMessage) *Event {
	var b struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
	}
	if json.Unmarshal(raw, &b) != nil || b.Type != "tool_result" || b.ToolUseID == "" {
		return nil
	}
	call, ok := s.launches[b.ToolUseID]
	if !ok {
		return nil
	}
	delete(s.launches, b.ToolUseID)
	var r struct {
		Status       string `json:"status"`
		Success      bool   `json:"success"`
		Resumed      string `json:"resumedAgentId"`
		Routing      any    `json:"routing"`
		AgentID      string `json:"agentId"`
		Description  string `json:"description"`
		Name         string `json:"name"`
		AgentType    string `json:"agent_type"`
		BackgroundID string `json:"backgroundTaskId"`
	}
	if json.Unmarshal(result, &r) != nil {
		return nil
	}
	if call.to != "" {
		// A message wakes a finished background agent (resumed) or lands in a
		// teammate's inbox (routed); one queued for a running agent changes nothing.
		switch {
		case !r.Success:
			return nil
		case r.Resumed != "":
			return statusEvent("task_updated", map[string]any{"subtype": "task_updated", "task_id": r.Resumed, "patch": map[string]any{"status": "running"}})
		case r.Routing != nil:
			return statusEvent("task_updated", map[string]any{"subtype": "task_updated", "task_id": call.to, "patch": map[string]any{"status": "running"}})
		}
		return nil
	}
	started := map[string]any{"subtype": "task_started", "tool_use_id": b.ToolUseID, "is_backgrounded": true, "description": call.description, "subagent_type": call.subagentType}
	switch {
	case r.Status == "async_launched" && r.AgentID != "":
		started["task_id"], started["task_type"] = r.AgentID, "local_agent"
		if r.Description != "" {
			started["description"] = r.Description
		}
	case r.Status == "teammate_spawned" && r.Name != "":
		// A teammate reports by name, so the name is its task id.
		started["task_id"], started["task_type"] = r.Name, "in_process_teammate"
		if r.AgentType != "" {
			started["subagent_type"] = r.AgentType
		}
		if call.description == "" {
			started["description"] = r.Name
		}
	case r.BackgroundID != "":
		started["task_id"], started["task_type"] = r.BackgroundID, "local_bash"
	default:
		return nil
	}
	return statusEvent("task_started", started)
}

func statusEvent(subtype string, v map[string]any) *Event {
	payload, _ := json.Marshal(v)
	return &Event{Kind: "status", Text: subtype, Payload: payload}
}

var (
	notificationTag = regexp.MustCompile(`(?s)<(task-id|tool-use-id|status|summary|result|subagent_tokens|tool_uses|duration_ms)>(.*?)</`)
	teammateBlock   = regexp.MustCompile(`(?s)<teammate-message\s([^>]*)>\n?(.*?)\n?</teammate-message>`)
	attribute       = regexp.MustCompile(`(\w+)="([^"]*)"`)
)

// noticeEvents turns a turn Claude Code adds for background work into events:
// a task notification, or a teammate's messages. ok is false for other text.
func noticeEvents(text string) (events []Event, ok bool) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "<task-notification>") {
		tags := map[string]string{}
		for _, m := range notificationTag.FindAllStringSubmatch(text, -1) {
			tags[m[1]] = html.UnescapeString(strings.TrimSpace(m[2])) // the body is XML-escaped
		}
		// Without a status it is no task's end: a Monitor event, for one.
		if tags["task-id"] == "" || tags["status"] == "" {
			return nil, true
		}
		summary := tags["summary"]
		// The result is the agent's report, unless it says the report came another way.
		if r := tags["result"]; r != "" && !strings.HasPrefix(r, "This agent's report was delivered") {
			summary = r
		}
		v := map[string]any{"subtype": "task_notification", "task_id": tags["task-id"], "tool_use_id": tags["tool-use-id"], "status": tags["status"], "summary": summary}
		if tags["subagent_tokens"] != "" || tags["tool_uses"] != "" {
			v["usage"] = map[string]any{"total_tokens": number(tags["subagent_tokens"]), "tool_uses": number(tags["tool_uses"]), "duration_ms": number(tags["duration_ms"])}
		}
		return []Event{*statusEvent("task_notification", v)}, true
	}
	if !strings.HasPrefix(text, "<teammate-message") && !strings.HasPrefix(text, "Another Claude session sent a message:") {
		return nil, false
	}
	blocks := teammateBlock.FindAllStringSubmatch(text, -1)
	if len(blocks) == 0 {
		return nil, false
	}
	for _, m := range blocks {
		attrs := map[string]string{}
		for _, a := range attribute.FindAllStringSubmatch(m[1], -1) {
			attrs[a[1]] = a[2]
		}
		from, body := attrs["teammate_id"], strings.TrimSpace(m[2])
		var protocol struct {
			Type   string `json:"type"`
			Result string `json:"result"`
		}
		if strings.HasPrefix(body, "{") && json.Unmarshal([]byte(body), &protocol) == nil && protocol.Type != "" {
			// Teammates also send protocol messages; going idle ends a stretch of work.
			if protocol.Type == "idle_notification" && from != "" {
				events = append(events, *statusEvent("task_notification", map[string]any{"subtype": "task_notification", "task_id": from, "status": "idle", "summary": protocol.Result}))
			}
			continue
		}
		payload, _ := json.Marshal(map[string]string{"teammate_id": from, "color": attrs["color"], "summary": attrs["summary"]})
		events = append(events, Event{Kind: "teammate", Text: body, Payload: payload})
	}
	return events, true
}

func number(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

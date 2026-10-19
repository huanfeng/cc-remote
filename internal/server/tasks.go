package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"cc-remote/internal/store"
)

// Subagents and other background tasks report through Claude's system
// events (task_started, task_progress, task_updated, task_notification). The
// task list folds them per task so the web can show progress on the Agent
// call that started each task and a panel of all tasks, whatever page of the
// transcript it has loaded.

type taskInfo struct {
	ID           string `json:"id"`
	ToolUseID    string `json:"toolUseId,omitempty"`
	Description  string `json:"description"`
	SubagentType string `json:"subagentType,omitempty"`
	TaskType     string `json:"taskType,omitempty"`
	Background   bool   `json:"background"`
	Status       string `json:"status"` // running, idle (a teammate), completed, failed, stopped, interrupted
	Progress     string `json:"progress,omitempty"`
	LastTool     string `json:"lastTool,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Tokens       int64  `json:"tokens,omitempty"`
	ToolUses     int64  `json:"toolUses,omitempty"`
	DurationMs   int64  `json:"durationMs,omitempty"`
	StartedAt    string `json:"startedAt"`
	EndedAt      string `json:"endedAt,omitempty"`
	lastAt       string // when its latest event was recorded
}

type taskEvent struct {
	TaskID       string `json:"task_id"`
	ToolUseID    string `json:"tool_use_id"`
	Description  string `json:"description"`
	SubagentType string `json:"subagent_type"`
	TaskType     string `json:"task_type"`
	Background   bool   `json:"is_backgrounded"`
	Status       string `json:"status"`
	Summary      string `json:"summary"`
	LastTool     string `json:"last_tool_name"`
	Usage        *struct {
		TotalTokens int64 `json:"total_tokens"`
		ToolUses    int64 `json:"tool_uses"`
		DurationMs  int64 `json:"duration_ms"`
	} `json:"usage"`
	Patch struct {
		Status  string `json:"status"`
		EndTime int64  `json:"end_time"`
	} `json:"patch"`
}

var taskSubtypes = []string{"task_started", "task_progress", "task_updated", "task_notification"}

func (s *Server) tasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, e := s.store.Get(id); e != nil {
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	out, err := s.sessionTasks(id)
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not read tasks", "无法读取任务"))
		return
	}
	respond(w, 200, out)
}

// fillActiveTasks counts the running background tasks of the sessions whose
// Claude still runs: a task outlives the turn that started it, so a session
// can look finished while its subagents work on. Takes s.mu.
func (s *Server) fillActiveTasks(xs []store.Session) {
	s.mu.Lock()
	live := make([]bool, len(xs))
	for i, x := range xs {
		rt := s.procs[x.ID]
		live[i] = s.terminals[x.ID] != nil || rt != nil && rt.proc != nil
	}
	s.mu.Unlock()
	for i := range xs {
		if !live[i] {
			continue
		}
		tasks, err := s.sessionTasks(xs[i].ID)
		if err != nil {
			continue
		}
		for _, t := range tasks {
			if t.Status == "running" {
				xs[i].ActiveTasks++
			}
		}
	}
}

// sessionTasks folds a session's task events into its tasks. Takes s.mu.
func (s *Server) sessionTasks(id string) ([]taskInfo, error) {
	events, err := s.store.TaskEvents(id)
	if err != nil {
		return nil, err
	}
	byID := map[string]*taskInfo{}
	var order []*taskInfo
	for _, e := range events {
		if !slices.Contains(taskSubtypes, e.Text) {
			continue
		}
		var v taskEvent
		if json.Unmarshal(e.Payload, &v) != nil || v.TaskID == "" {
			continue
		}
		t := byID[v.TaskID]
		if t == nil && v.ToolUseID != "" {
			// A notification may name the task differently from its start
			// (a teammate starts by name); the Agent call ties them.
			for _, other := range order {
				if other.ToolUseID == v.ToolUseID {
					t = other
					break
				}
			}
		}
		if t == nil && e.Text == "task_updated" {
			continue // an update of a task started before the loaded transcript
		}
		if t == nil {
			t = &taskInfo{ID: v.TaskID, Status: "running", StartedAt: e.CreatedAt}
			byID[v.TaskID] = t
			order = append(order, t)
		}
		if t.ToolUseID == "" {
			// The first call is the one that started it; a resumed agent's
			// later notification names the SendMessage call instead.
			t.ToolUseID = v.ToolUseID
		}
		t.lastAt = e.CreatedAt
		if v.Usage != nil {
			t.Tokens, t.ToolUses, t.DurationMs = v.Usage.TotalTokens, v.Usage.ToolUses, v.Usage.DurationMs
		}
		switch e.Text {
		case "task_started":
			t.Description, t.SubagentType, t.TaskType, t.Background = v.Description, v.SubagentType, v.TaskType, v.Background
		case "task_progress":
			t.Progress = v.Description
			if v.LastTool != "" {
				t.LastTool = v.LastTool
			}
		case "task_updated":
			if v.Patch.Status != "" {
				t.Status = v.Patch.Status
			}
			if v.Patch.EndTime > 0 {
				t.EndedAt = time.UnixMilli(v.Patch.EndTime).UTC().Format(time.RFC3339Nano)
			}
		case "task_notification":
			if v.Status != "" {
				t.Status = v.Status
			}
			t.Summary = v.Summary
			if t.EndedAt == "" {
				t.EndedAt = e.CreatedAt
			}
		}
	}
	// A task still "running" that the session's current Claude process does
	// not track (the process that started it exited) will never report again.
	// A terminal session's tasks live as long as the Claude Code that started
	// them; its idle teammates are done once that is gone.
	s.mu.Lock()
	var proc claudeProcess
	if rt := s.procs[id]; rt != nil {
		proc = rt.proc
	}
	var terminalStart time.Time
	if ts := s.terminals[id]; ts != nil {
		terminalStart = ts.started
	}
	s.mu.Unlock()
	out := make([]taskInfo, 0, len(order))
	for _, t := range order {
		last, _ := time.Parse(time.RFC3339Nano, t.lastAt)
		switch {
		case !terminalStart.IsZero() && !last.Before(terminalStart):
		case t.Status == "running" && (proc == nil || !proc.HasTask(t.ID)):
			t.Status = "interrupted"
		case t.Status == "idle":
			t.Status = "completed"
		}
		if t.Status != "running" {
			t.Progress = ""
		}
		out = append(out, *t)
	}
	return out, nil
}

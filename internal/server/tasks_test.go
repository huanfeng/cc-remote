package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

// Task events fold into one entry per task; a task left running by a Claude
// process that is gone reports as interrupted.
func TestTasksFoldClaudeTaskEvents(t *testing.T) {
	_, ts, st := setup(t, noRun)
	x := createSession(t, ts)
	add := func(subtype, payload string) {
		t.Helper()
		if _, err := st.Append(x.ID, "status", subtype, json.RawMessage(payload)); err != nil {
			t.Fatal(err)
		}
	}
	add("task_started", `{"type":"system","subtype":"task_started","task_id":"a1","tool_use_id":"toolu_1","description":"Count files","subagent_type":"general-purpose","is_backgrounded":false,"task_type":"local_agent"}`)
	add("task_progress", `{"type":"system","subtype":"task_progress","task_id":"a1","tool_use_id":"toolu_1","description":"Running ls","usage":{"total_tokens":100,"tool_uses":1,"duration_ms":500},"last_tool_name":"Bash"}`)
	add("task_started", `{"type":"system","subtype":"task_started","task_id":"b2","tool_use_id":"toolu_2","description":"Watch logs","is_backgrounded":true,"task_type":"local_agent"}`)
	add("task_progress", `{"type":"system","subtype":"task_progress","task_id":"b2","description":"Reading","usage":{"total_tokens":50,"tool_uses":2,"duration_ms":900},"last_tool_name":"Read"}`)
	add("task_updated", `{"type":"system","subtype":"task_updated","task_id":"a1","patch":{"status":"completed","end_time":1792121125374}}`)
	add("task_notification", `{"type":"system","subtype":"task_notification","task_id":"a1","tool_use_id":"toolu_1","status":"completed","summary":"2","usage":{"total_tokens":420,"tool_uses":1,"duration_ms":1000}}`)
	add("status", `{"type":"system","subtype":"status","status":"requesting"}`)
	code, b := request(t, ts, "GET", "/api/sessions/"+x.ID+"/tasks", nil, testToken)
	var tasks []taskInfo
	if code != 200 || json.Unmarshal(b, &tasks) != nil || len(tasks) != 2 {
		t.Fatalf("%d %s", code, b)
	}
	a, bg := tasks[0], tasks[1]
	if a.ID != "a1" || a.ToolUseID != "toolu_1" || a.Status != "completed" || a.Summary != "2" || a.Tokens != 420 || a.Progress != "" || a.EndedAt == "" || a.Background {
		t.Fatalf("finished task %+v", a)
	}
	if bg.ID != "b2" || !bg.Background || bg.LastTool != "Read" || bg.ToolUses != 2 || bg.Status != "interrupted" {
		t.Fatalf("orphaned task %+v", bg)
	}
}

// A terminal session's tasks come from its transcript. The first sync after an
// upgrade does not append the old history's task events, and tasks an earlier
// Claude Code left running end once the terminal runs a new one.
func TestTerminalTranscriptTasks(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	x, _ := connectTerminal(t, s, st, id, t.TempDir())
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "tasks-test")
	_ = os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, id+".jsonl")
	launch := func(n string) {
		appendTranscript(t, path, map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "toolu_" + n, "name": "Agent", "input": map[string]any{"description": "Task " + n, "run_in_background": true}}}}})
		appendTranscript(t, path, map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_" + n, "content": "launched"}}}, "toolUseResult": map[string]any{"status": "async_launched", "agentId": "a" + n}})
	}
	tasks := func() map[string]string {
		t.Helper()
		code, b := request(t, ts, "GET", "/api/sessions/"+x.ID+"/tasks", nil, testToken)
		var list []taskInfo
		if code != 200 || json.Unmarshal(b, &list) != nil {
			t.Fatalf("%d %s", code, b)
		}
		out := map[string]string{}
		for _, task := range list {
			out[task.ID] = task.Status
		}
		return out
	}
	launch("1")
	_ = st.MarkTranscript(x.ID, store.LegacyTasksKey) // synced by an earlier host
	syncConnection(t, s, id)
	if got := tasks(); len(got) != 0 {
		t.Fatalf("old history's tasks appended: %v", got)
	}
	launch("2")
	syncConnection(t, s, id)
	s.mu.Lock()
	s.terminals[x.ID] = &terminalSession{id: x.ID, started: time.Now().Add(-time.Hour)}
	s.mu.Unlock()
	if got := tasks(); len(got) != 1 || got["a2"] != "running" {
		t.Fatalf("tasks %v", got)
	}
	s.mu.Lock()
	s.terminals[x.ID].started = time.Now().Add(time.Hour) // a new Claude Code since
	s.mu.Unlock()
	if got := tasks(); got["a2"] != "interrupted" {
		t.Fatalf("task of an earlier Claude Code: %v", got)
	}
	s.mu.Lock()
	delete(s.terminals, x.ID)
	s.mu.Unlock()
}

// The session list counts the background tasks still running, so a session
// whose turn ended does not look finished while its subagents work on; a
// session without a running Claude has none.
func TestSessionListCountsActiveTasks(t *testing.T) {
	s, base, st, _ := setupTerminal(t)
	x := createTerminal(t, base)
	idle := createTerminal(t, base)
	s.mu.Lock()
	y, _ := s.store.Get(x.ID)
	_, err := s.terminalLocked(y)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	add := func(id, subtype, payload string) {
		t.Helper()
		if _, err := st.Append(id, "status", subtype, json.RawMessage(payload)); err != nil {
			t.Fatal(err)
		}
	}
	add(x.ID, "task_started", `{"subtype":"task_started","task_id":"a1","tool_use_id":"toolu_1","description":"Review","is_backgrounded":true,"task_type":"local_agent"}`)
	add(x.ID, "task_started", `{"subtype":"task_started","task_id":"a2","tool_use_id":"toolu_2","description":"Test","is_backgrounded":true,"task_type":"local_agent"}`)
	add(idle.ID, "task_started", `{"subtype":"task_started","task_id":"b1","tool_use_id":"toolu_3","description":"Orphan","is_backgrounded":true,"task_type":"local_agent"}`)
	_ = st.SetStatus(x.ID, "completed")
	active := func() map[string]int {
		_, b := call(t, base, "GET", "/api/sessions", nil)
		var list []struct {
			ID          string `json:"id"`
			ActiveTasks int    `json:"activeTasks"`
		}
		if err := json.Unmarshal(b, &list); err != nil {
			t.Fatal(err)
		}
		out := map[string]int{}
		for _, item := range list {
			out[item.ID] = item.ActiveTasks
		}
		return out
	}
	if got := active(); got[x.ID] != 2 || got[idle.ID] != 0 {
		t.Fatalf("active tasks %v", got)
	}
	// A restart for an update waits for them, though the turn has completed.
	if busy := s.busySessions(); len(busy) != 1 || busy[0] != x.Title {
		t.Fatalf("busy %v", busy)
	}
	add(x.ID, "task_notification", `{"subtype":"task_notification","task_id":"a1","tool_use_id":"toolu_1","status":"completed","summary":"done"}`)
	if got := active(); got[x.ID] != 1 {
		t.Fatalf("after one finished %v", got)
	}
	_, b := call(t, base, "GET", "/api/sessions/"+x.ID, nil)
	if !strings.Contains(string(b), `"activeTasks":1`) {
		t.Fatalf("session: %s", b)
	}
	add(x.ID, "task_notification", `{"subtype":"task_notification","task_id":"a2","tool_use_id":"toolu_2","status":"completed","summary":"done"}`)
	if busy := s.busySessions(); len(busy) != 0 {
		t.Fatalf("busy after the tasks finished %v", busy)
	}
}

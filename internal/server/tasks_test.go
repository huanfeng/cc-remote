package server

import (
	"encoding/json"
	"testing"
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

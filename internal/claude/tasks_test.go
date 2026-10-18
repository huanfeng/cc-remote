package claude

import (
	"encoding/json"
	"testing"
)

// Background work in a transcript becomes the task events a managed session gets.
func TestTranscriptTasks(t *testing.T) {
	var session NativeSession
	lines := []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"Agent","input":{"description":"审查 Task 5","subagent_type":"code-reviewer","run_in_background":true}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"Async agent launched"}]},"toolUseResult":{"status":"async_launched","agentId":"a1","description":"审查 Task 5"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu2","name":"Agent","input":{"description":"实现 Task 6","name":"impl-t6","subagent_type":"executor"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu2","content":"Spawned successfully."}]},"toolUseResult":{"status":"teammate_spawned","name":"impl-t6","agent_type":"executor"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu3","name":"Bash","input":{"command":"go test ./...","description":"Run tests","run_in_background":true}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu3","content":"started"}]},"toolUseResult":{"stdout":"","backgroundTaskId":"b1"}}`,
		`{"type":"user","origin":{"kind":"task-notification"},"message":{"content":"<task-notification>\n<task-id>a1</task-id>\n<tool-use-id>tu1</tool-use-id>\n<status>completed</status>\n<summary>Agent \"审查 Task 5\" finished</summary>\n<result>没有问题：a &lt; b。</result>\n<usage><subagent_tokens>1200</subagent_tokens><tool_uses>7</tool_uses><duration_ms>3000</duration_ms></usage>\n</task-notification>"}}`,
		`{"type":"user","message":{"content":"Another Claude session sent a message:\n<teammate-message teammate_id=\"impl-t6\" color=\"blue\" summary=\"进度\">\nTask 6 做了一半。\n</teammate-message>\n\n<teammate-message teammate_id=\"impl-t6\" color=\"blue\">\n{\"type\":\"idle_notification\",\"from\":\"impl-t6\",\"result\":\"做完了\"}\n</teammate-message>"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu4","name":"SendMessage","input":{"to":"impl-t6","message":"继续"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu4","content":"sent"}]},"toolUseResult":{"success":true,"message":"Message sent to impl-t6's inbox","routing":{"target":"@impl-t6"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu5","name":"SendMessage","input":{"to":"a1","message":"再看一遍"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu5","content":"resumed"}]},"toolUseResult":{"success":true,"message":"Resuming agent a1","resumedAgentId":"a1"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu6","name":"SendMessage","input":{"to":"a2","message":"顺便"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu6","content":"queued"}]},"toolUseResult":{"success":true,"message":"Message queued for delivery to a2 at its next tool round."}}`,
		`{"type":"user","origin":{"kind":"task-notification"},"message":{"content":"<task-notification>
<task-id>m1</task-id>
<summary>Monitor event: build</summary>
<event>line</event>
</task-notification>"}}`,
		`{"type":"attachment","attachment":{"type":"queued_command","commandMode":"task-notification","prompt":"<task-notification>\n<task-id>b1</task-id>\n<status>failed</status>\n<summary>Background command \"Run tests\" failed</summary>\n</task-notification>"}}`,
	}
	var got []string
	for _, line := range lines {
		for _, e := range transcriptEvents([]byte(line), &session) {
			if e.Kind == "user" {
				t.Fatalf("a background notice showed as a user turn: %q", e.Text)
			}
			if e.Kind != "status" && e.Kind != "teammate" {
				continue
			}
			var v map[string]any
			if json.Unmarshal(e.Payload, &v) != nil {
				t.Fatalf("payload %s", e.Payload)
			}
			got = append(got, e.Kind+":"+e.Text+":"+str(v["task_id"])+":"+str(v["task_type"])+str(v["status"])+str(v["teammate_id"])+":"+str(v["description"])+str(v["summary"]))
			if e.Kind == "teammate" && e.Text != "Task 6 做了一半。" {
				t.Fatalf("teammate text %q", e.Text)
			}
		}
	}
	want := []string{
		"status:task_started:a1:local_agent:审查 Task 5",
		"status:task_started:impl-t6:in_process_teammate:实现 Task 6",
		"status:task_started:b1:local_bash:Run tests",
		"status:task_notification:a1:completed:没有问题：a < b。",
		"teammate:Task 6 做了一半。::impl-t6:进度",
		"status:task_notification:impl-t6:idle:做完了",
		"status:task_updated:impl-t6::",
		"status:task_updated:a1::",
		"status:task_notification:b1:failed:Background command \"Run tests\" failed",
	}
	if len(got) != len(want) {
		t.Fatalf("events\n%q\nwant\n%q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
	if len(session.launches) != 0 {
		t.Fatalf("launches left: %v", session.launches)
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

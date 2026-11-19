package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/store"
	"cc-remote/internal/terminal"
)

const resumeTestScreen = "Resume this conversation?\nThis conversation has been inactive for 2 hours and is 120k tokens long.\nResuming it will use about 35% of your 5-hour usage limit.\n❯ 1. Resume\n  2. Start a new conversation\n  3. Resume from summary\nClaude writes a summary now, and you continue from it\nEnter to confirm · Esc to resume"

func TestResumeMenuRequestLifecycle(t *testing.T) {
	s, _, st := setup(t, noRun)
	x, _ := st.Create("resume", t.TempDir(), "", false)
	ts := &terminalSession{id: x.ID, conn: &channelConnection{sessionID: x.ID}}
	menu := terminal.ParseResumeMenu(resumeTestScreen)
	if menu == nil {
		t.Fatal("menu not parsed")
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncResumeMenuLocked(ts, menu, now)
	id := ts.resumeID
	if id == "" || s.requests[id] == nil {
		t.Fatal("missing request")
	}
	s.syncResumeMenuLocked(ts, menu, now.Add(time.Second))
	list, _ := st.Pending(x.ID)
	if len(list) != 1 || list[0].ToolName != "ResumeConversation" {
		t.Fatalf("pending %+v", list)
	}
	s.syncResumeMenuLocked(ts, nil, now.Add(2*time.Second))
	s.syncResumeMenuLocked(ts, menu, now.Add(2500*time.Millisecond))
	if ts.resumeID != id {
		t.Fatal("redraw duplicated request")
	}
	s.syncResumeMenuLocked(ts, nil, now.Add(3*time.Second))
	s.syncResumeMenuLocked(ts, nil, now.Add(6*time.Second))
	list, _ = st.Pending(x.ID)
	if len(list) != 0 {
		t.Fatal("local answer did not withdraw request")
	}
}

func TestResumeMenuWebChoice(t *testing.T) {
	for index, option := range []string{"resume", "new_conversation", "compact"} {
		t.Run(option, func(t *testing.T) {
			s, base, st, keys := setupTerminal(t)
			x := createTerminal(t, base)
			call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "/resume-menu-test"})
			var req store.Interaction
			awaitLong(t, func() string { return "waiting for resume choice" }, func() bool {
				list, _ := st.Pending(x.ID)
				if len(list) == 1 {
					req = list[0]
					return true
				}
				return false
			})
			if req.ToolName != "ResumeConversation" {
				t.Fatalf("tool %s", req.ToolName)
			}
			if y := terminalErrorSession(t, base, x.ID); y.Status != "waiting" {
				t.Fatalf("menu status %s", y.Status)
			}
			// A live selection change must not invalidate the card or send the wrong row.
			s.mu.Lock()
			runner := s.terminals[x.ID].runner
			s.mu.Unlock()
			if err := runner.Keys([]byte("\x1b[B")); err != nil {
				t.Fatal(err)
			}
			awaitLong(t, func() string { return "live selection not changed" }, func() bool { m := terminal.ParseResumeMenu(runner.Screen()); return m != nil && m.Selected == 1 })
			code, _ := call(t, base, "POST", "/api/requests/"+req.ID, map[string]any{"decision": "allow", "answers": map[string]string{"option": "invalid"}})
			if code != 400 {
				t.Fatalf("invalid choice %d", code)
			}
			code, body := call(t, base, "POST", "/api/requests/"+req.ID, map[string]any{"decision": "allow", "answers": map[string]string{"option": option}})
			if code != 200 {
				t.Fatalf("answer %d %s", code, body)
			}
			awaitLong(t, func() string { return keys("") }, func() bool { return strings.Contains(keys(""), "RESUME:"+string(rune('1'+index))) })
			code, _ = call(t, base, "POST", "/api/requests/"+req.ID, map[string]any{"decision": "allow", "answers": map[string]string{"option": option}})
			if code != 409 {
				t.Fatalf("duplicate answer %d", code)
			}
			events, _ := st.Events(x.ID, 0)
			found := false
			for _, e := range events {
				var result struct {
					ToolUseID string `json:"tool_use_id"`
					Content   string `json:"content"`
				}
				_ = json.Unmarshal(e.Payload, &result)
				if result.ToolUseID == req.ID && strings.Contains(result.Content, option) {
					found = true
				}
			}
			if !found {
				t.Fatal("missing recorded choice")
			}
		})
	}
}

func TestResumeMenuLocalAnswerExpiresWebCard(t *testing.T) {
	s, base, st, keys := setupTerminal(t)
	x := createTerminal(t, base)
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "/resume-menu-test"})
	var request store.Interaction
	awaitLong(t, func() string { return "missing resume card" }, func() bool {
		list, _ := st.Pending(x.ID)
		if len(list) == 1 {
			request = list[0]
			return true
		}
		return false
	})
	s.mu.Lock()
	runner := s.terminals[x.ID].runner
	s.mu.Unlock()
	if err := runner.Submit(); err != nil {
		t.Fatal(err)
	}
	awaitLong(t, func() string { return "local selection not processed" }, func() bool { return strings.Contains(keys(""), "RESUME:1") })
	awaitLong(t, func() string { return "local answer not withdrawn" }, func() bool { list, _ := st.Pending(x.ID); return len(list) == 0 })
	code, _ := call(t, base, "POST", "/api/requests/"+request.ID, map[string]any{"decision": "allow", "answers": map[string]string{"option": "new_conversation"}})
	if code != 409 {
		t.Fatalf("stale answer status %d", code)
	}
	if strings.Contains(keys(""), "RESUME:2") {
		t.Fatal("stale choice reached terminal")
	}
}

func TestResumeMenuHoldsQueuedMessages(t *testing.T) {
	_, base, st, keys := setupTerminal(t)
	x := createTerminal(t, base)
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "/resume-menu-test"})
	awaitLong(t, func() string { return "missing resume card" }, func() bool { list, _ := st.Pending(x.ID); return len(list) == 1 })
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "queued while choosing"})
	time.Sleep(1200 * time.Millisecond)
	if strings.Contains(keys(""), "RESUME:") {
		t.Fatal("queued message selected resume action")
	}
	y := terminalErrorSession(t, base, x.ID)
	if y.Status != "waiting" || len(y.Queued) == 0 {
		t.Fatalf("queued message lost: %+v", y)
	}
}

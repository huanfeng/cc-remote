package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/store"
	"cc-remote/internal/terminal"
)

const usageTestScreen = "You've hit your weekly limit · resets Nov 19, 1am (Asia/Shanghai)\n/upgrade or /usage-credits to finish what you’re working on.\n────────\nWhat do you want to do?\n❯ 1. Stop and wait for limit to reset\n  2. Wait here, then continue automatically at Nov 19, 1am\n  3. Switch to usage credits\n  4. Upgrade your plan"

func TestUsageMenuRequestLifecycle(t *testing.T) {
	s, _, st := setup(t, noRun)
	x, err := st.Create("usage", t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	ts := &terminalSession{id: x.ID, conn: &channelConnection{sessionID: x.ID}}
	menu := terminal.ParseUsageLimit(usageTestScreen)
	if menu == nil {
		t.Fatal("menu not parsed")
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncUsageMenuLocked(ts, menu, now)
	id := ts.usageID
	if id == "" || s.requests[id] == nil {
		t.Fatal("missing pending tool")
	}
	s.syncUsageMenuLocked(ts, menu, now.Add(time.Second))
	pending, _ := st.Pending(x.ID)
	if len(pending) != 1 || pending[0].ToolName != "UsageLimit" {
		t.Fatalf("pending %+v", pending)
	}
	s.syncUsageMenuLocked(ts, nil, now.Add(2*time.Second))
	s.syncUsageMenuLocked(ts, menu, now.Add(2500*time.Millisecond))
	if ts.usageID != id {
		t.Fatal("redraw duplicated request")
	}
	s.syncUsageMenuLocked(ts, nil, now.Add(3*time.Second))
	s.syncUsageMenuLocked(ts, nil, now.Add(6*time.Second))
	pending, _ = st.Pending(x.ID)
	if len(pending) != 0 || s.requests[id] != nil {
		t.Fatal("terminal answer did not withdraw tool")
	}
	s.syncUsageMenuLocked(ts, menu, now.Add(7*time.Second))
	if ts.usageID == id {
		t.Fatal("new menu reused expired request")
	}
}

func TestUsageMenuWebChoice(t *testing.T) {
	for _, option := range []string{"1", "2", "3", "4"} {
		t.Run(option, func(t *testing.T) {
			_, base, st, keys := setupTerminal(t)
			x := createTerminal(t, base)
			call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "/usage-limit-test"})
			var req store.Interaction
			awaitLong(t, func() string { return "waiting for UsageLimit tool" }, func() bool {
				list, _ := st.Pending(x.ID)
				if len(list) == 1 {
					req = list[0]
					return true
				}
				return false
			})
			if req.ToolName != "UsageLimit" {
				t.Fatalf("tool %s", req.ToolName)
			}
			if option == "1" {
				call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "queued behind the usage menu"})
				time.Sleep(1200 * time.Millisecond)
				if strings.Contains(keys(""), "USAGE:") {
					t.Fatal("queued message selected a usage action")
				}
			}
			code, _ := call(t, base, "POST", "/api/requests/"+req.ID, map[string]any{"decision": "allow", "answers": map[string]string{"option": "5"}})
			if code != 400 {
				t.Fatalf("invalid option status %d", code)
			}
			code, body := call(t, base, "POST", "/api/requests/"+req.ID, map[string]any{"decision": "allow", "answers": map[string]string{"option": option}})
			if code != 200 {
				t.Fatalf("answer %d %s", code, body)
			}
			awaitLong(t, func() string { return keys("") }, func() bool { return strings.Contains(keys(""), "USAGE:"+option) })
			code, _ = call(t, base, "POST", "/api/requests/"+req.ID, map[string]any{"decision": "allow", "answers": map[string]string{"option": option}})
			if code != 409 {
				t.Fatalf("duplicate status %d", code)
			}
			// A completed tool result retains the chosen action in conversation history.
			events, _ := st.Events(x.ID, 0)
			found := false
			for _, ev := range events {
				var b struct {
					ToolUseID string `json:"tool_use_id"`
				}
				_ = json.Unmarshal(ev.Payload, &b)
				if ev.Kind == "tool_result" && b.ToolUseID == req.ID {
					found = true
				}
			}
			if !found {
				t.Fatal("missing tool result")
			}
		})
	}
}

func TestUsageMenuAnsweredInTerminal(t *testing.T) {
	s, base, st, keys := setupTerminal(t)
	x := createTerminal(t, base)
	call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "/usage-limit-test"})
	var id string
	awaitLong(t, func() string { return "waiting for usage menu" }, func() bool {
		list, _ := st.Pending(x.ID)
		if len(list) == 1 {
			id = list[0].ID
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
	awaitLong(t, func() string { return keys("") }, func() bool { return strings.Contains(keys(""), "USAGE:1") })
	awaitLong(t, func() string { return "waiting for tool withdrawal" }, func() bool { list, _ := st.Pending(x.ID); return len(list) == 0 })
	code, _ := call(t, base, "POST", "/api/requests/"+id, map[string]any{"decision": "allow", "answers": map[string]string{"option": "3"}})
	if code != 409 {
		t.Fatalf("stale card status %d", code)
	}
	if strings.Contains(keys(""), "USAGE:3") {
		t.Fatal("stale choice sent")
	}
}

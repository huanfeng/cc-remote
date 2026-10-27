package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
	"cc-remote/internal/store"
	"cc-remote/internal/terminal"
)

func TestDeferredQueueEditsInPlaceAndPreservesAttachments(t *testing.T) {
	s, ts, st, procs := withFakeProc(t)
	s.cfg.UploadDir = t.TempDir()
	x := createSession(t, ts)
	base := "/api/sessions/" + x.ID
	request(t, ts, "POST", base+"/messages", map[string]any{"text": "start", "defer": true}, testToken)
	p := <-procs
	await(t, func() bool { return len(p.sentCopy()) == 1 })
	p.replay("start")
	c, b := uploadImage(t, ts, x.ID, testToken, tinyPNG(t))
	if c != 201 {
		t.Fatalf("upload %d %s", c, b)
	}
	var a attachment
	_ = json.Unmarshal(b, &a)
	for _, body := range []map[string]any{{"text": "two", "defer": true, "sendId": "queued-0002", "attachmentIds": []string{a.ID}}, {"text": "three", "defer": true, "sendId": "queued-0003"}} {
		if c, b := request(t, ts, "POST", base+"/messages", body, testToken); c != 202 {
			t.Fatalf("enqueue %d %s", c, b)
		}
	}
	if c, b := request(t, ts, "PATCH", base+"/queue/queued-0002", map[string]string{"text": "edited"}, testToken); c != 200 {
		t.Fatalf("edit %d %s", c, b)
	}
	if got := p.sentCopy(); len(got) != 1 {
		t.Fatalf("queued message reached process before result: %v", got)
	}
	s.mu.Lock()
	items := s.procs[x.ID].queuedMessages()
	s.mu.Unlock()
	if len(items) != 2 || items[0].Text != "edited" || !strings.Contains(string(items[0].Payload), a.ID) || !items[0].Editable {
		t.Fatalf("queue %+v", items)
	}
	other := createSession(t, ts)
	if c, _ := request(t, ts, "DELETE", "/api/sessions/"+other.ID+"/queue/queued-0002", nil, testToken); c != 409 {
		t.Fatal("cross-session edit allowed")
	}
	p.result(nil)
	await(t, func() bool { return len(p.sentCopy()) == 2 })
	if !strings.HasPrefix(p.sentCopy()[1], "edited") {
		t.Fatalf("wrong next prompt %q", p.sentCopy()[1])
	}
	if c, _ := request(t, ts, "PATCH", base+"/queue/queued-0002", map[string]string{"text": "too late"}, testToken); c != 409 {
		t.Fatal("edited dispatched message")
	}
	p.replay(p.sentCopy()[1])
	p.result(nil)
	await(t, func() bool { return len(p.sentCopy()) == 3 })
	if p.sentCopy()[2] != "three" {
		t.Fatal("queue order changed")
	}
	p.replay("three")
	p.result(nil)
	if got := strings.Join(userTexts(st, x.ID), ","); got != "start,edited,three" {
		t.Fatalf("history %s", got)
	}
}

func TestDeferredQueueWithdrawReturnsOriginalAndDeleteIsScoped(t *testing.T) {
	s, ts, _, procs := withFakeProc(t)
	x := createSession(t, ts)
	base := "/api/sessions/" + x.ID
	request(t, ts, "POST", base+"/messages", map[string]string{"text": "start"}, testToken)
	p := <-procs
	await(t, func() bool { return len(p.sentCopy()) == 1 })
	p.replay("start")
	for _, id := range []string{"queued-0002", "queued-0003"} {
		request(t, ts, "POST", base+"/messages", map[string]any{"text": id, "defer": true, "sendId": id}, testToken)
	}
	c, b := request(t, ts, "POST", base+"/queue/queued-0002/withdraw", nil, testToken)
	var reply struct {
		Message store.QueuedMessage `json:"message"`
	}
	_ = json.Unmarshal(b, &reply)
	if c != 200 || reply.Message.ID != "queued-0002" || reply.Message.Text != "queued-0002" {
		t.Fatalf("withdraw %d %s", c, b)
	}
	if c, _ := request(t, ts, "DELETE", base+"/queue/queued-0002", nil, testToken); c != 409 {
		t.Fatal("consumed withdrawal twice")
	}
	if c, _ := request(t, ts, "DELETE", base+"/queue/queued-0003", nil, testToken); c != 200 {
		t.Fatal("delete failed")
	}
	s.mu.Lock()
	n := len(s.procs[x.ID].sent)
	s.mu.Unlock()
	if n != 0 {
		t.Fatal("queue not empty")
	}
	p.result(nil)
}

func TestQueuedCodexSteerIsIdempotentAndUnknownCanBeDismissed(t *testing.T) {
	for _, outcome := range []string{"accepted", "rejected", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			s, ts, _ := setup(t, nil)
			var p *steeringProc
			calls := 0
			s.cfg.StartCodexProcess = func(_ claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
				p = &steeringProc{fakeProc: &fakeProc{hooks: h, done: make(chan struct{})}, steer: func(string) error {
					calls++
					if outcome == "rejected" {
						return codex.ErrSteerRejected
					}
					if outcome == "unknown" {
						return errors.New("lost acknowledgement")
					}
					return nil
				}}
				return p, nil
			}
			x := createCodexSession(t, ts)
			base := "/api/sessions/" + x.ID
			request(t, ts, "POST", base+"/messages", map[string]string{"text": "start"}, testToken)
			await(t, func() bool {
				s.mu.Lock()
				defer s.mu.Unlock()
				return s.procs[x.ID] != nil && s.procs[x.ID].proc != nil
			})
			await(t, func() bool { return len(p.sentCopy()) == 1 })
			p.replay("start")
			request(t, ts, "POST", base+"/messages", map[string]any{"text": "more", "defer": true, "sendId": "queued-0002"}, testToken)
			want := 409
			if outcome == "accepted" {
				want = 202
			}
			if c, b := request(t, ts, "POST", base+"/queue/queued-0002/send", nil, testToken); c != want {
				t.Fatalf("send %d %s", c, b)
			}
			if outcome != "rejected" {
				request(t, ts, "POST", base+"/queue/queued-0002/send", nil, testToken)
				if calls != 1 {
					t.Fatal("duplicate steer")
				}
			}
			s.mu.Lock()
			q := s.procs[x.ID].queuedMessages()
			s.mu.Unlock()
			if outcome == "accepted" && len(q) != 0 {
				t.Fatal("accepted queue item remains")
			}
			if outcome == "unknown" {
				if len(q) != 1 || q[0].Delivery != "unknown" || q[0].Editable {
					t.Fatalf("unknown state %+v", q)
				}
				// An uncertain steer is only a warning: new work must continue
				// without requiring the user to dismiss it first.
				request(t, ts, "POST", base+"/messages", map[string]any{"text": "later", "defer": true, "sendId": "queued-0003"}, testToken)
				p.result(nil)
				await(t, func() bool { return len(p.sentCopy()) == 2 })
				if got := p.sentCopy()[1]; got != "later" {
					t.Fatalf("unknown blocked later message: %q", got)
				}
				p.replay("later")
				p.result(nil)
				await(t, func() bool { y, _ := s.store.Get(x.ID); return y.Status == "completed" })
				if c, _ := request(t, ts, "DELETE", base+"/queue/queued-0002", nil, testToken); c != 200 {
					t.Fatal("cannot dismiss uncertain delivery")
				}
			}
			if outcome == "rejected" && (len(q) != 1 || !q[0].Editable) {
				t.Fatal("rejected item not restored")
			}
		})
	}
}

func TestDeferredTerminalQueueEditsAndDrainsAfterTask(t *testing.T) {
	s, base, st, _ := setupTerminal(t)
	x := createTerminal(t, base)
	path := "/api/sessions/" + x.ID
	call(t, base, "POST", path+"/messages", map[string]string{"text": "initial"})
	awaitLong(t, func() string { return strings.Join(eventTexts(st, x.ID), "|") }, func() bool {
		y, _ := st.Get(x.ID)
		return y.Status == "completed" && len(eventTexts(st, x.ID)) == 2
	})
	s.mu.Lock()
	_ = st.SetStatus(x.ID, "running") // native task is working
	s.mu.Unlock()
	for _, id := range []string{"terminal-0002", "terminal-0003"} {
		if c, b := call(t, base, "POST", path+"/messages", map[string]any{"text": id, "sendId": id, "defer": true}); c != 202 {
			t.Fatalf("queue %d %s", c, b)
		}
	}
	if c, b := call(t, base, "PATCH", path+"/queue/terminal-0003", map[string]string{"text": "edited terminal"}); c != 200 {
		t.Fatalf("edit %d %s", c, b)
	}
	if c, b := call(t, base, "DELETE", path+"/queue/terminal-0002", nil); c != 200 {
		t.Fatalf("delete %d %s", c, b)
	}
	s.mu.Lock()
	q := s.terminals[x.ID].queued
	if len(q) != 1 || !q[0].held || !q[0].message.Editable {
		t.Errorf("terminal did not hold editable queue: %+v", q)
	}
	s.mu.Unlock()
	call(t, base, "POST", path+"/stop", nil) // same drain path as the task's Stop hook
	awaitLong(t, func() string { return strings.Join(eventTexts(st, x.ID), "|") }, func() bool {
		return strings.Contains(strings.Join(eventTexts(st, x.ID), "|"), "user:edited terminal")
	})
	if strings.Contains(strings.Join(eventTexts(st, x.ID), "|"), "user:terminal-0002") {
		t.Fatal("deleted message ran")
	}
}

func TestClaudeSendQueuedItemPromotesOnlySelectedMessage(t *testing.T) {
	_, ts, _, procs := withFakeProc(t)
	x := createSession(t, ts)
	base := "/api/sessions/" + x.ID
	request(t, ts, "POST", base+"/messages", map[string]string{"text": "start"}, testToken)
	p := <-procs
	await(t, func() bool { return len(p.sentCopy()) == 1 })
	p.replay("start")
	for _, id := range []string{"queued-0002", "queued-0003", "queued-0004"} {
		request(t, ts, "POST", base+"/messages", map[string]any{"text": id, "defer": true, "sendId": id}, testToken)
	}
	for i := 0; i < 2; i++ {
		if c, b := request(t, ts, "POST", base+"/queue/queued-0003/send", nil, testToken); c != 202 {
			t.Fatalf("immediate %d %s", c, b)
		}
	}
	await(t, func() bool { return len(p.sentCopy()) == 2 })
	if got := p.sentCopy()[1]; got != "queued-0003" {
		t.Fatalf("sent wrong item %q", got)
	}
	p.result(nil)
	p.replay("queued-0003")
	p.result(nil)
	await(t, func() bool { return len(p.sentCopy()) == 3 })
	if got := p.sentCopy()[2]; got != "queued-0002" {
		t.Fatalf("remaining queue reordered: %q", got)
	}
}

func TestTerminalExitPreservesHeldMessageAndAttachment(t *testing.T) {
	for _, closing := range []bool{false, true} {
		t.Run(map[bool]string{false: "process exit", true: "normal shutdown"}[closing], func(t *testing.T) {
			s, ts, st := setup(t, nil)
			x := createSession(t, ts)
			payload := json.RawMessage(`{"attachments":[{"id":"original-image","name":"image.png"}]}`)
			runtime := &terminalSession{id: x.ID, dir: t.TempDir(), closing: closing, runner: &terminal.Runner{}, queued: []terminalQueued{{held: true, message: store.QueuedMessage{ID: "queued-0002", Text: "keep this text", Payload: payload}}}}
			s.terminalExited(runtime)
			events, _ := st.Events(x.ID, 0)
			found, notRun := false, false
			for _, e := range events {
				if e.Kind == "user" && e.Text == "keep this text" && strings.Contains(string(e.Payload), "original-image") {
					found = true
				}
				if e.Kind == "error" && strings.Contains(e.Text, "not executed") {
					notRun = true
				}
			}
			if !found || !notRun {
				t.Fatalf("held message lost: %+v", events)
			}
		})
	}
}

func TestQueuedSteerExitBeforeAcknowledgementRecordsInputOnce(t *testing.T) {
	s, ts, st := setup(t, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var p *steeringProc
	s.cfg.StartCodexProcess = func(_ claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		p = &steeringProc{fakeProc: &fakeProc{hooks: h, done: make(chan struct{})}, steer: func(string) error { close(entered); <-release; return nil }}
		return p, nil
	}
	x := createCodexSession(t, ts)
	base := "/api/sessions/" + x.ID
	request(t, ts, "POST", base+"/messages", map[string]string{"text": "start"}, testToken)
	await(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.procs[x.ID] != nil && s.procs[x.ID].proc != nil
	})
	await(t, func() bool { return len(p.sentCopy()) == 1 })
	p.replay("start")
	request(t, ts, "POST", base+"/messages", map[string]any{"text": "more", "defer": true, "sendId": "queued-0002"}, testToken)
	done := make(chan int, 1)
	go func() { c, _ := request(t, ts, "POST", base+"/queue/queued-0002/send", nil, testToken); done <- c }()
	<-entered
	p.Close() // processExit preserves the pending instruction before the RPC returns
	close(release)
	if c := <-done; c != 202 {
		t.Fatalf("acknowledgement %d", c)
	}
	if got := strings.Join(userTexts(st, x.ID), ","); got != "start,more" {
		t.Fatalf("input recorded twice: %q", got)
	}
	if c, _ := request(t, ts, "POST", base+"/queue/queued-0002/send", nil, testToken); c != 202 {
		t.Fatalf("receipt retry %d", c)
	}
	if got := strings.Join(userTexts(st, x.ID), ","); got != "start,more" {
		t.Fatal("retry recorded input again")
	}
}

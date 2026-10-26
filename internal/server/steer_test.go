package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
)

func TestSteerConfirmationNeverDeliversWhenReceiptIsLost(t *testing.T) {
	for _, state := range []string{"missing", "expired", "evicted", "pending", "accepted", "unknown"} {
		t.Run(state, func(t *testing.T) {
			s, ts, _ := setup(t, nil)
			var p *steeringProc
			calls := 0
			s.cfg.StartCodexProcess = func(_ claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
				p = &steeringProc{fakeProc: &fakeProc{hooks: h, done: make(chan struct{})}, steer: func(string) error { calls++; return nil }}
				return p, nil
			}
			x := createCodexSession(t, ts)
			request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "a later task"}, testToken)
			await(t, func() bool {
				s.mu.Lock()
				defer s.mu.Unlock()
				return s.procs[x.ID] != nil && s.procs[x.ID].proc != nil
			})
			p.replay("a later task")
			const id = "confirm-12345"
			s.mu.Lock()
			s.sent.remember(x.ID, id, "steered")
			switch state {
			case "missing":
				s.sent = sends{} // a host restart also loses this cache
			case "expired":
				s.sent[sendKey(x.ID, id)] = sentMessage{status: "steered", at: time.Now().Add(-2 * sendMemory)}
			case "evicted":
				delete(s.sent, sendKey(x.ID, id))
			case "pending":
				s.sent.remember(x.ID, id, "steering")
			case "unknown":
				s.sent.remember(x.ID, id, "steer-unknown")
			}
			s.mu.Unlock()
			code, b := request(t, ts, "GET", "/api/sessions/"+x.ID+"/steer/"+id, nil, testToken)
			want := 409
			if state == "accepted" {
				want = 202
			}
			if code != want {
				t.Fatalf("confirmation %d %s", code, b)
			}
			if got := strings.Join(userTexts(s.store, x.ID), ","); got != "a later task" {
				t.Fatal("query recorded input")
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if calls != 0 || s.procs[x.ID].proc != p {
				t.Fatal("query delivered into the later task")
			}
		})
	}
}

type steeringProc struct {
	*fakeProc
	steer func(string) error
}

func (p *steeringProc) Steer(text string) error { return p.steer(text) }

func TestCodexSteerRecordsOnceAndDoesNotConsumeQueue(t *testing.T) {
	s, ts, st := setup(t, nil)
	var p *steeringProc
	calls := 0
	s.cfg.StartCodexProcess = func(_ claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		p = &steeringProc{fakeProc: &fakeProc{hooks: h, done: make(chan struct{})}, steer: func(text string) error {
			calls++
			if text != "focus on tests" {
				t.Errorf("steer text: %q", text)
			}
			// Completion can arrive before turn/steer's acknowledgement.
			p.result(nil)
			return nil
		}}
		return p, nil
	}
	x := createCodexSession(t, ts)
	request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "start"}, testToken)
	await(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.procs[x.ID] != nil && s.procs[x.ID].proc != nil
	})
	p.replay("start")
	for i := 0; i < 2; i++ {
		code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/steer", map[string]string{"text": "focus on tests", "sendId": "steer-12345"}, testToken)
		if code != 202 {
			t.Fatalf("steer: %d %s", code, b)
		}
	}
	if calls != 1 {
		t.Fatalf("duplicate delivery: %d", calls)
	}
	if got := strings.Join(userTexts(st, x.ID), ","); got != "start,focus on tests" {
		t.Fatalf("history: %s", got)
	}
	y, _ := st.Get(x.ID)
	if y.Status != "completed" {
		t.Fatalf("completion race: %s", y.Status)
	}
}

func TestCodexSteerRejectionAndUnknownDelivery(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected", true: "uncertain"}[uncertain], func(t *testing.T) {
			s, ts, st := setup(t, nil)
			var p *steeringProc
			calls := 0
			s.cfg.StartCodexProcess = func(_ claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
				p = &steeringProc{fakeProc: &fakeProc{hooks: h, done: make(chan struct{})}, steer: func(string) error {
					calls++
					if uncertain {
						return errors.New("lost acknowledgement")
					}
					return codex.ErrSteerRejected
				}}
				return p, nil
			}
			x := createCodexSession(t, ts)
			request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "start"}, testToken)
			await(t, func() bool {
				s.mu.Lock()
				defer s.mu.Unlock()
				return s.procs[x.ID] != nil && s.procs[x.ID].proc != nil
			})
			p.replay("start")
			for i := 0; i < 2; i++ {
				code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/steer", map[string]string{"text": "extra", "sendId": "steer-12345"}, testToken)
				if code != 409 {
					t.Fatalf("expected conflict: %d %s", code, b)
				}
			}
			want := 2
			if uncertain {
				want = 1
			}
			if calls != want {
				t.Fatalf("calls %d want %d", calls, want)
			}
			users := userTexts(st, x.ID)
			if !uncertain && len(users) != 1 {
				t.Fatalf("rejected input recorded: %v", users)
			}
		})
	}
}

func TestSteerRequiresActiveCodexAndSendID(t *testing.T) {
	_, ts, _ := setup(t, nil)
	for _, x := range []string{createSession(t, ts).ID, createCodexSession(t, ts).ID, "missing"} {
		code, _ := request(t, ts, "POST", "/api/sessions/"+x+"/steer", map[string]string{"text": "extra", "sendId": "steer-12345"}, testToken)
		if code == 202 {
			t.Fatal("accepted without active Codex turn")
		}
	}
	for _, body := range []map[string]string{{"text": "extra"}, {"text": "/compact", "sendId": "steer-12345"}, {"text": " ", "sendId": "steer-12345"}} {
		code, b := request(t, ts, "POST", "/api/sessions/missing/steer", body, testToken)
		if code != 400 {
			t.Fatalf("validation %d %s", code, b)
		}
	}
}

func TestSteerBlocksConcurrentSubmissionsUntilAcknowledged(t *testing.T) {
	s, ts, _ := setup(t, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var p *steeringProc
	s.cfg.StartCodexProcess = func(_ claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		p = &steeringProc{fakeProc: &fakeProc{hooks: h, done: make(chan struct{})}, steer: func(string) error { close(entered); <-release; return nil }}
		return p, nil
	}
	x := createCodexSession(t, ts)
	path := "/api/sessions/" + x.ID
	request(t, ts, "POST", path+"/messages", map[string]string{"text": "start"}, testToken)
	await(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.procs[x.ID] != nil && s.procs[x.ID].proc != nil
	})
	p.replay("start")
	response := make(chan int, 1)
	go func() {
		code, _ := request(t, ts, "POST", path+"/steer", map[string]string{"text": "extra", "sendId": "steer-12345"}, testToken)
		response <- code
	}()
	<-entered
	for _, body := range []map[string]string{{"text": "extra", "sendId": "steer-12345"}, {"text": "extra", "sendId": "steer-67890"}} {
		if code, b := request(t, ts, "POST", path+"/steer", body, testToken); code != 409 {
			t.Fatalf("concurrent steer %d %s", code, b)
		}
	}
	if code, b := request(t, ts, "POST", path+"/messages", map[string]string{"text": "queued"}, testToken); code != 409 {
		t.Fatalf("concurrent queue %d %s", code, b)
	}
	releaseOnce.Do(func() { close(release) })
	if code := <-response; code != 202 {
		t.Fatalf("steer %d", code)
	}
	if code, b := request(t, ts, "POST", path+"/messages", map[string]string{"text": "queued"}, testToken); code != 202 {
		t.Fatalf("queue after ack %d %s", code, b)
	}
}

func TestLateSteerSettlementCannotModifyReplacementExecution(t *testing.T) {
	s, ts, st := setup(t, nil)
	x := createCodexSession(t, ts)
	s.mu.Lock()
	old := &sessionRuntime{id: x.ID, token: "old", ready: make(chan struct{}), outcome: "failed"}
	s.busyLocked(old)
	oldDone := old.run.done
	current := &sessionRuntime{id: x.ID, token: "new", ready: make(chan struct{}), outcome: "completed", turn: true}
	s.procs[x.ID] = current
	s.busyLocked(current)
	currentRun := current.run
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.requests["new-approval"] = &pending{sessionID: x.ID, ctx: ctx, channel: make(chan answer, 1)}
	s.settleLocked(old)
	_, approval := s.requests["new-approval"]
	active := s.runs[x.ID] == currentRun
	// Leave no synthetic process for server cleanup to wait on.
	delete(s.procs, x.ID)
	current.turn = false
	s.settleLocked(current)
	s.mu.Unlock()
	if !approval || !active {
		t.Fatal("old settlement modified replacement execution")
	}
	select {
	case <-oldDone:
	default:
		t.Fatal("old execution not released")
	}
	y, _ := st.Get(x.ID)
	if y.Status != "completed" {
		t.Fatalf("replacement outcome: %s", y.Status)
	}
}

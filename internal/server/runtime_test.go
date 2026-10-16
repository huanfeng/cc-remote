package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

// TestIdleProcessIsReaped: an idle session process exits after the idle
// timeout and the next message starts a new one that resumes the conversation.
func TestIdleProcessIsReaped(t *testing.T) {
	starts := make(chan claude.Options, 4)
	s, ts, st := setup(t, nil)
	s.mu.Lock()
	s.idleTimeout = 50 * time.Millisecond
	s.mu.Unlock()
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go s.reapIdle(stop, 20*time.Millisecond)
	run := func(_ context.Context, o claude.Options, emit func(claude.Event) error) error {
		return emit(claude.Event{Kind: "result", Text: "ok"})
	}
	inner := runnerStarter(run)
	s.startProcess = func(o claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		starts <- o
		return inner(o, h)
	}
	x := createSession(t, ts)
	if code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "one"}, testToken); code != 202 {
		t.Fatalf("send %d %s", code, b)
	}
	await(t, func() bool { y, _ := st.Get(x.ID); return y.Status == "completed" })
	await(t, func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.procs[x.ID] == nil })
	if code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "two"}, testToken); code != 202 {
		t.Fatalf("send %d %s", code, b)
	}
	<-starts
	select {
	case o := <-starts:
		if !o.Resume {
			t.Fatal("restarted process did not resume")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no new process after reaping")
	}
	await(t, func() bool { y, _ := st.Get(x.ID); return y.Status == "completed" })
}

// fakeProc is a scripted Claude process: the test ends turns with finish.
type fakeProc struct {
	hooks    claude.ProcessHooks
	mu       sync.Mutex
	sent     []string
	inflight int
	maxIn    int
	closed   bool
	done     chan struct{}
}

func (p *fakeProc) Send(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, text)
	p.inflight++
	if p.inflight > p.maxIn {
		p.maxIn = p.inflight
	}
	return nil
}
func (p *fakeProc) Interrupt() error      { return nil } // ignores it, like a stuck turn
func (p *fakeProc) BackgroundTasks() int  { return 0 }
func (p *fakeProc) Done() <-chan struct{} { return p.done }
func (p *fakeProc) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	close(p.done)
	p.hooks.Exit(nil)
}
func (p *fakeProc) finish(err error) {
	p.mu.Lock()
	p.inflight--
	p.mu.Unlock()
	p.hooks.TurnDone(err)
}
func (p *fakeProc) sentCopy() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.sent...)
}

func withFakeProc(t *testing.T) (*Server, *httptest.Server, *store.Store, chan *fakeProc) {
	t.Helper()
	s, ts, st := setup(t, nil)
	procs := make(chan *fakeProc, 4)
	s.startProcess = func(o claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		p := &fakeProc{hooks: h, done: make(chan struct{})}
		procs <- p
		return p, nil
	}
	return s, ts, st, procs
}

// Messages are written one at a time, in the order they were accepted, and
// enter the history when written (Claude would merge queued stdin messages).
func TestQueuedMessagesAreSentOneAtATimeInOrder(t *testing.T) {
	_, ts, st, procs := withFakeProc(t)
	x := createSession(t, ts)
	for _, text := range []string{"one", "two", "three"} {
		if code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": text}, testToken); code != 202 {
			t.Fatalf("send %s: %d %s", text, code, b)
		}
	}
	p := <-procs
	await(t, func() bool { return len(p.sentCopy()) == 1 })
	code, b := request(t, ts, "GET", "/api/sessions", nil, testToken)
	if code != 200 || !strings.Contains(string(b), `"queued":[{"text":"two"},{"text":"three"}]`) {
		t.Fatalf("queued listing %d %s", code, b)
	}
	p.finish(nil)
	await(t, func() bool { return len(p.sentCopy()) == 2 })
	p.finish(errors.New("boom"))
	await(t, func() bool { return len(p.sentCopy()) == 3 })
	if y, _ := st.Get(x.ID); y.Status != "running" {
		t.Fatalf("status with a message in flight: %s", y.Status)
	}
	p.finish(nil)
	await(t, func() bool { y, _ := st.Get(x.ID); return y.Status == "completed" })
	if got := p.sentCopy(); strings.Join(got, ",") != "one,two,three" || p.maxIn != 1 {
		t.Fatalf("sent %v, max in flight %d", got, p.maxIn)
	}
	events, _ := st.Events(x.ID, 0)
	var users []string
	for _, e := range events {
		if e.Kind == "user" {
			users = append(users, e.Text)
		}
	}
	if strings.Join(users, ",") != "one,two,three" {
		t.Fatalf("history %v", users)
	}
}

// A second stop ends a turn that ignores the interrupt; the next message
// starts a new process.
func TestSecondStopEndsTheProcess(t *testing.T) {
	_, ts, st, procs := withFakeProc(t)
	x := createSession(t, ts)
	request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "hang"}, testToken)
	p := <-procs
	await(t, func() bool { return len(p.sentCopy()) == 1 })
	for i := 0; i < 2; i++ {
		if code, _ := request(t, ts, "POST", "/api/sessions/"+x.ID+"/stop", nil, testToken); code != 200 {
			t.Fatalf("stop %d: %d", i, code)
		}
	}
	await(t, func() bool { y, _ := st.Get(x.ID); return y.Status == "interrupted" })
	request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "again"}, testToken)
	select {
	case next := <-procs:
		await(t, func() bool { return len(next.sentCopy()) == 1 })
	case <-time.After(3 * time.Second):
		t.Fatal("no new process after the second stop")
	}
}

// An approval request between turns (a background agent) keeps the session
// busy instead of being refused.
func TestBridgeRequestWhileIdle(t *testing.T) {
	s, ts, st, procs := withFakeProc(t)
	x := createSession(t, ts)
	request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "start agent"}, testToken)
	p := <-procs
	await(t, func() bool { return len(p.sentCopy()) == 1 })
	p.finish(nil)
	await(t, func() bool { y, _ := st.Get(x.ID); return y.Status == "completed" })
	s.mu.Lock()
	token := s.procs[x.ID].token
	s.mu.Unlock()
	got := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest("POST", ts.URL+"/api/bridge/request", strings.NewReader(`{"kind":"approval","toolName":"Write","input":{}}`))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			got <- 0
			return
		}
		resp.Body.Close()
		got <- resp.StatusCode
	}()
	var pending []store.Interaction
	await(t, func() bool { pending, _ = st.Pending(x.ID); return len(pending) == 1 })
	if y, _ := st.Get(x.ID); y.Status != "waiting" {
		t.Fatalf("status during idle approval: %s", y.Status)
	}
	request(t, ts, "POST", "/api/requests/"+pending[0].ID, map[string]string{"decision": "allow"}, testToken)
	if code := <-got; code != 200 {
		t.Fatalf("bridge answered %d", code)
	}
	await(t, func() bool { y, _ := st.Get(x.ID); return y.Status == "completed" })
}

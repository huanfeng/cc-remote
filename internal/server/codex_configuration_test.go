package server

import (
	"encoding/json"
	"sync"
	"testing"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

type configurableCodex struct {
	*fakeProc
	configMu      sync.Mutex
	model, effort string
}

func (p *configurableCodex) Configure(model, effort string) {
	p.configMu.Lock()
	defer p.configMu.Unlock()
	p.model, p.effort = model, effort
}

func TestCodexRuntimeReportsAndSwitchesConfiguration(t *testing.T) {
	s, ts, st := setup(t, nil)
	x := createCodexSession(t, ts)
	started := make(chan *configurableCodex, 2)
	s.cfg.StartCodexProcess = func(_ claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		p := &configurableCodex{fakeProc: &fakeProc{hooks: h, done: make(chan struct{})}}
		_ = h.Event(claude.Event{Kind: "session", Payload: json.RawMessage(`{"provider":"codex","session_id":"native-thread","model":"reported-model","modelId":"reported-model","effort":"medium","permissionMode":"workspace-write"}`)})
		started <- p
		return p, nil
	}
	if code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "hold"}, testToken); code != 202 {
		t.Fatalf("send %d %s", code, b)
	}
	p := <-started
	await(t, func() bool { return len(p.sentCopy()) == 1 })
	p.replay("hold")
	if code, b := request(t, ts, "PATCH", "/api/sessions/"+x.ID, map[string]string{"model": "next-model", "effort": "high"}, testToken); code != 200 {
		t.Fatalf("patch %d %s", code, b)
	}
	p.configMu.Lock()
	model, effort := p.model, p.effort
	p.configMu.Unlock()
	if model != "next-model" || effort != "high" {
		t.Fatalf("not configured: %s %s", model, effort)
	}
	_ = p.hooks.Event(claude.Event{Kind: "status", Payload: json.RawMessage(`{"tokenUsage":{"last":{"totalTokens":1000},"total":{"totalTokens":9000},"modelContextWindow":4000}}`)})
	code, b := request(t, ts, "GET", "/api/sessions/"+x.ID, nil, testToken)
	var result struct {
		Session store.Session `json:"session"`
	}
	if code != 200 || json.Unmarshal(b, &result) != nil {
		t.Fatalf("session %d %s", code, b)
	}
	live := result.Session.Live
	if live == nil || live.Model != "reported-model" || live.Effort != "medium" || live.PermissionMode != "workspace-write" || live.ContextTokens != 1000 || live.ContextPercent != 25 {
		t.Fatalf("incorrect live state: %+v", live)
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		t.Fatal("configuration interrupted current process")
	}
	if stored, _ := st.Get(x.ID); stored.Model != "next-model" {
		t.Fatal("override not saved")
	}
	p.result(nil)
}

func TestCodexConfigurationSavedDuringStartup(t *testing.T) {
	s, ts, _ := setup(t, nil)
	x := createCodexSession(t, ts)
	starting, release := make(chan struct{}), make(chan struct{})
	started := make(chan *configurableCodex, 1)
	s.cfg.StartCodexProcess = func(_ claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		close(starting)
		<-release
		p := &configurableCodex{fakeProc: &fakeProc{hooks: h, done: make(chan struct{})}}
		started <- p
		return p, nil
	}
	if code, _ := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "queued"}, testToken); code != 202 {
		t.Fatal(code)
	}
	<-starting
	code, b := request(t, ts, "PATCH", "/api/sessions/"+x.ID, map[string]string{"model": "latest-model", "effort": "high"}, testToken)
	close(release)
	if code != 200 {
		t.Fatalf("patch %d %s", code, b)
	}
	p := <-started
	await(t, func() bool { return len(p.sentCopy()) == 1 })
	p.configMu.Lock()
	model, effort := p.model, p.effort
	p.configMu.Unlock()
	if model != "latest-model" || effort != "high" {
		t.Fatalf("startup lost overrides: %s %s", model, effort)
	}
	p.replay("queued")
	p.result(nil)
}

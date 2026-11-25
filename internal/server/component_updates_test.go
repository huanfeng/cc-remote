package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cc-remote/internal/i18n"
)

const updatesPath = "/api/component-updates"

func TestComponentUpdateRequiresCheckedVersionAndSerializesInstallations(t *testing.T) {
	s, ts, _ := setup(t, noRun)
	s.updates.path = filepath.Join(t.TempDir(), "component-updates.json")
	started := make(chan componentUpdate, 1)
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	t.Cleanup(finish)
	adapter := componentAdapter{
		check: func(context.Context, i18n.Lang) (componentUpdate, error) {
			return componentUpdate{Phase: "available", CurrentVersion: "1.0.0", Version: "1.1.0", Notes: "Release changes"}, nil
		},
		install: func(ctx context.Context, candidate componentUpdate) (string, error) {
			started <- candidate
			select {
			case <-release:
				return candidate.Version, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}
	s.updates.adapters["claude"] = adapter
	s.updates.adapters["codex"] = adapter
	expect := func(method, path string, body any, token string, want int) {
		t.Helper()
		if code, b := request(t, ts, method, updatesPath+path, body, token); code != want {
			t.Fatalf("%s %s: got %d, want %d (%s)", method, path, code, want, b)
		}
	}
	expect("GET", "", nil, "", 401)
	expect("POST", "/anything/check", nil, testToken, 404)
	req := httptest.NewRequest("POST", updatesPath+"/claude/check", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Origin", "https://other.example")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unexpected status for cross-origin check: %d", rec.Code)
	}
	expect("POST", "/claude/install", map[string]string{"version": "1.1.0"}, testToken, 409)
	expect("POST", "/claude/check", nil, testToken, 202)
	expect("POST", "/codex/check", nil, testToken, 202)
	waitForUpdatePhase(t, s, "codex", "available")
	candidate := waitForUpdatePhase(t, s, "claude", "available")
	if candidate.Notes != "Release changes" {
		t.Fatalf("missing candidate notes: %+v", candidate)
	}
	expect("POST", "/claude/install", map[string]string{"version": "1.2.0"}, testToken, 409)
	expect("POST", "/claude/install", map[string]string{"version": "1.1.0"}, testToken, 202)
	select {
	case c := <-started:
		if c.ID != "claude" || c.Version != "1.1.0" {
			t.Fatalf("unexpected install target: %+v", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("update did not start")
	}
	expect("POST", "/codex/install", map[string]string{"version": "1.1.0"}, testToken, 409)
	expect("POST", "/claude/check", nil, testToken, 202)
	waitForUpdatePhase(t, s, "claude", "installing")
	finish()
	result := waitForUpdatePhase(t, s, "claude", "updated")
	if result.CurrentVersion != "1.1.0" {
		t.Fatalf("updated version: %+v", result)
	}
	code, raw := request(t, ts, "GET", updatesPath, nil, testToken)
	var snapshot updateSnapshot
	if code != 200 || json.Unmarshal(raw, &snapshot) != nil || len(snapshot.History) != 1 {
		t.Fatalf("missing update history: %d %s", code, raw)
	}
	entry := snapshot.History[0]
	if entry.Component != "claude" || entry.From != "1.0.0" || entry.Version != "1.1.0" || entry.Status != "updated" || entry.At.IsZero() {
		t.Fatalf("incorrect update entry: %+v", entry)
	}
}

func TestCoreUpdateHistoryReconcilesAfterRestart(t *testing.T) {
	s, ts, _ := setup(t, noRun)
	dataDir := t.TempDir()
	const notesURL = "https://github.com/huanfeng/cc-remote/releases/tag/v0.45.0"
	s.updates.path = filepath.Join(dataDir, "component-updates.json")
	s.updates.adapters["core"] = componentAdapter{
		check: func(context.Context, i18n.Lang) (componentUpdate, error) {
			return componentUpdate{Phase: "available", CurrentVersion: "0.44.0", Version: "v0.45.0", NotesURL: notesURL}, nil
		},
		install: func(_ context.Context, candidate componentUpdate) (string, error) {
			s.mu.Lock()
			s.restartPending = true
			s.mu.Unlock()
			return candidate.Version, nil
		},
	}
	request(t, ts, "POST", updatesPath+"/core/check", nil, testToken)
	waitForUpdatePhase(t, s, "core", "available")
	if code, b := request(t, ts, "POST", updatesPath+"/core/install", map[string]string{"version": "v0.45.0"}, testToken); code != 202 {
		t.Fatalf("start Core update: %d %s", code, b)
	}
	waitForUpdatePhase(t, s, "core", "waiting")

	// Read the same journal as the next Core process would at startup.
	next := &Server{cfg: Config{DataDir: dataDir, Version: "0.45.0"}}
	next.initComponentUpdates()
	result := next.updates.snapshot()
	if next.updates.journal.Pending != nil || result.Components[0].Phase != "updated" || result.Components[0].NotesURL != notesURL ||
		len(result.History) != 1 || result.History[0].Status != "updated" || result.History[0].NotesURL != notesURL {
		t.Fatalf("Core update did not survive restart: %+v", result)
	}
	again := &Server{cfg: next.cfg}
	again.initComponentUpdates()
	if history := again.updates.snapshot().History; len(history) != 1 || history[0].Version != "v0.45.0" {
		t.Fatalf("history not persistent: %+v", history)
	}
}

func TestUpdateProviderLeaseWaitsAndDetachesOnlyTarget(t *testing.T) {
	s := &Server{procs: map[string]*sessionRuntime{
		"active": {provider: "codex", run: &execution{}},
		"other":  {provider: "claude", run: &execution{}},
	}}
	if ok, _, _ := s.leaseUpdateProvider("codex"); ok {
		t.Fatal("leased an active provider")
	}
	s.procs["idle"] = &sessionRuntime{provider: "codex", proc: updateTestIdleProcess{}}
	delete(s.procs, "active")
	ok, _, detached := s.leaseUpdateProvider("codex")
	if !ok || s.updatingProvider != "codex" || len(detached) != 1 || !detached[0].stopping || s.procs["other"] == nil || s.procs["idle"] != nil {
		t.Fatal("did not detach only the idle target process")
	}
}

type updateTestIdleProcess struct{ claudeProcess }

func (updateTestIdleProcess) BackgroundTasks() int { return 0 }

func waitForUpdatePhase(t *testing.T, s *Server, id, phase string) componentUpdate {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.updates.mu.Lock()
		state := s.updates.state[id]
		s.updates.mu.Unlock()
		if state.Phase == phase {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s update phase: got %q, want %q (%s)", id, state.Phase, phase, state.Message)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

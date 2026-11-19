package server

import (
	"encoding/json"
	"strings"
	"testing"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

const terminalModelErrorMessage = "There's an issue with the selected model (glm-5.3-flash[1m]). It may not exist or you may not have access to it. Run /model to pick a different model."

func terminalErrorSession(t *testing.T, base, id string) store.Session {
	t.Helper()
	code, body := call(t, base, "GET", "/api/sessions", nil)
	var sessions []store.Session
	if code != 200 || json.Unmarshal(body, &sessions) != nil {
		t.Fatalf("list: %d %s", code, body)
	}
	for _, session := range sessions {
		if session.ID == id {
			return session
		}
	}
	t.Fatal("session missing")
	return store.Session{}
}

func modelErrorEvents(t *testing.T, st *store.Store, id string) []store.Event {
	t.Helper()
	events, err := st.Events(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Event
	for _, event := range events {
		if event.Kind == "error" && strings.Contains(event.Text, "glm-5.3-flash[1m]") {
			out = append(out, event)
		}
	}
	return out
}

func TestTerminalModelErrorLifecycle(t *testing.T) {
	s, base, st, keys := setupTerminal(t)
	x := createTerminal(t, base)
	send := func(text string) {
		t.Helper()
		if code, body := call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": text}); code != 202 {
			t.Fatalf("send: %d %s", code, body)
		}
	}
	awaitStatus := func(status string) {
		t.Helper()
		awaitLong(t, func() string { return "status = " + terminalErrorSession(t, base, x.ID).Status + ", want " + status }, func() bool {
			return terminalErrorSession(t, base, x.ID).Status == status
		})
	}
	send("/model-error-test")
	awaitStatus("failed")
	if errors := modelErrorEvents(t, st, x.ID); len(errors) != 1 || flatText(errors[0].Text) != flatText(terminalModelErrorMessage) {
		t.Fatalf("model error was not synchronized once: %+v", errors)
	}
	s.mu.Lock()
	ts := s.terminals[x.ID]
	s.mu.Unlock()
	for range 3 {
		s.syncTerminal(ts)
	}
	if len(modelErrorEvents(t, st, x.ID)) != 1 {
		t.Fatal("polling duplicated the model error")
	}
	if err := ts.runner.Keys([]byte("s")); err != nil {
		t.Fatal(err)
	}
	awaitLong(t, func() string { return "late Stop not processed" }, func() bool { return strings.Contains(keys(x.ID), "MODEL-LATE-STOP") })
	if latest := terminalErrorSession(t, base, x.ID); latest.Status != "failed" {
		t.Fatalf("late Stop hid failure: %+v", latest)
	}
	if err := ts.runner.Keys([]byte("t")); err != nil {
		t.Fatal(err)
	}
	awaitLong(t, func() string { return "late transcript error not written" }, func() bool { return strings.Contains(keys(x.ID), "MODEL-TRANSCRIPT-ERROR") })
	s.syncTerminal(ts)
	if errors := modelErrorEvents(t, st, x.ID); len(errors) != 1 {
		t.Fatalf("late transcript duplicated screen error: %+v", errors)
	}
	send("/model-error-recover-test")
	awaitStatus("running")
	for range 3 {
		s.syncTerminal(ts)
	}
	if latest := terminalErrorSession(t, base, x.ID); latest.Status != "running" {
		t.Fatalf("old error overwrote recovered turn: %+v", latest)
	}
	if err := ts.runner.Keys([]byte("f")); err != nil {
		t.Fatal(err)
	}
	awaitStatus("completed")
	send("/model-error-test")
	awaitStatus("failed")
	if errors := modelErrorEvents(t, st, x.ID); len(errors) != 2 {
		t.Fatalf("same failure on a new turn should be reported again: %+v", errors)
	}
	// The same model can fail again without a successful turn or a polled
	// empty screen between errors. A fresh repaint must not look stale.
	send("/model-error-test")
	awaitLong(t, func() string { return "consecutive identical model failure was ignored" }, func() bool {
		return terminalErrorSession(t, base, x.ID).Status == "failed" && len(modelErrorEvents(t, st, x.ID)) == 3
	})
}

func TestTerminalTranscriptErrorWithoutStop(t *testing.T) {
	_, base, st, _ := setupTerminal(t)
	x := createTerminal(t, base)
	if code, body := call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "/transcript-error-test"}); code != 202 {
		t.Fatalf("send: %d %s", code, body)
	}
	awaitLong(t, func() string {
		return "transcript API error left terminal " + terminalErrorSession(t, base, x.ID).Status
	}, func() bool {
		return terminalErrorSession(t, base, x.ID).Status == "failed"
	})
	if errors := modelErrorEvents(t, st, x.ID); len(errors) != 1 || errors[0].Text != terminalModelErrorMessage {
		t.Fatalf("transcript error not synchronized once: %+v", errors)
	}
}

func TestImportedHistoricalAPIErrorsAreNotDuplicated(t *testing.T) {
	s, _, st := setup(t, noRun)
	x, conn := connectTerminal(t, s, st, store.ID(), t.TempDir())
	old := claude.Event{Kind: "assistant", Text: terminalModelErrorMessage}
	s.mu.Lock()
	s.applyTranscriptLocked(conn, claude.NativeSession{}, []claude.Event{old})
	current := old
	current.Kind = "error"
	s.applyTranscriptLocked(conn, claude.NativeSession{}, []claude.Event{current})
	s.applyTranscriptLocked(conn, claude.NativeSession{}, []claude.Event{current, {Kind: "user", Text: "retry"}, current})
	s.mu.Unlock()
	events, err := st.Events(x.ID, 0)
	if err != nil || len(events) != 3 || events[0].Kind != "assistant" || events[2].Kind != "error" {
		t.Fatalf("historical failure duplicated or new failure lost: %+v %v", events, err)
	}
}

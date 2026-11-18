package server

import (
	"encoding/json"
	"strings"
	"testing"

	"cc-remote/internal/store"
)

func TestTerminalRetryStatusLifecycle(t *testing.T) {
	s, base, st, _ := setupTerminal(t)
	x := createTerminal(t, base)
	if code, body := call(t, base, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "/retry-status-test"}); code != 202 {
		t.Fatalf("send: %d %s", code, body)
	}
	var latest store.Session
	read := func() store.Session {
		code, body := call(t, base, "GET", "/api/sessions", nil)
		var sessions []store.Session
		if code != 200 || json.Unmarshal(body, &sessions) != nil {
			t.Fatalf("list: %d %s", code, body)
		}
		for _, session := range sessions {
			if session.ID == x.ID {
				latest = session
				return session
			}
		}
		t.Fatal("session missing")
		return store.Session{}
	}
	awaitLong(t, func() string { return "missing retry in session API" }, func() bool {
		y := read()
		return y.Live != nil && y.Live.Retry != nil
	})
	if latest.Status != "running" || latest.Live.Retry.Attempt != 6 || latest.Live.Retry.MaxAttempts != 10 || !strings.Contains(latest.Live.Retry.Message, "ECONNRESET") {
		t.Fatalf("retry payload: %+v %+v", latest, latest.Live.Retry)
	}
	s.mu.Lock()
	runner := s.terminals[x.ID].runner
	s.mu.Unlock()
	if err := runner.Keys([]byte("u")); err != nil {
		t.Fatal(err)
	}
	awaitLong(t, func() string { return "retry counter not updated" }, func() bool {
		y := read()
		return y.Live != nil && y.Live.Retry != nil && y.Live.Retry.Attempt == 7
	})
	if err := st.SetStatus(x.ID, "waiting"); err != nil {
		t.Fatal(err)
	}
	if y := read(); y.Live != nil && y.Live.Retry != nil {
		t.Fatal("stale retry displayed while waiting for user")
	}
	if err := st.SetStatus(x.ID, "running"); err != nil {
		t.Fatal(err)
	}
	if err := runner.Keys([]byte("r")); err != nil {
		t.Fatal(err)
	}
	awaitLong(t, func() string { return "retry not cleared on recovery" }, func() bool {
		y := read()
		return y.Status == "running" && y.Live != nil && y.Live.Retry == nil && y.Live.Activity == "Thinking…"
	})
	if err := runner.Keys([]byte("f")); err != nil {
		t.Fatal(err)
	}
	awaitLong(t, func() string { return "completion not reported" }, func() bool {
		y := read()
		return y.Status == "completed" && (y.Live == nil || y.Live.Retry == nil)
	})
}

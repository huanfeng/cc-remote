package server

import (
	"strings"
	"time"

	"cc-remote/internal/claude"
)

// A failed request can leave Claude Code alive at the prompt without a Stop
// hook. Explicit transcript diagnostics are preferred; model validation and
// expired-login screens are fallbacks for errors Claude Code does not persist.
func (s *Server) syncTerminalFailureLocked(ts *terminalSession, events []claude.Event, haveTranscript bool) {
	if haveTranscript {
		for _, event := range events[min(ts.errorScan, len(events)):] {
			if at, err := time.Parse(time.RFC3339Nano, event.CreatedAt); err == nil && at.Before(ts.turnStarted) {
				continue
			}
			switch event.Kind {
			case "error":
				ts.pendingError = strings.TrimSpace(event.Text)
			case "user", "assistant", "tool":
				ts.pendingError = ""
			}
		}
		ts.errorScan = len(events)
	}
	message, version := ts.runner.ModelError()
	if message == "" {
		ts.ignoredModelError = ""
	}
	if ts.turnFailed {
		return
	}
	x, err := s.store.Get(ts.id)
	// An idle terminal can show an expired-login footer before its first
	// submission. Only that complete parsed diagnostic is proactive; stale
	// model failures in an idle terminal are not a new failed turn.
	idleLogin := x.Status == "idle" && strings.HasPrefix(message, "Login expired")
	if err != nil || x.Status != "running" && x.Status != "completed" && !idleLogin {
		return
	}
	if _, retry := ts.runner.ActivityState(); retry != nil {
		return // a recoverable request is still retrying
	}
	if ts.pendingError != "" {
		message = ts.pendingError
	} else if message == ts.ignoredModelError && version <= ts.ignoredModelVersion {
		return // an old diagnostic still on screen after submitting a new turn
	}
	if message == "" {
		return
	}
	if ts.pendingError == "" {
		if _, err := s.store.Append(ts.id, "error", message, nil); err != nil {
			return
		}
	}
	ts.turnFailed, ts.failureMessage = true, message
	_ = s.store.SetStatus(ts.id, "failed")
	// Held messages remain editable and are not automatically sent into the
	// same invalid model. The user can change the model and explicitly retry.
}

func (s *Server) resetTerminalFailureLocked(ts *terminalSession) {
	ts.turnStarted = time.Now()
	ts.turnFailed, ts.failureMessage, ts.pendingError = false, "", ""
	ts.ignoredModelError, ts.ignoredModelVersion = ts.runner.ModelError()
}

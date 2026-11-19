package claude

import (
	"encoding/json"
	"testing"
)

func TestNativeAPIErrorEvents(t *testing.T) {
	modelError := "There's an issue with the selected model (glm-5.3-flash[1m]). It may not exist or you may not have access to it. Run /model to pick a different model."
	for _, tc := range []struct {
		name  string
		text  string
		flags map[string]any
		want  string
	}{
		{"selected model", modelError, map[string]any{"isApiErrorMessage": true, "error": "invalid_request"}, "error"},
		{"API failure", "API Error: Connection dropped (ECONNRESET)", map[string]any{"isApiErrorMessage": true, "apiError": "connection_failed", "error": "server_error"}, "error"},
		{"ordinary assistant quoting error", modelError, nil, "assistant"},
		{"explicit nonerror", modelError, map[string]any{"isApiErrorMessage": false}, "assistant"},
		{"sidechain failure", modelError, map[string]any{"isApiErrorMessage": true, "isSidechain": true}, ""},
		{"usage menu", "You've hit your weekly limit", map[string]any{"isApiErrorMessage": true, "apiError": "usage_limit_reached", "error": "rate_limit"}, "assistant"},
		{"rate limit", "API Error: Rate limited", map[string]any{"isApiErrorMessage": true, "error": "rate_limit"}, "assistant"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := map[string]any{"type": "assistant", "timestamp": "2026-11-19T01:02:03Z", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": tc.text}}}}
			for key, value := range tc.flags {
				v[key] = value
			}
			line, _ := json.Marshal(v)
			var session NativeSession
			events := transcriptEvents(line, &session)
			if tc.want == "" {
				if len(events) != 0 {
					t.Fatalf("ignored record emitted %+v", events)
				}
				return
			}
			if len(events) != 1 || events[0].Kind != tc.want || events[0].Text != tc.text || events[0].CreatedAt != "2026-11-19T01:02:03Z" {
				t.Fatalf("events %+v, want %s preserving original text/time", events, tc.want)
			}
			if session.LastConversationAt != "2026-11-19T01:02:03Z" {
				t.Fatalf("error must retain conversation activity time: %+v", session)
			}
		})
	}
}

func TestNativeToolErrorIsNotTurnFailure(t *testing.T) {
	events := transcriptEvents([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call","is_error":true,"content":"command failed"}]}}`), &NativeSession{})
	if len(events) != 1 || events[0].Kind != "tool_result" {
		t.Fatalf("tool failure emitted %+v", events)
	}
}

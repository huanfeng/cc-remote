package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/store"
	"github.com/coder/websocket"
)

func TestTerminalCompactLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, command, key, finalStatus string
		compacted                       bool
	}{
		{"manual completion", "/compact keep the API notes", "f", "completed", true},
		{"automatic completion keeps the turn running", "/compact-auto-test", "f", "running", true},
		{"failure without Stop", "/compact", "e", "failed", false},
		{"web stop", "/compact", "", "interrupted", false},
		{"terminal Esc", "/compact", "\x1b", "interrupted", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, base, st, _ := setupTerminal(t)
			x := createTerminal(t, base)
			path := "/api/sessions/" + x.ID
			if code, body := call(t, base, "POST", path+"/messages", map[string]string{"text": tc.command}); code != http.StatusAccepted {
				t.Fatalf("send: %d %s", code, body)
			}
			read := func() store.Session {
				code, body := call(t, base, "GET", path, nil)
				var reply struct{ Session store.Session }
				if code != http.StatusOK || json.Unmarshal(body, &reply) != nil || reply.Session.ID != x.ID {
					t.Fatalf("session: %d %s", code, body)
				}
				return reply.Session
			}
			awaitLong(t, func() string { return "compaction did not become visible" }, func() bool {
				session := read()
				return session.Status == "running" && session.Live != nil && session.Live.Compacting
			})
			s.mu.Lock()
			ts := s.terminals[x.ID]
			s.mu.Unlock()
			settings, err := os.ReadFile(filepath.Join(ts.dir, "settings.json"))
			var config struct{ Hooks map[string]json.RawMessage }
			if err != nil || json.Unmarshal(settings, &config) != nil || len(config.Hooks["PreCompact"]) == 0 || len(config.Hooks["PostCompact"]) == 0 {
				t.Fatalf("compaction hooks missing from terminal settings: %v", err)
			}
			switch tc.key {
			case "":
				if code, body := call(t, base, "POST", path+"/stop", nil); code != http.StatusOK {
					t.Fatalf("stop: %d %s", code, body)
				}
			case "\x1b":
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				url := "ws" + strings.TrimPrefix(base, "http") + "/api/terminals/" + x.ID + "/stream"
				conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + testToken}}})
				if err != nil {
					t.Fatal(err)
				}
				defer conn.CloseNow()
				if err := conn.Write(ctx, websocket.MessageBinary, []byte(tc.key)); err != nil {
					t.Fatal(err)
				}
			default:
				if err := ts.runner.Keys([]byte(tc.key)); err != nil {
					t.Fatal(err)
				}
			}
			awaitLong(t, func() string { return "compaction did not settle as " + tc.finalStatus }, func() bool {
				session := read()
				return session.Status == tc.finalStatus && (session.Live == nil || !session.Live.Compacting)
			})
			if tc.finalStatus == "interrupted" {
				awaitLong(t, func() string { return "late compaction hook did not run" }, func() bool {
					keys, _ := os.ReadFile(filepath.Join(s.cfg.ClaudeConfigEnv, "keys.log"))
					return strings.Contains(string(keys), "COMPACT-LATE-POST")
				})
				if session := read(); session.Status != "interrupted" {
					t.Fatalf("late hook changed interrupted status to %s", session.Status)
				}
			}
			if tc.compacted {
				var compactSeq int64
				awaitLong(t, func() string { return "missing compaction divider" }, func() bool {
					events, _ := st.Events(x.ID, 0)
					for _, event := range events {
						if event.Kind == "compact" {
							compactSeq = event.Seq
						}
					}
					return compactSeq != 0
				})
				if tc.finalStatus == "completed" && read().CompletionEventSeq < compactSeq {
					t.Fatal("compaction divider was imported after the completion watermark")
				}
			}
		})
	}
}

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cc-remote/internal/store"
)

func TestImportedConversationUsesOriginalClock(t *testing.T) {
	s, ts, st := setup(t, nil)
	id := store.ID()
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "clock")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	first, _ := json.Marshal(map[string]any{"type": "user", "timestamp": "2025-05-01T10:00:00+08:00", "cwd": t.TempDir(), "message": map[string]any{"content": "old question"}})
	second := `{"type":"assistant","timestamp":"2025-05-01T02:03:00Z","message":{"content":[{"type":"text","text":"old response"}]}}`
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(string(first)+"\n"+second+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code, body := request(t, ts, "POST", "/api/native/sessions/"+id+"/import", map[string]bool{"confirmStopped": true}, testToken)
	if code != 201 {
		t.Fatalf("import %d %s", code, body)
	}
	var x store.Session
	if err := json.Unmarshal(body, &x); err != nil {
		t.Fatal(err)
	}
	if x.LastConversationAt != "2025-05-01T02:03:00.000000000Z" || x.ConversationTimeEstimated {
		t.Fatalf("import clock: %+v", x)
	}
	_ = st.SetStatus(x.ID, "waiting")
	_ = st.SetTitle(x.ID, "new title")
	_ = st.SetOverrides(x.ID, "default", "test", "high")
	got, _ := st.Get(x.ID)
	if got.LastConversationAt != x.LastConversationAt {
		t.Fatalf("administration changed clock: %+v", got)
	}
	events, err := st.Events(x.ID, 0)
	if err != nil || len(events) != 2 || events[0].CreatedAt != "2025-05-01T02:00:00.000000000Z" {
		t.Fatalf("source event clock: %+v %v", events, err)
	}
}

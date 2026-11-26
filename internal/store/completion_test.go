package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestCompletionZeroWatermarkIsExposed(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	x, err := s.Create("no events", "/tmp", ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(x.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	x, err = s.Get(x.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(x)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["completionEventSeq"]) != "0" || string(fields["completionReadAt"]) != `""` || x.CompletedAt == "" {
		t.Fatalf("zero watermark missing: %s", body)
	}
}

func TestCompletionIdentityAndWatermark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	x, err := s.Create("task", "/tmp", ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(x.ID, "running"); err != nil {
		t.Fatal(err)
	}
	e, err := s.Append(x.ID, "assistant", "finished", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(x.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	first, err := s.Get(x.ID)
	if err != nil || first.CompletedAt == "" || first.CompletionEventSeq != e.Seq {
		t.Fatalf("first completion: %+v %v", first, err)
	}
	if err := s.MarkCompletionRead(x.ID, first.CompletedAt); err != nil {
		t.Fatal(err)
	}
	// Settings, transcript administration and repeated completion notifications
	// must not make an already-read result appear unread again.
	if err := s.SetTitle(x.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(x.ID, "status", "administration", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(x.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	again, err := s.FindNative(x.Provider, x.ClaudeSessionID)
	if err != nil || again.CompletedAt != first.CompletedAt || again.CompletionEventSeq != first.CompletionEventSeq || again.CompletionReadAt != first.CompletedAt {
		t.Fatalf("duplicate completion changed identity: %+v %v", again, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].CompletedAt != first.CompletedAt || list[0].CompletionEventSeq != first.CompletionEventSeq || list[0].CompletionReadAt != first.CompletedAt {
		t.Fatalf("reopened list: %+v %v", list, err)
	}
	if err := s.SetStatus(x.ID, "running"); err != nil {
		t.Fatal(err)
	}
	e, err = s.Append(x.ID, "result", "second result", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(x.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCompletionRead(x.ID, first.CompletedAt); err != nil {
		t.Fatal(err)
	}
	second, err := s.Get(x.ID)
	if err != nil || second.CompletedAt == first.CompletedAt || second.CompletionEventSeq != e.Seq || second.CompletionReadAt != "" {
		t.Fatalf("second completion: %+v %v", second, err)
	}
	if err := s.Delete(x.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM session_completion`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("completion cleanup: %d %v", count, err)
	}
}

func TestUpgradeBaselinesCompletionHistoryOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	x, err := s.Create("existing", t.TempDir(), ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(x.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`ALTER TABLE session_completion DROP COLUMN read_completed_at`); err != nil {
		t.Fatal(err)
	}
	reopen := func() {
		t.Helper()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		upgraded, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		s = upgraded
	}
	reopen()
	got, err := s.Get(x.ID)
	if err != nil || got.CompletedAt == "" || got.CompletionReadAt != got.CompletedAt {
		t.Fatalf("upgrade baseline: %+v %v", got, err)
	}
	for _, status := range []string{"running", "completed"} {
		if err := s.SetStatus(x.ID, status); err != nil {
			t.Fatal(err)
		}
	}
	reopen()
	got, err = s.Get(x.ID)
	if err != nil || got.CompletionReadAt != "" {
		t.Fatalf("reopening must preserve unread completions: %+v %v", got, err)
	}
}

func TestCompletionDoesNotBackfillHistoricalSessions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	x, err := s.Create("old", "/tmp", ID(), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE sessions SET status='completed' WHERE id=?`, x.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(x.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(x.ID)
	if err != nil || got.CompletedAt != "" || got.CompletionEventSeq != 0 {
		t.Fatalf("historical completion should stay without identity: %+v %v", got, err)
	}
}

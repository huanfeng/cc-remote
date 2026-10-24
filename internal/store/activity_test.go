package store

import (
	"path/filepath"
	"testing"
)

func TestConversationActivityIgnoresAdministration(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, _ := s.Create("A", "/tmp", ID(), false)
	b, _ := s.Create("B", "/tmp", ID(), false)
	for _, item := range []struct{ id, at string }{{a.ID, "2026-01-01T11:00:00+08:00"}, {b.ID, "2026-01-01T04:00:00Z"}} {
		if _, err := s.AppendAt(item.id, "user", "hello", nil, item.at); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.SetTitle(a.ID, "renamed")
	_ = s.SetOverrides(a.ID, "read-only", "test", "high")
	_ = s.SetStatus(a.ID, "running")
	_, _ = s.Append(a.ID, "error", "host restarted", nil)
	_, _ = s.Append(a.ID, "status", "connected", nil)
	_ = s.ResetContext(a.ID)
	list, err := s.List()
	if err != nil || list[0].ID != b.ID {
		t.Fatalf("order: %+v %v", list, err)
	}
	got, _ := s.Get(a.ID)
	if got.LastConversationAt != "2026-01-01T03:00:00.000000000Z" || got.ConversationTimeEstimated {
		t.Fatalf("activity: %+v", got)
	}
}

func TestNativeConversationTimePreservedAndCorrectsEstimate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	x, _ := s.Create("imported", "/tmp", ID(), true)
	if _, err = s.RecordTranscriptAt(x.ID, "one", "assistant", "old", nil, "2025-02-01T10:30:00Z"); err != nil {
		t.Fatal(err)
	}
	if added, err := s.RecordTranscriptAt(x.ID, "one", "assistant", "old", nil, "2026-02-01T10:30:00Z"); err != nil || added {
		t.Fatal("duplicate sync changed activity", err)
	}
	if _, err = s.AppendAt(x.ID, "assistant", "unknown", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err = s.SetConversationTime(x.ID, "2026-02-01T10:30:00Z", true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(x.ID)
	if got.LastConversationAt != "2025-02-01T10:30:00.000000000Z" || got.ConversationTimeEstimated {
		t.Fatalf("mtime replaced exact clock: %+v", got)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, _ = s.Get(x.ID)
	if got.LastConversationAt != "2025-02-01T10:30:00.000000000Z" {
		t.Fatalf("reopen: %+v", got)
	}
	y, _ := s.Create("estimated", "/tmp", ID(), true)
	_ = s.SetConversationTime(y.ID, "2026-02-01T10:30:00Z", true)
	_ = s.SetConversationTime(y.ID, "2025-02-01T10:30:00Z", false)
	got, _ = s.Get(y.ID)
	if got.ConversationTimeEstimated || got.LastConversationAt != "2025-02-01T10:30:00.000000000Z" {
		t.Fatalf("estimate not corrected: %+v", got)
	}
	if err = s.Delete(y.ID); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundTaskActivityHasConversationClock(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	x, _ := s.Create("task", "/tmp", ID(), false)
	_, err = s.AppendAt(x.ID, "assistant", "main turn done", nil, "2026-01-01T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AppendAt(x.ID, "status", "task_notification", nil, "2026-01-01T10:20:00Z")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AppendAt(x.ID, "status", "connected", nil, "2026-01-01T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(x.ID)
	if got.LastConversationAt != "2026-01-01T10:20:00.000000000Z" {
		t.Fatalf("background activity: %+v", got)
	}
}

func TestConversationActivityBackfillOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	x, _ := s.Create("legacy", "/tmp", ID(), false)
	_, _ = s.AppendAt(x.ID, "assistant", "old", nil, "2025-02-01T10:30:00Z")
	_, err = s.db.Exec(`DROP TABLE session_activity`)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, _ := s.Get(x.ID)
	if got.LastConversationAt != "2025-02-01T10:30:00.000000000Z" || !got.ConversationTimeEstimated {
		t.Fatalf("backfill: %+v", got)
	}
}

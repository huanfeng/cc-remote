package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDurableEventsAndInterruptedRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	x, err := s.Create("Session", t.TempDir(), ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.Append(x.ID, "assistant_delta", "你好", json.RawMessage(`{"type":"stream_event"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetResume(x.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.SetStatus(x.ID, "waiting"); err != nil {
		t.Fatal(err)
	}
	req := Interaction{ID: ID(), SessionID: x.ID, Kind: "approval", ToolName: "Bash", Input: json.RawMessage(`{"command":"git status"}`)}
	if err = s.AddInteraction(req); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	x, err = s.Get(x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if x.Status != "interrupted" || !x.Resume {
		t.Fatalf("invalid recovered session: %+v", x)
	}
	events, err := s.Events(x.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Text != "你好" || events[1].Kind != "error" {
		t.Fatalf("events: %+v", events)
	}
	after, err := s.Events(x.ID, e.Seq)
	if err != nil || len(after) != 1 {
		t.Fatalf("cursor: %+v %v", after, err)
	}
	pending, err := s.Pending(x.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending survived restart: %+v %v", pending, err)
	}
}

func TestApprovalExactlyOnce(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	x, err := s.Create("One", t.TempDir(), ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	r := Interaction{ID: ID(), SessionID: x.ID, Kind: "question", ToolName: "AskUserQuestion", Input: json.RawMessage(`{}`)}
	if err = s.AddInteraction(r); err != nil {
		t.Fatal(err)
	}
	if err = s.Resolve(r.ID, "resolved"); err != nil {
		t.Fatal(err)
	}
	if err = s.Resolve(r.ID, "resolved"); err == nil {
		t.Fatal("second resolution accepted")
	}
}

func TestDeleteRemovesOnlyThatSession(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var ids []string
	for _, title := range []string{"gone", "kept"} {
		x, err := s.Create(title, t.TempDir(), ID(), false)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, x.ID)
		if _, err = s.Append(x.ID, "user", title, nil); err != nil {
			t.Fatal(err)
		}
		if err = s.AddInteraction(Interaction{ID: ID(), SessionID: x.ID, Kind: "approval", ToolName: "Bash", Input: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err = s.RecordTranscript(x.ID, "k:1", "assistant", "hi", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Delete(ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ids[0]); err != sql.ErrNoRows {
		t.Fatalf("session survived: %v", err)
	}
	for table, want := range map[string]int{"events": 2, "interactions": 1, "channel_transcript": 1} {
		var gone, kept int
		_ = s.db.QueryRow(`SELECT count(*) FROM `+table+` WHERE session_id=?`, ids[0]).Scan(&gone)
		_ = s.db.QueryRow(`SELECT count(*) FROM `+table+` WHERE session_id=?`, ids[1]).Scan(&kept)
		if gone != 0 || kept != want {
			t.Fatalf("%s: deleted session rows %d, other session rows %d", table, gone, kept)
		}
	}
	if err = s.Delete(ids[0]); err != sql.ErrNoRows {
		t.Fatalf("second delete: %v", err)
	}
}

func TestMigratesOldSessionSchemaWithOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE sessions(id TEXT PRIMARY KEY,title TEXT NOT NULL,cwd TEXT NOT NULL,claude_id TEXT NOT NULL UNIQUE,status TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,resume INTEGER NOT NULL DEFAULT 0);
INSERT INTO sessions VALUES('old','Old','/tmp','abcd1234-1234-1234-1234-123456789abc','idle','t','t',1);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	x, err := s.Get("old")
	if err != nil || x.Mode != "managed" || x.PermissionMode != "" || x.Model != "" || x.Effort != "" || !x.Resume {
		t.Fatalf("migrated %+v %v", x, err)
	}
	if err = s.SetOverrides("old", "bypassPermissions", "opus", "high"); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].PermissionMode != "bypassPermissions" || list[0].Model != "opus" || list[0].Effort != "high" {
		t.Fatalf("overrides %+v %v", list, err)
	}
}

// Sessions of the retired shared mode reopen as stopped terminal sessions whose
// recorded transcript is not recorded again; the migration is idempotent.
func TestMigratesSharedSessionsToTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := s.Create("Shared", "/tmp", "ABCD1234-1234-1234-1234-123456789ABC", true)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := s.Create("Managed", "/tmp", ID(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetMode(shared.ID, "channel"); err != nil {
		t.Fatal(err)
	}
	if err = s.SetStatus(shared.ID, "offline"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordTranscript(shared.ID, "hash:1", "assistant", "hello", nil); err != nil {
		t.Fatal(err)
	}
	s.Close()
	for range 2 {
		if s, err = Open(path); err != nil {
			t.Fatal(err)
		}
		x, err := s.Get(shared.ID)
		if err != nil || x.Mode != "terminal" || x.Status != "idle" || x.ClaudeSessionID != shared.ClaudeSessionID || !x.Resume {
			t.Fatalf("migrated %+v %v", x, err)
		}
		if added, err := s.RecordTranscript(shared.ID, "abcd1234-1234-1234-1234-123456789abc:hash:1", "assistant", "hello", nil); err != nil || added {
			t.Fatalf("transcript recorded again: %v %v", added, err)
		}
		if y, err := s.Get(managed.ID); err != nil || y.Mode != "managed" {
			t.Fatalf("managed session changed %+v %v", y, err)
		}
		s.Close()
	}
}

// Task events come from the partial index, not a scan of the session's events.
func TestTaskEventsUsePartialIndex(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	x, _ := s.Create("Session", t.TempDir(), ID(), false)
	_, _ = s.Append(x.ID, "status", "task_started", json.RawMessage(`{"task_id":"a"}`))
	_, _ = s.Append(x.ID, "status", "requesting", json.RawMessage(`{}`))
	_, _ = s.Append(x.ID, "assistant", "task_ looks alike", nil)
	events, err := s.TaskEvents(x.ID)
	if err != nil || len(events) != 1 || events[0].Text != "task_started" {
		t.Fatal(events, err)
	}
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT seq,session_id,kind,text,payload,created_at FROM events WHERE session_id=? AND kind='status' AND text LIKE 'task_%' ORDER BY seq`, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var id, parent, unused int
		var detail string
		_ = rows.Scan(&id, &parent, &unused, &detail)
		plan += detail + "; "
	}
	if !strings.Contains(plan, "events_tasks") {
		t.Fatalf("plan does not use the partial index: %s", plan)
	}
}

// A session synced by a host without task events is flagged once; one synced
// by this host (TranscriptV2Key) never is.
func TestLegacyTranscriptFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := s.Create("Old", "/tmp", ID(), true)
	current, _ := s.Create("Current", "/tmp", ID(), true)
	if _, err = s.RecordTranscript(old.ID, "abc:hash:1", "assistant", "hello", nil); err != nil {
		t.Fatal(err)
	}
	_ = s.MarkTranscript(current.ID, TranscriptV2Key)
	_, _ = s.RecordTranscript(current.ID, "abc:hash:2", "assistant", "hi", nil)
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	if !s.TranscriptFlag(old.ID, LegacyTasksKey) || !s.TranscriptFlag(old.ID, TranscriptV2Key) || s.TranscriptFlag(current.ID, LegacyTasksKey) {
		t.Fatal("flags after the upgrade")
	}
	if err = s.ClearTranscriptFlag(old.ID, LegacyTasksKey); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.TranscriptFlag(old.ID, LegacyTasksKey) {
		t.Fatal("flagged again on the next start")
	}
}

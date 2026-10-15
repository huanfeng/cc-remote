package store

import (
	"encoding/json"
	"path/filepath"
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

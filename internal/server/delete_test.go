package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

func TestDeleteManagedSessionStopsOnlyWhenAsked(t *testing.T) {
	started := make(chan struct{}, 1)
	s, ts, st := setup(t, func(ctx context.Context, _ claude.Options, _ func(claude.Event) error) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	s.cfg.UploadDir = t.TempDir()
	a, kept := createSession(t, ts), createSession(t, ts)
	for _, x := range []store.Session{a, kept} {
		if c, b := uploadFile(t, ts, x.ID, testToken, "notes.txt", []byte("x")); c != 201 {
			t.Fatalf("upload %d %s", c, b)
		}
	}
	if c, b := request(t, ts, "POST", "/api/sessions/"+a.ID+"/messages", map[string]string{"text": "work"}, testToken); c != 202 {
		t.Fatalf("send %d %s", c, b)
	}
	<-started
	if c, b := request(t, ts, "DELETE", "/api/sessions/"+a.ID, nil, testToken); c != 409 {
		t.Fatalf("deleted a running session: %d %s", c, b)
	}
	if c, b := request(t, ts, "DELETE", "/api/sessions/"+a.ID+"?stop=1", nil, testToken); c != 200 {
		t.Fatalf("stop and delete: %d %s", c, b)
	}
	if _, err := st.Get(a.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("session row survived: %v", err)
	}
	if events, _ := st.Events(a.ID, 0); len(events) != 0 {
		t.Fatalf("events survived: %+v", events)
	}
	if _, err := os.Stat(filepath.Join(s.cfg.UploadDir, a.ID)); !os.IsNotExist(err) {
		t.Fatalf("upload dir survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.cfg.UploadDir, kept.ID)); err != nil {
		t.Fatalf("other session uploads removed: %v", err)
	}
	if c, _ := request(t, ts, "GET", "/api/sessions/"+a.ID, nil, testToken); c != 404 {
		t.Fatal("deleted session still readable")
	}
	if c, _ := request(t, ts, "DELETE", "/api/sessions/"+a.ID, nil, testToken); c != 404 {
		t.Fatal("second delete")
	}
	if c, _ := request(t, ts, "DELETE", "/api/sessions/"+kept.ID, nil, ""); c != 401 {
		t.Fatal("anonymous delete")
	}
	s.mu.Lock()
	_, running := s.runs[a.ID]
	s.mu.Unlock()
	if running {
		t.Fatal("run survived delete")
	}
}

func TestDeleteChannelSessionRequiresOfflineTerminal(t *testing.T) {
	_, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	a, key := registerChannel(t, ts, id, t.TempDir())
	if c, b := request(t, ts, "DELETE", "/api/sessions/"+a.ID+"?stop=1", nil, testToken); c != 409 {
		t.Fatalf("deleted online channel: %d %s", c, b)
	}
	if c, b := channelReq(t, ts, "POST", "/api/channels/"+id+"/disconnect", key, nil); c != 200 {
		t.Fatalf("disconnect %d %s", c, b)
	}
	if c, b := request(t, ts, "DELETE", "/api/sessions/"+a.ID, nil, testToken); c != 200 {
		t.Fatalf("delete offline channel: %d %s", c, b)
	}
	if _, err := st.FindClaude(id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("channel session survived: %v", err)
	}
	// Reconnecting the same native session starts a fresh record.
	b, _ := registerChannel(t, ts, id, a.Cwd)
	if b.ID == a.ID {
		t.Fatal("reused deleted record")
	}
	var list []store.Session
	_, raw := request(t, ts, "GET", "/api/sessions", nil, testToken)
	_ = json.Unmarshal(raw, &list)
	if len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("list %+v", list)
	}
}

func TestSessionUploadDirStaysInsideRoot(t *testing.T) {
	s := New(nil, Config{UploadDir: t.TempDir()})
	for _, bad := range []string{"", "..", "../" + store.ID(), store.ID() + "/..", "x"} {
		if _, err := s.sessionUploadDir(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	id := store.ID()
	dir, err := s.sessionUploadDir(id)
	if err != nil || dir != filepath.Join(s.cfg.UploadDir, id) {
		t.Fatalf("%s %v", dir, err)
	}
	if _, err = New(nil, Config{UploadDir: "relative"}).sessionUploadDir(id); err == nil {
		t.Fatal("relative upload root accepted")
	}
}

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

func channelReq(t *testing.T, ts *httptest.Server, method, path, key string, body any) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(body)
	r, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("X-Channel-Key", key)
	res, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out
}
func registerChannel(t *testing.T, ts *httptest.Server, id, cwd string) (store.Session, string) {
	t.Helper()
	code, b := channelReq(t, ts, "POST", "/api/channels/register", "", map[string]string{"id": id, "cwd": cwd})
	if code != 200 {
		t.Fatalf("register %d %s", code, b)
	}
	var x struct {
		store.Session
		Key string `json:"connectionKey"`
	}
	if e := json.Unmarshal(b, &x); e != nil {
		t.Fatal(e)
	}
	return x.Session, x.Key
}
func TestChannelsUseOneNativeProcessAndIsolateSessions(t *testing.T) {
	var spawned atomic.Int32
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { spawned.Add(1); return nil })
	idA, idB := store.ID(), store.ID()
	a, keyA := registerChannel(t, ts, idA, t.TempDir())
	b, keyB := registerChannel(t, ts, idB, t.TempDir())
	if a.Mode != "channel" || !a.Online || keyA == "" {
		t.Fatalf("invalid shared state %+v", a)
	}
	for _, x := range []store.Session{a, b} {
		if c, _ := request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "for " + x.ID}, testToken); c != 202 {
			t.Fatalf("send %d", c)
		}
	}
	code, raw := channelReq(t, ts, "GET", "/api/channels/"+idA+"/commands?after=0", keyA, nil)
	if code != 200 {
		t.Fatalf("poll %d %s", code, raw)
	}
	var commands []channelCommand
	_ = json.Unmarshal(raw, &commands)
	if len(commands) != 1 || commands[0].Method != "notifications/claude/channel" || !bytes.Contains(raw, []byte(a.ID)) || bytes.Contains(raw, []byte(b.ID)) {
		t.Fatalf("crossed queues %s", raw)
	}
	if spawned.Load() != 0 {
		t.Fatal("native send spawned a second Claude")
	}
	if c, _ := channelReq(t, ts, "GET", "/api/channels/"+idA+"/commands?after=0", keyB, nil); c != 409 {
		t.Fatal("cross-session connection key accepted")
	}
	if c, _ := channelReq(t, ts, "GET", "/api/channels/"+idA+"/commands?after=1", keyA, nil); c != 200 {
		t.Fatal("ack failed")
	}
	_, raw = channelReq(t, ts, "GET", "/api/channels/"+idA+"/commands?after=1", keyA, nil)
	if string(bytes.TrimSpace(raw)) != "[]" {
		t.Fatalf("ack replayed %s", raw)
	}
	if c, _ := channelReq(t, ts, "POST", "/api/channels/register", "", map[string]string{"id": idA, "cwd": a.Cwd}); c != 409 {
		t.Fatal("duplicate native process accepted")
	}
	s.mu.Lock()
	s.channels[idA].seen = time.Now().Add(-channelTTL - time.Second)
	s.mu.Unlock()
	if c, _ := request(t, ts, "POST", "/api/sessions/"+a.ID+"/messages", map[string]string{"text": "offline"}, testToken); c != 409 {
		t.Fatal("offline native spawned managed fallback")
	}
	_, newKey := registerChannel(t, ts, idA, a.Cwd)
	if newKey == keyA {
		t.Fatal("reconnect reused key")
	}
	if c, _ := channelReq(t, ts, "POST", "/api/channels/"+idA+"/disconnect", keyA, map[string]any{}); c != 409 {
		t.Fatal("old connection disconnected new one")
	}
	x, _ := st.Get(a.ID)
	if x.Mode != "channel" {
		t.Fatal("shared mode not persisted")
	}
}
func TestChannelsApprovalAndNativeTitle(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	cwd := t.TempDir()
	id := store.ID()
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "native-test")
	_ = os.MkdirAll(dir, 0700)
	user, _ := json.Marshal(map[string]any{"type": "user", "cwd": cwd, "message": map[string]string{"content": "hello from terminal"}})
	native := append(user, []byte("\n{\"type\":\"custom-title\",\"customTitle\":\"原标题\"}\n")...)
	path := filepath.Join(dir, id+".jsonl")
	if e := os.WriteFile(path, native, 0600); e != nil {
		t.Fatal(e)
	}
	x, key := registerChannel(t, ts, id, cwd)
	if x.Title != "原标题" {
		t.Fatalf("lost title %+v", x)
	}
	payload := map[string]string{"request_id": "abcde", "tool_name": "Write", "description": "write test", "input_preview": "test.txt"}
	if c, b := channelReq(t, ts, "POST", "/api/channels/"+id+"/events", key, map[string]any{"kind": "channel_permission", "payload": payload}); c != 200 {
		t.Fatalf("permission %d %s", c, b)
	}
	pending, _ := st.Pending(x.ID)
	if len(pending) != 1 {
		t.Fatal("approval not persisted")
	}
	for i, want := range []int{200, 409} {
		c, _ := request(t, ts, "POST", "/api/requests/"+pending[0].ID, map[string]string{"decision": "allow"}, testToken)
		if c != want {
			t.Fatalf("answer %d got%d want%d", i, c, want)
		}
	}
	_, b := channelReq(t, ts, "GET", "/api/channels/"+id+"/commands?after=0", key, nil)
	if !bytes.Contains(b, []byte("notifications/claude/channel/permission")) || !bytes.Contains(b, []byte("abcde")) {
		t.Fatalf("wrong verdict %s", b)
	}
	for i := 0; i < 2; i++ {
		if c, _ := channelReq(t, ts, "POST", "/api/channels/"+id+"/heartbeat", key, map[string]any{}); c != 200 {
			t.Fatal("heartbeat failed")
		}
	}
	events, _ := st.Events(x.ID, 0)
	if len(events) != 1 || events[0].Text != "hello from terminal" {
		t.Fatalf("history duplicated %+v", events)
	}
	if c, _ := channelReq(t, ts, "POST", "/api/channels/"+id+"/disconnect", key, map[string]any{}); c != 200 {
		t.Fatal("disconnect failed")
	}
	_, b = request(t, ts, "GET", "/api/sessions", nil, testToken)
	if !bytes.Contains(b, []byte(`"online":false`)) {
		t.Fatalf("offline missing %s", b)
	}
}
func TestChannelControlRejectsNonLoopback(t *testing.T) {
	s, _, _ := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	r := httptest.NewRequest("POST", "http://host/api/channels/register", bytes.NewBufferString(fmt.Sprintf(`{"id":%q,"cwd":%q}`, store.ID(), t.TempDir())))
	r.RemoteAddr = "192.168.1.20:9000"
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("external register %d", w.Code)
	}
}

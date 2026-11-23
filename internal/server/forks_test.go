package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

func writeBranchFixture(t *testing.T, s *Server, id, cwd string, messages ...map[string]any) {
	t.Helper()
	var content bytes.Buffer
	for _, message := range messages {
		message["cwd"] = cwd
		if err := json.NewEncoder(&content).Encode(message); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "branch-test")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), content.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestForkSessionBeforeMessageCopiesUploadsAndKeepsSource(t *testing.T) {
	s, ts, st := setup(t, noRun)
	s.cfg.UploadDir = t.TempDir()
	parent := createSession(t, ts)
	if err := st.SetResume(parent.ID); err != nil {
		t.Fatal(err)
	}
	data := tinyPNG(t)
	code, raw := uploadImage(t, ts, parent.ID, testToken, data)
	var uploaded attachment
	if code != 201 || json.Unmarshal(raw, &uploaded) != nil {
		t.Fatalf("upload: %d %s", code, raw)
	}
	prompt, payload, err := s.messageAttachments(i18n.EN, parent.ID, "second question", []string{uploaded.ID})
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	firstID, answerID, secondID := store.ID(), store.ID(), store.ID()
	first := map[string]any{"type": "user", "uuid": firstID, "parentUuid": nil, "timestamp": stamp, "message": map[string]any{"content": "first question"}}
	answer := map[string]any{"type": "assistant", "uuid": answerID, "parentUuid": firstID, "timestamp": stamp, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "first answer"}}}}
	second := map[string]any{"type": "user", "uuid": secondID, "parentUuid": answerID, "timestamp": stamp, "message": map[string]any{"content": prompt}}
	writeBranchFixture(t, s, parent.ClaudeSessionID, parent.Cwd, first, answer, second)
	for _, msg := range []store.Event{{Kind: "user", Text: "first question"}, {Kind: "assistant", Text: "first answer"}, {Kind: "user", Text: "second question", Payload: payload}} {
		if _, err := st.AppendAt(parent.ID, msg.Kind, msg.Text, msg.Payload, stamp); err != nil {
			t.Fatal(err)
		}
	}
	childNative := store.ID()
	writeBranchFixture(t, s, childNative, parent.Cwd, first, answer)
	var calls int
	s.cfg.ForkNativeSession = func(_ context.Context, o claude.Options, boundary string) (string, error) {
		calls++
		if o.SessionID != parent.ClaudeSessionID || boundary != answerID || o.Cwd != parent.Cwd {
			t.Errorf("fork options %+v, boundary %q", o, boundary)
		}
		return childNative, nil
	}
	path := "/api/sessions/" + parent.ID
	code, raw = request(t, ts, http.MethodGet, path+"?tail=10", nil, testToken)
	var source struct {
		Session store.Session
		Events  []store.Event
	}
	if code != 200 || json.Unmarshal(raw, &source) != nil || len(source.Events) != 3 || !source.Session.CanFork || !source.Events[2].Branchable {
		t.Fatalf("source history: %d %s", code, raw)
	}
	if !source.Events[0].EditableInBranch || source.Events[1].EditableInBranch || !source.Events[2].EditableInBranch {
		t.Fatalf("unexpected edit capabilities: %+v", source.Events)
	}
	anchor := source.Events[2]
	req := forkRequest{AnchorSeq: anchor.Seq, AnchorToken: source.Events[0].BranchAnchor, Before: true, RequestID: "fork-test-request", AttachmentIDs: []string{uploaded.ID}}
	if code, raw := request(t, ts, http.MethodPost, path+"/fork", req, testToken); code != 409 || calls != 0 {
		t.Fatalf("accepted stale fork: %d %s", code, raw)
	}
	req.AnchorToken = anchor.BranchAnchor
	code, raw = request(t, ts, http.MethodPost, path+"/fork", req, testToken)
	var result forkResponse
	if code != 201 || json.Unmarshal(raw, &result) != nil || result.Session.ForkedFromID != parent.ID || result.Session.ClaudeSessionID != childNative || len(result.Attachments) != 1 {
		t.Fatalf("fork: %d %s", code, raw)
	}
	if events, err := st.Events(result.Session.ID, 0); err != nil || len(events) != 2 || events[1].Text != "first answer" {
		t.Fatalf("child events: %+v / %v", events, err)
	}
	if events, err := st.Events(parent.ID, 0); err != nil || len(events) != 3 || events[2].Text != "second question" {
		t.Fatalf("source events: %+v / %v", events, err)
	}
	code, raw = request(t, ts, http.MethodPost, path+"/fork", req, testToken)
	var retry forkResponse
	if code != 200 || json.Unmarshal(raw, &retry) != nil || calls != 1 || retry.Session.ID != result.Session.ID || retry.Attachments[0] != result.Attachments[0] {
		t.Fatalf("retry: %d %s (calls %d)", code, raw, calls)
	}
	if code, raw := request(t, ts, http.MethodGet, result.Attachments[0].URL, nil, testToken); code != 200 || !bytes.Equal(raw, data) {
		t.Fatalf("child attachment: %d %s", code, raw)
	}
	if result.Attachments[0].URL == uploaded.URL || result.AttachmentIDs[0] != result.Attachments[0].ID {
		t.Fatalf("attachment is not scoped to the branch: %+v", result)
	}
}

func TestNativeForkBeforeFirstMessageUsesFreshContext(t *testing.T) {
	s, ts, st := setup(t, noRun)
	id := store.ID()
	writeBranchFixture(t, s, id, s.cfg.Cwd, map[string]any{"type": "user", "uuid": store.ID(), "parentUuid": nil, "message": map[string]any{"content": "first question"}})
	path := "/api/native/sessions/" + id
	code, raw := request(t, ts, http.MethodGet, path, nil, testToken)
	var source struct{ Events []store.Event }
	if code != 200 || json.Unmarshal(raw, &source) != nil || len(source.Events) != 1 || !source.Events[0].Branchable {
		t.Fatalf("native history: %d %s", code, raw)
	}
	event := source.Events[0]
	code, raw = request(t, ts, http.MethodPost, path+"/fork", forkRequest{AnchorSeq: event.Seq, AnchorToken: event.BranchAnchor, Before: true}, testToken)
	var result forkResponse
	if code != 201 || json.Unmarshal(raw, &result) != nil || result.Session.CanFork || result.Session.Mode != "managed" || result.Session.ClaudeSessionID == id {
		t.Fatalf("fresh branch: %d %s", code, raw)
	}
	if events, err := st.Events(result.Session.ID, 0); err != nil || len(events) != 0 {
		t.Fatalf("fresh history: %+v / %v", events, err)
	}
}

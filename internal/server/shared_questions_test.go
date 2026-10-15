package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

var sharedQuestion = map[string]any{"questions": []any{map[string]any{"question": "Pick a color", "header": "Color", "options": []any{map[string]any{"label": "Red"}, map[string]any{"label": "Blue"}}}}}

type hookResult struct {
	code int
	body answer
}

// askFromHook starts the long poll a shared terminal's PermissionRequest hook makes.
func askFromHook(t *testing.T, ctx context.Context, ts *httptest.Server, claudeID, kind, tool string, input any) chan hookResult {
	t.Helper()
	done := make(chan hookResult, 1)
	b, _ := json.Marshal(map[string]any{"kind": kind, "toolName": tool, "input": input})
	r, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/channels/"+claudeID+"/interactions", bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer "+testToken)
	go func() {
		res, err := ts.Client().Do(r)
		if err != nil {
			done <- hookResult{code: -1}
			return
		}
		defer res.Body.Close()
		var a answer
		_ = json.NewDecoder(res.Body).Decode(&a)
		done <- hookResult{res.StatusCode, a}
	}()
	return done
}

func waitPending(t *testing.T, st *store.Store, session string, n int) []store.Interaction {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if x, _ := st.Pending(session); len(x) == n {
			return x
		}
	}
	t.Fatalf("pending interactions never reached %d", n)
	return nil
}

func hookAnswer(t *testing.T, done chan hookResult) hookResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("hook long poll did not return")
		return hookResult{}
	}
}

func outcomes(t *testing.T, st *store.Store, session string) []string {
	t.Helper()
	events, _ := st.Events(session, 0)
	var out []string
	for _, e := range events {
		if e.Kind == "interaction" {
			out = append(out, e.Text)
		}
	}
	return out
}

func TestSharedQuestionAnsweredFromWeb(t *testing.T) {
	_, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	x, _ := registerChannel(t, ts, id, t.TempDir())
	done := askFromHook(t, context.Background(), ts, id, "question", "AskUserQuestion", sharedQuestion)
	p := waitPending(t, st, x.ID, 1)[0]
	if p.Kind != "question" || p.ToolName != "AskUserQuestion" || !bytes.Contains(p.Input, []byte("Pick a color")) {
		t.Fatalf("interaction %+v", p)
	}
	if c, b := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "allow"}, testToken); c != 400 {
		t.Fatalf("unanswered question accepted %d %s", c, b)
	}
	if c, b := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "allow", "answers": map[string]string{"Pick a color": "Red, Blue"}}, testToken); c != 200 {
		t.Fatalf("answer %d %s", c, b)
	}
	r := hookAnswer(t, done)
	if r.code != 200 || r.body.Decision != "allow" || r.body.Answers["Pick a color"] != "Red, Blue" {
		t.Fatalf("hook got %+v", r)
	}
	if c, _ := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "allow", "answers": map[string]string{"Pick a color": "Red"}}, testToken); c != 409 {
		t.Fatal("answered twice")
	}
	if got := outcomes(t, st, x.ID); len(got) != 1 || got[0] != "web" {
		t.Fatalf("outcomes %v", got)
	}
}

func TestSharedPlanApproveRejectAndHandoff(t *testing.T) {
	_, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	x, _ := registerChannel(t, ts, id, t.TempDir())
	plan := map[string]any{"plan": "1. write hello.txt", "planFilePath": "p.md"}

	done := askFromHook(t, context.Background(), ts, id, "plan", "ExitPlanMode", plan)
	p := waitPending(t, st, x.ID, 1)[0]
	if c, _ := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "allow", "mode": "plan"}, testToken); c != 400 {
		t.Fatal("invalid mode accepted")
	}
	if c, b := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "allow", "mode": "acceptEdits"}, testToken); c != 200 {
		t.Fatalf("approve %d %s", c, b)
	}
	if r := hookAnswer(t, done); r.body.Decision != "allow" || r.body.Mode != "acceptEdits" {
		t.Fatalf("approve reached hook as %+v", r)
	}

	done = askFromHook(t, context.Background(), ts, id, "plan", "ExitPlanMode", plan)
	p = waitPending(t, st, x.ID, 1)[0]
	if c, _ := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "deny", "message": "split step 1"}, testToken); c != 200 {
		t.Fatal("reject failed")
	}
	if r := hookAnswer(t, done); r.body.Decision != "deny" || r.body.Message != "split step 1" {
		t.Fatalf("reject reached hook as %+v", r)
	}

	done = askFromHook(t, context.Background(), ts, id, "plan", "ExitPlanMode", plan)
	p = waitPending(t, st, x.ID, 1)[0]
	if c, b := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "terminal"}, testToken); c != 200 || !bytes.Contains(b, []byte("terminal")) {
		t.Fatalf("handoff %d %s", c, b)
	}
	if r := hookAnswer(t, done); r.code != 200 || r.body.Decision != "terminal" {
		t.Fatalf("handoff reached hook as %+v", r)
	}
	if got := strings.Join(outcomes(t, st, x.ID), ","); got != "web,web,handoff" {
		t.Fatalf("outcomes %s", got)
	}
}

func TestSharedQuestionWithdrawnByTerminal(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	cwd := t.TempDir()
	// Hook gave up (terminal answered): its request context ends.
	x, key := registerChannel(t, ts, id, cwd)
	ctx, cancel := context.WithCancel(context.Background())
	done := askFromHook(t, ctx, ts, id, "question", "AskUserQuestion", sharedQuestion)
	waitPending(t, st, x.ID, 1)
	cancel()
	<-done
	waitPending(t, st, x.ID, 0)

	// Backstop: the transcript shows the tool result although the hook still waits.
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "shared-test")
	_ = os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, id+".jsonl")
	use := map[string]any{"type": "assistant", "cwd": cwd, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "toolu_1", "name": "AskUserQuestion", "input": sharedQuestion}}}}
	line, _ := json.Marshal(use)
	_ = os.WriteFile(path, append(line, '\n'), 0600)
	done = askFromHook(t, context.Background(), ts, id, "question", "AskUserQuestion", sharedQuestion)
	waitPending(t, st, x.ID, 1)
	result := map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": `"Pick a color"="Red"`}}}}
	line, _ = json.Marshal(result)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.Write(append(line, '\n'))
	f.Close()
	if c, _ := channelReq(t, ts, "POST", "/api/channels/"+id+"/heartbeat", key, map[string]any{}); c != 200 {
		t.Fatal("heartbeat failed")
	}
	if r := hookAnswer(t, done); r.body.Decision != "terminal" {
		t.Fatalf("backstop answered %+v", r)
	}

	// Disconnect releases a waiting hook too.
	done = askFromHook(t, context.Background(), ts, id, "plan", "ExitPlanMode", map[string]any{"plan": "p"})
	waitPending(t, st, x.ID, 1)
	if c, _ := channelReq(t, ts, "POST", "/api/channels/"+id+"/disconnect", key, map[string]any{}); c != 200 {
		t.Fatal("disconnect failed")
	}
	if r := hookAnswer(t, done); r.body.Decision != "terminal" {
		t.Fatalf("disconnect answered %+v", r)
	}
	if got := strings.Join(outcomes(t, st, x.ID), ","); got != "terminal,terminal,expired" {
		t.Fatalf("outcomes %s", got)
	}
}

func TestSharedQuestionEndpointGuards(t *testing.T) {
	s, ts, _ := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	body := map[string]any{"kind": "question", "toolName": "AskUserQuestion", "input": sharedQuestion}
	if c, _ := request(t, ts, "POST", "/api/channels/"+id+"/interactions", body, testToken); c != 409 {
		t.Fatalf("offline terminal accepted %d", c)
	}
	registerChannel(t, ts, id, t.TempDir())
	if c, _ := request(t, ts, "POST", "/api/channels/"+id+"/interactions", body, ""); c != 401 {
		t.Fatal("unauthenticated hook accepted")
	}
	for _, bad := range []map[string]any{
		{"kind": "approval", "toolName": "Bash", "input": map[string]any{}},
		{"kind": "question", "toolName": "ExitPlanMode", "input": map[string]any{}},
		{"kind": "plan", "toolName": "ExitPlanMode", "input": "text"},
	} {
		if c, _ := request(t, ts, "POST", "/api/channels/"+id+"/interactions", bad, testToken); c != 400 {
			t.Fatalf("accepted %v", bad)
		}
	}
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "http://host/api/channels/"+id+"/interactions", bytes.NewReader(raw))
	r.RemoteAddr = "192.168.1.20:9000"
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("remote hook %d", w.Code)
	}
	_, _ = io.Copy(io.Discard, w.Body)
}

func appendTranscript(t *testing.T, path string, v any) {
	t.Helper()
	line, _ := json.Marshal(v)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(append(line, '\n'))
	f.Close()
}

func waitOutcomes(t *testing.T, st *store.Store, session, want string) {
	t.Helper()
	got := ""
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if got = strings.Join(outcomes(t, st, session), ","); got == want {
			return
		}
	}
	ev, _ := st.Events(session, 0)
	for _, e := range ev {
		t.Logf("%s %q %s", e.Kind, e.Text, e.Payload)
	}
	t.Fatalf("outcomes %s, want %s", got, want)
}

func TestSharedQuestionTimeoutExpires(t *testing.T) {
	old := sharedQuestionTimeout
	sharedQuestionTimeout = 100 * time.Millisecond
	t.Cleanup(func() { sharedQuestionTimeout = old })
	_, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	x, _ := registerChannel(t, ts, id, t.TempDir())
	if r := hookAnswer(t, askFromHook(t, context.Background(), ts, id, "question", "AskUserQuestion", sharedQuestion)); r.body.Decision != "terminal" {
		t.Fatalf("timeout answered %+v", r)
	}
	waitPending(t, st, x.ID, 0)
	if got := strings.Join(outcomes(t, st, x.ID), ","); got != "expired" {
		t.Fatalf("outcomes %s", got)
	}
}

func TestSharedAnswerResolvedWhileTimerFires(t *testing.T) {
	old := sharedQuestionTimeout
	sharedQuestionTimeout = 150 * time.Millisecond
	t.Cleanup(func() { sharedQuestionTimeout = old })
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	x, _ := registerChannel(t, ts, id, t.TempDir())
	done := askFromHook(t, context.Background(), ts, id, "question", "AskUserQuestion", sharedQuestion)
	p := waitPending(t, st, x.ID, 1)[0]
	// Hold the lock past the deadline so the handler waits for it after its timer fired,
	// then resolve like the web does.
	s.mu.Lock()
	time.Sleep(400 * time.Millisecond)
	q := s.requests[p.ID]
	delete(s.requests, p.ID)
	_ = st.Resolve(p.ID, "resolved")
	q.channel <- answer{Decision: "allow", Answers: map[string]string{"Pick a color": "Red"}}
	s.mu.Unlock()
	if r := hookAnswer(t, done); r.body.Decision != "allow" || r.body.Answers["Pick a color"] != "Red" {
		t.Fatalf("resolved answer dropped: %+v", r)
	}
}

func TestSharedAnswerChecks(t *testing.T) {
	_, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	x, _ := registerChannel(t, ts, id, t.TempDir())
	done := askFromHook(t, context.Background(), ts, id, "question", "AskUserQuestion", sharedQuestion)
	p := waitPending(t, st, x.ID, 1)[0]
	if c, _ := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "allow", "answers": map[string]string{"Pick a color": strings.Repeat("a", 64<<10+1)}}, testToken); c != 400 {
		t.Fatalf("oversized answer accepted %d", c)
	}
	if c, b := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "allow", "answers": map[string]string{"Pick a color": "Red", "Injected": "x"}}, testToken); c != 200 {
		t.Fatalf("answer %d %s", c, b)
	}
	r := hookAnswer(t, done)
	if len(r.body.Answers) != 1 || r.body.Answers["Pick a color"] != "Red" {
		t.Fatalf("hook got %+v", r.body.Answers)
	}
}

func TestSharedQuestionSurvivesBridgeReregister(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	cwd := t.TempDir()
	x, _ := registerChannel(t, ts, id, cwd)
	done := askFromHook(t, context.Background(), ts, id, "question", "AskUserQuestion", sharedQuestion)
	p := waitPending(t, st, x.ID, 1)[0]
	// A hook that already gave up still expires with the old connection.
	ctx, cancel := context.WithCancel(context.Background())
	gone := askFromHook(t, ctx, ts, id, "plan", "ExitPlanMode", map[string]any{"plan": "p"})
	waitPending(t, st, x.ID, 2)
	s.mu.Lock()
	s.channels[id].seen = time.Now().Add(-time.Hour) // missed heartbeats
	s.mu.Unlock()
	cancel()
	<-gone
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		s.mu.Lock()
		n := len(s.requests)
		s.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("canceled hook still pending")
		}
	}
	registerChannel(t, ts, id, cwd)
	if got := waitPending(t, st, x.ID, 1); got[0].ID != p.ID {
		t.Fatalf("pending %+v", got)
	}
	if c, b := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "allow", "answers": map[string]string{"Pick a color": "Blue"}}, testToken); c != 200 {
		t.Fatalf("answer after re-register %d %s", c, b)
	}
	if r := hookAnswer(t, done); r.body.Decision != "allow" || r.body.Answers["Pick a color"] != "Blue" {
		t.Fatalf("hook got %+v", r)
	}
}

func TestSharedWebAnswerOverriddenByTerminal(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	cwd := t.TempDir()
	x, _ := registerChannel(t, ts, id, cwd)
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "shared-test")
	_ = os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, id+".jsonl")
	ask := func(useID, web, result string, want string) {
		t.Helper()
		appendTranscript(t, path, map[string]any{"type": "assistant", "cwd": cwd, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": useID, "name": "AskUserQuestion", "input": sharedQuestion}}}})
		done := askFromHook(t, context.Background(), ts, id, "question", "AskUserQuestion", sharedQuestion)
		p := waitPending(t, st, x.ID, 1)[0]
		if c, b := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "allow", "answers": map[string]string{"Pick a color": web}}, testToken); c != 200 {
			t.Fatalf("answer %d %s", c, b)
		}
		hookAnswer(t, done)
		// Claude records the answer that actually won.
		appendTranscript(t, path, map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": useID, "content": `User has answered your questions: "Pick a color"="` + result + `".`}}}})
		waitOutcomes(t, st, x.ID, want)
	}
	ask("toolu_1", "Red", "Red", "web")
	time.Sleep(2 * time.Second) // the correction check runs 1.5 s after the answer
	if got := strings.Join(outcomes(t, st, x.ID), ","); got != "web" {
		t.Fatalf("matching answer corrected: %s", got)
	}
	ask("toolu_2", "Red", "Blue", "web,web,terminal")
}

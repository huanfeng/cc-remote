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

// connectTerminal records a terminal session bound to Claude session claudeID
// with the connection a running terminal registers, without starting Claude Code.
func connectTerminal(t *testing.T, s *Server, st *store.Store, claudeID, cwd string) (store.Session, *channelConnection) {
	t.Helper()
	x, err := st.Create("Terminal", cwd, claudeID, false)
	if err == nil {
		err = st.SetMode(x.ID, "terminal")
	}
	if err != nil {
		t.Fatal(err)
	}
	c := &channelConnection{sessionID: x.ID, seen: time.Now(), keyPrefix: claudeID + ":"}
	s.mu.Lock()
	s.channels[claudeID] = c
	s.mu.Unlock()
	return x, c
}

// syncConnection applies the transcript as the terminal session's sync loop does.
func syncConnection(t *testing.T, s *Server, claudeID string) {
	t.Helper()
	n, events, err := claude.NativeHistory(s.cfg.ClaudeConfigDir, claudeID)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyTranscriptLocked(s.channels[claudeID], n, events)
}

// askFromHook starts the long poll a terminal session's PermissionRequest hook makes.
func askFromHook(t *testing.T, ctx context.Context, ts *httptest.Server, claudeID, kind, tool string, input any, agent ...string) chan hookResult {
	t.Helper()
	done := make(chan hookResult, 1)
	fields := map[string]any{"kind": kind, "toolName": tool, "input": input}
	if len(agent) > 0 {
		fields["agentId"] = agent[0]
	}
	b, _ := json.Marshal(fields)
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
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	x, _ := connectTerminal(t, s, st, id, t.TempDir())
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
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	x, _ := connectTerminal(t, s, st, id, t.TempDir())
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
	x, conn := connectTerminal(t, s, st, id, cwd)
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
	syncConnection(t, s, id)
	if r := hookAnswer(t, done); r.body.Decision != "terminal" {
		t.Fatalf("backstop answered %+v", r)
	}

	// Claude Code exiting releases a waiting hook too.
	done = askFromHook(t, context.Background(), ts, id, "plan", "ExitPlanMode", map[string]any{"plan": "p"})
	waitPending(t, st, x.ID, 1)
	s.terminalExited(&terminalSession{id: x.ID, claudeID: id, conn: conn, closing: true, dir: t.TempDir()})
	if r := hookAnswer(t, done); r.body.Decision != "terminal" {
		t.Fatalf("disconnect answered %+v", r)
	}
	if got := strings.Join(outcomes(t, st, x.ID), ","); got != "terminal,terminal,expired" {
		t.Fatalf("outcomes %s", got)
	}
}

func TestSharedQuestionEndpointGuards(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	body := map[string]any{"kind": "question", "toolName": "AskUserQuestion", "input": sharedQuestion}
	if c, _ := request(t, ts, "POST", "/api/channels/"+id+"/interactions", body, testToken); c != 409 {
		t.Fatalf("offline terminal accepted %d", c)
	}
	connectTerminal(t, s, st, id, t.TempDir())
	if c, _ := request(t, ts, "POST", "/api/channels/"+id+"/interactions", body, ""); c != 401 {
		t.Fatal("unauthenticated hook accepted")
	}
	for _, bad := range []map[string]any{
		{"kind": "approval", "toolName": "AskUserQuestion", "input": sharedQuestion},
		{"kind": "approval", "toolName": "", "input": map[string]any{}},
		{"kind": "question", "toolName": "Bash", "input": map[string]any{}},
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
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	x, _ := connectTerminal(t, s, st, id, t.TempDir())
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
	x, _ := connectTerminal(t, s, st, id, t.TempDir())
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
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	x, _ := connectTerminal(t, s, st, id, t.TempDir())
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

func TestSharedWebAnswerOverriddenByTerminal(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	cwd := t.TempDir()
	x, _ := connectTerminal(t, s, st, id, cwd)
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

func TestSharedApprovalFromWebAndTerminal(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	cwd := t.TempDir()
	x, _ := connectTerminal(t, s, st, id, cwd)
	bash := map[string]any{"command": "rm -rf build", "description": "Clean"}

	_ = st.SetStatus(x.ID, "waiting")
	done := askFromHook(t, context.Background(), ts, id, "approval", "Bash", bash)
	p := waitPending(t, st, x.ID, 1)[0]
	if p.Kind != "approval" || p.ToolName != "Bash" {
		t.Fatalf("interaction %+v", p)
	}
	if c, b := request(t, ts, "POST", "/api/requests/"+p.ID, map[string]any{"decision": "allow", "answers": map[string]string{"x": "y"}, "mode": "bypassPermissions"}, testToken); c != 200 {
		t.Fatalf("allow %d %s", c, b)
	}
	if r := hookAnswer(t, done); r.body.Decision != "allow" || r.body.Answers != nil || r.body.Mode != "" {
		t.Fatalf("hook got %+v", r)
	}
	if got, _ := st.Get(x.ID); got.Status != "running" {
		t.Fatalf("status after the web answer %s", got.Status)
	}

	// The terminal answers: only the result of the same call withdraws the web card.
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "shared-test")
	_ = os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, id+".jsonl")
	use := func(tid string, input map[string]any) map[string]any {
		return map[string]any{"type": "assistant", "cwd": cwd, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": tid, "name": "Bash", "input": input}}}}
	}
	result := func(tid string) map[string]any {
		return map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": tid, "content": "ok"}}}}
	}
	appendTranscript(t, path, use("toolu_a", map[string]any{"command": "ls"}))
	appendTranscript(t, path, use("toolu_b", bash))
	done = askFromHook(t, context.Background(), ts, id, "approval", "Bash", bash)
	waitPending(t, st, x.ID, 1)
	appendTranscript(t, path, result("toolu_a"))
	syncConnection(t, s, id)
	if pending, _ := st.Pending(x.ID); len(pending) != 1 {
		t.Fatal("another call's result withdrew the approval")
	}
	appendTranscript(t, path, result("toolu_b"))
	syncConnection(t, s, id)
	if r := hookAnswer(t, done); r.body.Decision != "terminal" {
		t.Fatalf("terminal answer reached hook as %+v", r)
	}
	waitOutcomes(t, st, x.ID, "web,terminal")
}

// A transcript result ends only the call an approval asks about: not an
// identical call still running, nor a subagent's identical call. Answering one
// approval keeps the session waiting while another is open.
func TestSharedApprovalBackstopMatchesItsCall(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	cwd := t.TempDir()
	x, _ := connectTerminal(t, s, st, id, cwd)
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "shared-test")
	_ = os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, id+".jsonl")
	test := map[string]any{"command": "npm test"}
	use := func(tid string) map[string]any {
		return map[string]any{"type": "assistant", "cwd": cwd, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": tid, "name": "Bash", "input": test}}}}
	}
	result := func(tid string) map[string]any {
		return map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": tid, "content": "ok"}}}}
	}

	// npm test #1 was approved and still runs when the identical #2 asks.
	appendTranscript(t, path, use("toolu_1"))
	appendTranscript(t, path, use("toolu_2"))
	second := askFromHook(t, context.Background(), ts, id, "approval", "Bash", test)
	waitPending(t, st, x.ID, 1)
	// A subagent asks for the same command; its call is not in this transcript.
	agent := askFromHook(t, context.Background(), ts, id, "approval", "Bash", test, "a1b2c3")
	waitPending(t, st, x.ID, 2)
	appendTranscript(t, path, result("toolu_1"))
	syncConnection(t, s, id)
	if pending, _ := st.Pending(x.ID); len(pending) != 2 {
		t.Fatalf("an identical call's result withdrew %d approvals", 2-len(pending))
	}

	_ = st.SetStatus(x.ID, "waiting")
	pending, _ := st.Pending(x.ID)
	if c, b := request(t, ts, "POST", "/api/requests/"+pending[0].ID, map[string]any{"decision": "allow"}, testToken); c != 200 {
		t.Fatalf("allow %d %s", c, b)
	}
	var r hookResult
	other := agent
	select {
	case r = <-second:
	case r = <-agent:
		other = second
	case <-time.After(5 * time.Second):
		t.Fatal("no hook got the web answer")
	}
	if r.body.Decision != "allow" {
		t.Fatalf("hook got %+v", r)
	}
	if got, _ := st.Get(x.ID); got.Status != "waiting" {
		t.Fatalf("status %s while another approval waits", got.Status)
	}
	pending, _ = st.Pending(x.ID)
	if c, b := request(t, ts, "POST", "/api/requests/"+pending[0].ID, map[string]any{"decision": "deny"}, testToken); c != 200 {
		t.Fatalf("deny %d %s", c, b)
	}
	if r := hookAnswer(t, other); r.body.Decision != "deny" {
		t.Fatalf("hook got %+v", r)
	}
	if got, _ := st.Get(x.ID); got.Status != "running" {
		t.Fatalf("status %s after the last approval", got.Status)
	}
}

// The one open call an approval asks about: its result withdraws the card and,
// with no other dialog open, ends "waiting". A subagent's identical approval is
// not this transcript's call and stays.
func TestSharedApprovalBackstopEndsItsCall(t *testing.T) {
	s, ts, st := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	id := store.ID()
	cwd := t.TempDir()
	x, _ := connectTerminal(t, s, st, id, cwd)
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "shared-test")
	_ = os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, id+".jsonl")
	build := map[string]any{"command": "npm run build"}
	appendTranscript(t, path, map[string]any{"type": "assistant", "cwd": cwd, "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": "toolu_b", "name": "Bash", "input": build}}}})

	agent := askFromHook(t, context.Background(), ts, id, "approval", "Bash", build, "a1b2c3")
	waitPending(t, st, x.ID, 1)
	main := askFromHook(t, context.Background(), ts, id, "approval", "Bash", build)
	waitPending(t, st, x.ID, 2)
	_ = st.SetStatus(x.ID, "waiting")
	appendTranscript(t, path, map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_b", "content": "ok"}}}})
	syncConnection(t, s, id)
	if r := hookAnswer(t, main); r.body.Decision != "terminal" {
		t.Fatalf("the asked call's result reached the hook as %+v", r)
	}
	if pending, _ := st.Pending(x.ID); len(pending) != 1 {
		t.Fatalf("%d approvals open; the subagent's should stay", len(pending))
	}
	if got, _ := st.Get(x.ID); got.Status != "waiting" {
		t.Fatalf("status %s while the subagent's dialog is open", got.Status)
	}
	pending, _ := st.Pending(x.ID)
	if c, b := request(t, ts, "POST", "/api/requests/"+pending[0].ID, map[string]any{"decision": "terminal"}, testToken); c != 200 {
		t.Fatalf("handoff %d %s", c, b)
	}
	if r := hookAnswer(t, agent); r.body.Decision != "terminal" {
		t.Fatalf("hook got %+v", r)
	}
	if got, _ := st.Get(x.ID); got.Status != "waiting" {
		t.Fatalf("status %s after a handoff; the dialog is still open in the terminal", got.Status)
	}
}

package channel

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

	"cc-remote/internal/hostlock"
)

const askSession = "abcd1234-1234-1234-1234-123456789abc"

var askQuestion = map[string]any{"questions": []any{map[string]any{"question": "Pick a color", "header": "Color", "options": []any{map[string]any{"label": "Red"}, map[string]any{"label": "Blue"}}, "multiSelect": false}}}

func askFixture(t *testing.T, host string) (cfgPath, binding string) {
	t.Helper()
	dir := t.TempDir()
	binding = filepath.Join(dir, "session-changed")
	data, _ := json.Marshal(askConfig{Host: host, Token: "tok", SessionID: askSession, BindingFile: binding})
	cfgPath = filepath.Join(dir, "ask.json")
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, binding
}

func hookInput(t *testing.T, session, tool string, input any, transcript string) io.Reader {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"session_id": session, "transcript_path": transcript, "hook_event_name": "PermissionRequest", "tool_name": tool, "tool_input": input})
	return bytes.NewReader(b)
}

func toolLine(blocks ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"content": blocks}})
	return string(b) + "\n"
}

func decisionOf(t *testing.T, out []byte) map[string]any {
	t.Helper()
	var v struct {
		Hook struct {
			Event    string         `json:"hookEventName"`
			Decision map[string]any `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &v); err != nil || v.Hook.Event != "PermissionRequest" {
		t.Fatalf("invalid hook output %q: %v", out, err)
	}
	return v.Hook.Decision
}

func TestAskHookAnswersQuestionFromWeb(t *testing.T) {
	var got struct {
		Kind, ToolName string
		Input          map[string]any
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channels/"+askSession+"/interactions" || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"decision":"allow","answers":{"Pick a color":"Blue"}}`))
	}))
	defer ts.Close()
	cfg, _ := askFixture(t, ts.URL)
	var out bytes.Buffer
	if err := askHook(context.Background(), hookInput(t, askSession, "AskUserQuestion", askQuestion, ""), &out, cfg, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "question" || got.ToolName != "AskUserQuestion" || got.Input["questions"] == nil {
		t.Fatalf("host request %+v", got)
	}
	d := decisionOf(t, out.Bytes())
	updated, _ := d["updatedInput"].(map[string]any)
	answers, _ := updated["answers"].(map[string]any)
	if d["behavior"] != "allow" || updated["questions"] == nil || answers["Pick a color"] != "Blue" {
		t.Fatalf("decision %+v", d)
	}
}

func TestAskDecisionMapping(t *testing.T) {
	multi := askDecision("question", askQuestion, webAnswer{Decision: "allow", Answers: map[string]string{"Pick a color": "Red, Blue"}})
	if multi["updatedInput"].(map[string]any)["answers"].(map[string]string)["Pick a color"] != "Red, Blue" {
		t.Fatalf("multi %+v", multi)
	}
	free := askDecision("question", askQuestion, webAnswer{Decision: "allow", Answers: map[string]string{"Pick a color": "Green please"}})
	if free["updatedInput"].(map[string]any)["answers"].(map[string]string)["Pick a color"] != "Green please" {
		t.Fatalf("free text %+v", free)
	}
	if _, leaked := askQuestion["answers"]; leaked {
		t.Fatal("decision mutated the original input")
	}
	if askDecision("question", askQuestion, webAnswer{Decision: "allow"}) != nil {
		t.Fatal("allow without answers must not decide")
	}
	plan := map[string]any{"plan": "do it", "planFilePath": "p.md"}
	ok := askDecision("plan", plan, webAnswer{Decision: "allow", Mode: "acceptEdits"})
	perms, _ := ok["updatedPermissions"].([]any)
	if ok["behavior"] != "allow" || ok["updatedInput"].(map[string]any)["plan"] != "do it" || len(perms) != 1 || perms[0].(map[string]any)["mode"] != "acceptEdits" || perms[0].(map[string]any)["destination"] != "session" {
		t.Fatalf("plan approve %+v", ok)
	}
	if m := askDecision("plan", plan, webAnswer{Decision: "allow", Mode: "plan"})["updatedPermissions"].([]any)[0].(map[string]any)["mode"]; m != "default" {
		t.Fatalf("unknown mode not defaulted: %v", m)
	}
	deny := askDecision("plan", plan, webAnswer{Decision: "deny", Message: "split step 2"})
	if deny["behavior"] != "deny" || deny["message"] != "split step 2" || deny["updatedInput"] != nil {
		t.Fatalf("plan deny %+v", deny)
	}
	if m, _ := askDecision("plan", plan, webAnswer{Decision: "deny"})["message"].(string); !strings.Contains(m, "Keep planning") {
		t.Fatalf("default feedback %q", m)
	}
	if askDecision("plan", plan, webAnswer{Decision: "terminal"}) != nil {
		t.Fatal("terminal handoff must not decide")
	}
}

func TestAskHookFailsOpen(t *testing.T) {
	calls := 0
	status := 409
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(status)
		if status == 200 {
			w.Write([]byte(`{"decision":"terminal"}`))
		}
	}))
	defer ts.Close()
	cfg, binding := askFixture(t, ts.URL)
	run := func(session, tool string, path string) (string, error) {
		var out bytes.Buffer
		err := askHook(context.Background(), hookInput(t, session, tool, askQuestion, ""), &out, path, 10*time.Millisecond)
		return out.String(), err
	}
	if out, _ := run("ffffffff-1234-1234-1234-123456789abc", "AskUserQuestion", cfg); out != "" || calls != 0 {
		t.Fatalf("other session answered: %q calls=%d", out, calls)
	}
	if out, _ := run(askSession, "Bash", cfg); out != "" || calls != 0 {
		t.Fatal("non-interactive tool handled")
	}
	if out, err := run(askSession, "AskUserQuestion", cfg); out != "" || err == nil || calls != 1 {
		t.Fatalf("host error: %q %v", out, err)
	}
	status = 200
	if out, err := run(askSession, "AskUserQuestion", cfg); out != "" || err != nil {
		t.Fatalf("handoff produced a decision: %q %v", out, err)
	}
	if out, err := run(askSession, "AskUserQuestion", filepath.Join(t.TempDir(), "missing.json")); out != "" || err == nil {
		t.Fatal("missing config must fail open with an error")
	}
	_ = os.WriteFile(binding, []byte("other"), 0600)
	calls = 0
	if out, _ := run(askSession, "AskUserQuestion", cfg); out != "" || calls != 0 {
		t.Fatal("changed binding still asked the web")
	}
}

func TestAskHookStopsWhenTerminalAnswers(t *testing.T) {
	canceled := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // the server watches for disconnects once the body is read
		<-r.Context().Done()
		close(canceled)
	}))
	defer ts.Close()
	cfg, _ := askFixture(t, ts.URL)
	transcript := filepath.Join(t.TempDir(), askSession+".jsonl")
	// An earlier identical question was already answered; it must not end this one.
	old := toolLine(map[string]any{"type": "tool_use", "id": "toolu_old", "name": "AskUserQuestion", "input": askQuestion}) +
		toolLine(map[string]any{"type": "tool_result", "tool_use_id": "toolu_old", "content": "answered"})
	current := toolLine(map[string]any{"type": "tool_use", "id": "toolu_new", "name": "AskUserQuestion", "input": askQuestion})
	if err := os.WriteFile(transcript, []byte(old+current), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan string)
	go func() {
		var out bytes.Buffer
		_ = askHook(context.Background(), hookInput(t, askSession, "AskUserQuestion", askQuestion, transcript), &out, cfg, 20*time.Millisecond)
		done <- out.String()
	}()
	select {
	case <-done:
		t.Fatal("hook ended before the terminal answered")
	case <-time.After(200 * time.Millisecond):
	}
	f, _ := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString(toolLine(map[string]any{"type": "tool_result", "tool_use_id": "toolu_new", "content": "answered in terminal"}))
	f.Close()
	select {
	case out := <-done:
		if out != "" {
			t.Fatalf("terminal answer still printed %q", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hook kept waiting after the terminal answered")
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("host long poll not canceled")
	}
}

func TestHookSettingsRegistersAskHook(t *testing.T) {
	var v struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
				Timeout int      `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(hookSettings("/bin/ccr"), &v); err != nil {
		t.Fatal(err)
	}
	ask := v.Hooks["PermissionRequest"]
	if len(ask) != 1 || ask[0].Matcher != "AskUserQuestion|ExitPlanMode" || ask[0].Hooks[0].Command != "/bin/ccr" || ask[0].Hooks[0].Args[0] != "channel-ask-hook" || ask[0].Hooks[0].Timeout != 1800 {
		t.Fatalf("ask hook %+v", ask)
	}
	if s := v.Hooks["SessionStart"]; len(s) != 1 || s[0].Hooks[0].Args[0] != "channel-session-hook" {
		t.Fatalf("session hook %+v", s)
	}
}

func collectLines(t *testing.T, path string, offset int64, partial []byte) (int64, []byte, []string, bool) {
	t.Helper()
	var lines []string
	offset, partial, ok := readTranscript(path, offset, partial, func(line []byte) { lines = append(lines, string(line)) })
	return offset, partial, lines, ok
}

func TestReadTranscriptStartsAtTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	filler := strings.Repeat("x", 999) + "\n"
	var b strings.Builder
	for b.Len() < 3*transcriptTail {
		b.WriteString(filler)
	}
	b.WriteString("last\n")
	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	offset, partial, lines, ok := collectLines(t, path, 0, nil)
	if !ok || offset != int64(b.Len()) || len(partial) != 0 {
		t.Fatalf("first read ok=%v offset=%d partial=%d", ok, offset, len(partial))
	}
	total := 0
	for _, l := range lines {
		if l != strings.TrimSuffix(filler, "\n") && l != "last" {
			t.Fatalf("partial or foreign line %q", l[:min(len(l), 20)])
		}
		total += len(l) + 1
	}
	if total > transcriptTail || lines[len(lines)-1] != "last" {
		t.Fatalf("read %d bytes, last %q", total, lines[len(lines)-1])
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString("next\npart")
	f.Close()
	offset, partial, lines, ok = collectLines(t, path, offset, partial)
	if !ok || len(lines) != 1 || lines[0] != "next" || string(partial) != "part" {
		t.Fatalf("incremental read %v %q", lines, partial)
	}
	// A rewritten (shorter) file is read again from the start.
	if err := os.WriteFile(path, []byte("fresh\n"), 0600); err != nil {
		t.Fatal(err)
	}
	offset, partial, lines, ok = collectLines(t, path, offset, partial)
	if !ok || offset != 6 || len(partial) != 0 || len(lines) != 1 || lines[0] != "fresh" {
		t.Fatalf("after truncation offset=%d partial=%q lines=%v", offset, partial, lines)
	}
	if _, _, _, ok = collectLines(t, filepath.Join(t.TempDir(), "missing"), 0, nil); ok {
		t.Fatal("missing transcript reported as read")
	}
}

func TestWatchTranscriptFirstLookNeedsSuccessfulRead(t *testing.T) {
	// The transcript appears only after the hook started; it already holds an
	// answered identical question, which must not count as this call's answer.
	path := filepath.Join(t.TempDir(), askSession+".jsonl")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	go func() {
		time.Sleep(60 * time.Millisecond)
		old := toolLine(map[string]any{"type": "tool_use", "id": "toolu_old", "name": "AskUserQuestion", "input": askQuestion}) +
			toolLine(map[string]any{"type": "tool_result", "tool_use_id": "toolu_old", "content": "answered"})
		_ = os.WriteFile(path, []byte(old), 0600)
	}()
	if watchTranscript(ctx, path, "AskUserQuestion", askQuestion, "", 10*time.Millisecond) {
		t.Fatal("an earlier answered question ended the hook")
	}
}

func TestCleanStaleConfigs(t *testing.T) {
	data := t.TempDir()
	own, busy, gone := askSession, "bbbbbbbb-1234-1234-1234-123456789abc", "cccccccc-1234-1234-1234-123456789abc"
	mk := func(name, session string, age time.Duration) string {
		dir := filepath.Join(data, "channel-config", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(dir, session+".json"), []byte("{}"), 0600)
		old := time.Now().Add(-age)
		_ = os.Chtimes(dir, old, old)
		return dir
	}
	ownOld := mk("session-1", own, 48*time.Hour)
	busyOld := mk("session-2", busy, 48*time.Hour)
	goneOld := mk("session-3", gone, 48*time.Hour)
	fresh := mk("session-4", gone, time.Minute)
	lockDir := filepath.Join(data, "channel-locks", busy)
	_ = os.MkdirAll(lockDir, 0700)
	release, err := hostlock.Acquire(lockDir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	cleanStaleConfigs(data, own, 24*time.Hour)
	for dir, want := range map[string]bool{ownOld: false, busyOld: true, goneOld: false, fresh: true} {
		if got := fileExists(dir); got != want {
			t.Errorf("%s exists=%v, want %v", filepath.Base(dir), got, want)
		}
	}
}

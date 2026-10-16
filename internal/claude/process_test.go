package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// streamChild imitates "claude -p --input-format stream-json": one result per
// user message, "hang" waits for an interrupt, "bg" is followed by a turn the
// child starts itself (origin set), "crash" exits mid-session.
func streamChild() {
	out := json.NewEncoder(os.Stdout)
	_ = out.Encode(map[string]any{"type": "system", "subtype": "init", "session_id": testID})
	in := bufio.NewScanner(os.Stdin)
	result := func(extra map[string]any) {
		v := map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": "done"}
		for k, x := range extra {
			v[k] = x
		}
		_ = out.Encode(v)
	}
	for in.Scan() {
		var msg struct {
			Type    string `json:"type"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		_ = json.Unmarshal(in.Bytes(), &msg)
		if msg.Type != "user" {
			continue
		}
		text := msg.Message.Content
		_ = out.Encode(map[string]any{"type": "user", "isReplay": true, "message": map[string]any{"role": "user", "content": text}})
		switch text {
		case "crash":
			os.Exit(3)
		case "fail":
			result(map[string]any{"subtype": "error_during_execution", "is_error": true, "errors": []string{"boom"}})
		case "hang":
			for in.Scan() {
				if strings.Contains(in.Text(), `"interrupt"`) {
					_ = out.Encode(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success"}})
					result(map[string]any{"subtype": "error_during_execution", "is_error": true, "errors": []string{"interrupted"}})
					break
				}
			}
		case "bg":
			_ = out.Encode(map[string]any{"type": "system", "subtype": "task_started", "task_id": "t1"})
			result(nil)
			_ = out.Encode(map[string]any{"type": "system", "subtype": "task_notification", "task_id": "t1", "status": "completed"})
			_ = out.Encode(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "background finished"}}}})
			result(map[string]any{"origin": map[string]any{"kind": "task-notification"}})
		default:
			_ = out.Encode(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "echo " + text}}}})
			result(nil)
		}
	}
	os.Exit(0)
}

type turnResult struct {
	err    error
	origin bool
}

type processLog struct {
	events  chan Event
	replays chan string
	results chan turnResult
	exit    chan error
}

func startTestProcess(t *testing.T) (*Process, processLog) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_REMOTE_TEST_CHILD", "stream")
	l := processLog{make(chan Event, 64), make(chan string, 8), make(chan turnResult, 8), make(chan error, 1)}
	p, err := StartProcess(Options{Executable: exe, HostExecutable: exe, SessionID: testID, Cwd: t.TempDir(), BridgeURL: "http://127.0.0.1:1/api/bridge/request", BridgeToken: "secret"}, ProcessHooks{
		Event:  func(e Event) error { l.events <- e; return nil },
		Replay: func(c string) { l.replays <- c },
		Result: func(err error, origin bool) { l.results <- turnResult{err, origin} },
		Exit:   func(err error) { l.exit <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	return p, l
}

func wait[T any](t *testing.T, c chan T, what string) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
	var zero T
	return zero
}

func TestProcessReplaysResultsAndInterrupt(t *testing.T) {
	p, l := startTestProcess(t)
	if err := p.Send("hello"); err != nil {
		t.Fatal(err)
	}
	if got := wait(t, l.replays, "replay"); got != "hello" {
		t.Fatalf("replay %q", got)
	}
	if r := wait(t, l.results, "first result"); r.err != nil || r.origin {
		t.Fatalf("first result %+v", r)
	}
	found := false
	for len(l.events) > 0 {
		if e := <-l.events; e.Kind == "assistant" && e.Text == "echo hello" {
			found = true
		}
	}
	if !found {
		t.Fatal("assistant event missing")
	}
	_ = p.Send("fail")
	wait(t, l.replays, "fail replay")
	if r := wait(t, l.results, "failed result"); r.err == nil || !strings.Contains(r.err.Error(), "boom") {
		t.Fatalf("failed turn: %+v", r)
	}
	_ = p.Send("hang")
	wait(t, l.replays, "hang replay")
	if err := p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if r := wait(t, l.results, "interrupted result"); r.err == nil {
		t.Fatal("interrupted turn reported success")
	}
	_ = p.Send("bg")
	wait(t, l.replays, "bg replay")
	if r := wait(t, l.results, "bg result"); r.origin {
		t.Fatal("our turn reported an origin")
	}
	if r := wait(t, l.results, "self-started result"); !r.origin {
		t.Fatal("self-started turn without origin")
	}
	if n := p.BackgroundTasks(); n != 0 {
		t.Fatalf("background tasks %d", n)
	}
	p.Close()
	if err := wait(t, l.exit, "exit"); err != nil {
		t.Fatalf("clean close reported %v", err)
	}
	if err := p.Send("late"); err == nil {
		t.Fatal("send after close succeeded")
	}
}

func TestProcessCrashReportsExit(t *testing.T) {
	p, l := startTestProcess(t)
	_ = p.Send("crash")
	if err := wait(t, l.exit, "exit"); err == nil {
		t.Fatal("crash not reported")
	}
	<-p.Done()
}

func TestProcessArgsUseStreamInput(t *testing.T) {
	args := strings.Join(processArgs(Options{SessionID: testID, Resume: true}, "s.json", "m.json"), " ")
	for _, want := range []string{"-p", "--input-format stream-json", "--output-format stream-json", "--resume " + testID} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q missing %q", args, want)
		}
	}
}

// A task ends on its notification or on an update to a final status.
func TestProcessTracksTaskEnds(t *testing.T) {
	p := &Process{tasks: map[string]bool{}}
	meta := func(line string) streamMeta {
		var m streamMeta
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	p.trackTasks(meta(`{"type":"system","subtype":"task_started","task_id":"a"}`))
	p.trackTasks(meta(`{"type":"system","subtype":"task_started","task_id":"b"}`))
	p.trackTasks(meta(`{"type":"system","subtype":"task_updated","task_id":"a","patch":{"status":"running"}}`))
	if !p.HasTask("a") || p.BackgroundTasks() != 2 {
		t.Fatal("running update ended a task")
	}
	p.trackTasks(meta(`{"type":"system","subtype":"task_updated","task_id":"a","patch":{"status":"killed"}}`))
	p.trackTasks(meta(`{"type":"system","subtype":"task_notification","task_id":"b","status":"completed"}`))
	if p.HasTask("a") || p.HasTask("b") || p.BackgroundTasks() != 0 {
		t.Fatal("ended tasks still tracked")
	}
}

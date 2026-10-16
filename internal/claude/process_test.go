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

type processLog struct {
	events chan Event
	turns  chan error
	self   chan bool
	exit   chan error
}

func startTestProcess(t *testing.T) (*Process, processLog) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_REMOTE_TEST_CHILD", "stream")
	l := processLog{make(chan Event, 64), make(chan error, 8), make(chan bool, 8), make(chan error, 1)}
	p, err := StartProcess(Options{Executable: exe, HostExecutable: exe, SessionID: testID, Cwd: t.TempDir(), BridgeURL: "http://127.0.0.1:1/api/bridge/request", BridgeToken: "secret"}, ProcessHooks{
		Event:    func(e Event) error { l.events <- e; return nil },
		TurnDone: func(err error) { l.turns <- err },
		SelfTurn: func(b bool) { l.self <- b },
		Exit:     func(err error) { l.exit <- err },
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

func TestProcessTurnsInterruptAndSelfTurns(t *testing.T) {
	p, l := startTestProcess(t)
	if err := p.Send("hello"); err != nil {
		t.Fatal(err)
	}
	if err := wait(t, l.turns, "first turn"); err != nil {
		t.Fatalf("first turn: %v", err)
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
	// Two messages at once are answered in order, one TurnDone each.
	_ = p.Send("one")
	_ = p.Send("fail")
	if err := wait(t, l.turns, "queued 1"); err != nil {
		t.Fatalf("queued 1: %v", err)
	}
	if err := wait(t, l.turns, "queued 2"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("failed turn: %v", err)
	}
	_ = p.Send("hang")
	time.Sleep(100 * time.Millisecond)
	if err := p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := wait(t, l.turns, "interrupted turn"); err == nil {
		t.Fatal("interrupted turn reported success")
	}
	_ = p.Send("bg")
	if err := wait(t, l.turns, "bg turn"); err != nil {
		t.Fatalf("bg turn: %v", err)
	}
	if !wait(t, l.self, "self turn start") || wait(t, l.self, "self turn end") {
		t.Fatal("self turn not reported as start then end")
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
	select {
	case err := <-l.turns:
		t.Fatalf("crashed turn reported done: %v", err)
	default:
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

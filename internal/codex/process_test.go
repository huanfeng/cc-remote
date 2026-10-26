package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"cc-remote/internal/claude"
)

func TestMain(m *testing.M) {
	if os.Getenv("CODEX_ADAPTER_TEST_HELPER") == "1" {
		fakeAppServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeAppServer implements only the tested RPCs. Any inference outside a turn,
// overlap, leaked bridge credentials, or protocol mismatch fails the subprocess.
func fakeAppServer() {
	if os.Getenv("CODEX_ADAPTER_TEST_MODE") == "descendant" {
		for {
			_ = os.WriteFile(os.Getenv("CODEX_ADAPTER_TEST_TICK"), []byte(fmt.Sprint(time.Now().UnixNano())), 0600)
			time.Sleep(20 * time.Millisecond)
		}
	}
	fail := func(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(7) }
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "CC_REMOTE_") || key == "CLAUDECODE" {
			fail("leaked host environment: " + key)
		}
	}
	out := json.NewEncoder(os.Stdout)
	send := func(v any) {
		if err := out.Encode(v); err != nil {
			os.Exit(8)
		}
	}
	notify := func(method string, params any) { send(map[string]any{"method": method, "params": params}) }
	// Exercise unsolicited output as soon as the child starts.
	notify("thread/started", map[string]any{"thread": map[string]any{"id": "early"}})
	initialized := false
	ack := false
	active := ""
	turn := 0
	finish := func(status string) {
		notify("turn/completed", map[string]any{"threadId": "actual-thread", "turn": map[string]any{"id": fmt.Sprintf("turn-%d", turn), "status": status, "items": []any{}}})
		active = ""
	}
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		var msg rpcMessage
		if json.Unmarshal(s.Bytes(), &msg) != nil {
			fail("invalid JSON")
		}
		var params map[string]any
		_ = json.Unmarshal(msg.Params, &params)
		reply := func(v any) { send(map[string]any{"id": msg.ID, "result": v}) }
		switch msg.Method {
		case "initialize":
			if initialized {
				fail("duplicate initialize")
			}
			initialized = true
			if os.Getenv("CODEX_ADAPTER_TEST_MODE") == "stall" {
				continue
			}
			if os.Getenv("CODEX_ADAPTER_TEST_MODE") == "crash" {
				fail("synthetic startup crash")
			}
			reply(map[string]any{"userAgent": "fake", "platformFamily": "windows", "platformOs": "windows", "codexHome": os.Getenv("CODEX_HOME")})
		case "initialized":
			if !initialized {
				fail("early acknowledgement")
			}
			ack = true
			if os.Getenv("CODEX_ADAPTER_TEST_MODE") == "blocked-pipe" {
				time.Sleep(time.Hour)
			}
		case "model/list":
			if !ack {
				fail("model/list before handshake")
			}
			if os.Getenv("CODEX_ADAPTER_TEST_MODE") != "models" {
				fail("unexpected model discovery")
			}
			if params["cursor"] == nil {
				reply(map[string]any{"data": []any{map[string]any{"id": "picker-a", "model": "model-a", "displayName": "Model A", "description": "first"}}, "nextCursor": "page2"})
			} else {
				reply(map[string]any{"data": []any{map[string]any{"id": "model-b", "displayName": "Model B", "description": "second"}}, "nextCursor": nil})
			}
		case "thread/start", "thread/resume":
			if !ack {
				fail("thread before handshake")
			}
			if os.Getenv("CODEX_ADAPTER_TEST_MODE") == "models" {
				fail("model discovery started thread")
			}
			mode := os.Getenv("CODEX_ADAPTER_TEST_SANDBOX")
			if mode == "" {
				if _, ok := params["sandbox"]; ok {
					fail("empty sandbox should inherit")
				}
			} else if params["sandbox"] != mode {
				fail("incorrect sandbox")
			}
			policy := "on-request"
			if mode == "danger-full-access" {
				policy = "never"
			}
			if params["approvalPolicy"] != policy {
				fail("incorrect approval policy")
			}
			if msg.Method == "thread/start" && params["threadId"] != nil {
				fail("legacy placeholder leaked")
			}
			if msg.Method == "thread/resume" && params["threadId"] != "saved-thread" {
				fail("resume lost thread ID")
			}
			reply(map[string]any{"thread": map[string]any{"id": "actual-thread"}})
		case "turn/start", "thread/compact/start":
			if active != "" {
				fail("overlapping turns")
			}
			if params["threadId"] != "actual-thread" {
				fail("wrong actual thread")
			}
			turn++
			active = "compact"
			if msg.Method == "turn/start" {
				input, _ := params["input"].([]any)
				if len(input) != 1 && os.Getenv("CODEX_ADAPTER_TEST_MODE") != "attachments" {
					fail("bad input")
				}
				value, _ := input[0].(map[string]any)
				active = stringValue(value["text"])
				if os.Getenv("CODEX_ADAPTER_TEST_MODE") == "attachments" {
					if len(input) != 2 {
						fail("missing vision input")
					}
					image, _ := input[1].(map[string]any)
					if image["type"] != "localImage" || image["path"] != os.Getenv("CODEX_ADAPTER_TEST_IMAGE") {
						fail("wrong vision input")
					}
					if strings.Contains(active, "Use the Read tool") {
						fail("Claude attachment instruction leaked")
					}
					active = "attachment"
				}
			}
			if msg.Method == "thread/compact/start" {
				reply(map[string]any{})
				time.Sleep(75 * time.Millisecond)
			}
			notify("turn/started", map[string]any{"threadId": "actual-thread", "turn": map[string]any{"id": fmt.Sprintf("turn-%d", turn), "status": "inProgress", "items": []any{}}})
			if msg.Method != "thread/compact/start" {
				reply(map[string]any{"turn": map[string]any{"id": fmt.Sprintf("turn-%d", turn), "status": "inProgress", "items": []any{}}})
			}
			if msg.Method == "thread/compact/start" && os.Getenv("CODEX_ADAPTER_TEST_MODE") == "compact-delay" {
				continue
			}
			switch active {
			case "hold":
				continue
			case "tree":
				child := exec.Command(os.Args[0])
				child.Env = append(os.Environ(), "CODEX_ADAPTER_TEST_MODE=descendant")
				if err := child.Start(); err != nil {
					fail(err.Error())
				}
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(os.Getenv("CODEX_ADAPTER_TEST_TICK")); err == nil {
						break
					}
					if time.Now().After(deadline) {
						fail("descendant did not start")
					}
					time.Sleep(10 * time.Millisecond)
				}
				notify("item/agentMessage/delta", map[string]any{"threadId": "actual-thread", "turnId": fmt.Sprintf("turn-%d", turn), "itemId": "tree", "delta": "tree-ready"})
				continue
			case "approval", "file", "question", "unknown":
				method := "item/commandExecution/requestApproval"
				request := map[string]any{"threadId": "actual-thread", "turnId": fmt.Sprintf("turn-%d", turn), "itemId": "tool-1", "command": "echo hello", "startedAtMs": 123}
				if active == "file" {
					method = "item/fileChange/requestApproval"
				}
				if active == "question" {
					method = "item/tool/requestUserInput"
					request["isBlocking"] = true
					request["questions"] = []any{map[string]any{"id": "q1", "header": "Choice", "question": "Pick one?", "options": []any{map[string]any{"label": "A", "description": "First"}}}}
				}
				if active == "unknown" {
					method = "future/unsafe"
				}
				send(map[string]any{"id": "server-request", "method": method, "params": request})
				continue
			case "crash":
				fail("synthetic turn crash")
			case "tool":
				item := map[string]any{"id": "tool-1", "type": "commandExecution", "command": "echo hello", "cwd": ".", "status": "inProgress"}
				notify("item/started", map[string]any{"threadId": "actual-thread", "turnId": fmt.Sprintf("turn-%d", turn), "item": item, "startedAtMs": 123})
				item["status"] = "completed"
				item["aggregatedOutput"] = "hello"
				item["exitCode"] = 0
				notify("item/completed", map[string]any{"threadId": "actual-thread", "turnId": fmt.Sprintf("turn-%d", turn), "item": item, "completedAtMs": 124})
			}
			notify("item/agentMessage/delta", map[string]any{"threadId": "actual-thread", "turnId": fmt.Sprintf("turn-%d", turn), "itemId": "agent-1", "delta": "echo " + active})
			notify("item/completed", map[string]any{"threadId": "actual-thread", "turnId": fmt.Sprintf("turn-%d", turn), "item": map[string]any{"id": "agent-1", "type": "agentMessage", "text": "echo " + active}, "completedAtMs": 125})
			finish("completed")
		case "turn/steer":
			if active != "hold" || params["threadId"] != "actual-thread" || params["expectedTurnId"] != fmt.Sprintf("turn-%d", turn) {
				fail("wrong steer target")
			}
			switch os.Getenv("CODEX_ADAPTER_TEST_MODE") {
			case "steer-reject":
				send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "turn/steer unsupported"}})
			case "steer-wrong-ack":
				reply(map[string]any{"turnId": "different-turn"})
			case "steer-disconnect":
				fail("lost steer acknowledgement")
			default:
				reply(map[string]any{"turnId": fmt.Sprintf("turn-%d", turn)})
			}
		case "turn/interrupt":
			if active == "" || params["turnId"] != fmt.Sprintf("turn-%d", turn) {
				fail("wrong interrupt target")
			}
			reply(map[string]any{})
			finish("interrupted")
		case "":
			if active == "unknown" {
				if msg.Error == nil || msg.Error.Code != -32601 {
					fail("unknown request was not rejected")
				}
			} else {
				var result map[string]any
				_ = json.Unmarshal(msg.Result, &result)
				if active == "question" {
					answers, _ := result["answers"].(map[string]any)
					a, _ := answers["q1"].(map[string]any)
					list, _ := a["answers"].([]any)
					if len(list) != 1 || list[0] != "A" {
						fail("question answer mapping failed")
					}
				} else {
					want := os.Getenv("CODEX_ADAPTER_TEST_DECISION")
					if want == "" {
						want = "accept"
					}
					if result["decision"] != want {
						fail("incorrect approval decision")
					}
				}
			}
			finish("completed")
		default:
			fail("unexpected method " + msg.Method)
		}
	}
}

type adapterLog struct {
	events  chan claude.Event
	replays chan string
	results chan error
	exits   chan error
}

func fakeOptions(t *testing.T) claude.Options {
	t.Helper()
	t.Setenv("CODEX_ADAPTER_TEST_HELPER", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_REMOTE_HOST_TOKEN", "must-not-leak")
	t.Setenv("CLAUDECODE", "1")
	return claude.Options{Executable: exe, SessionID: "legacy-placeholder", Cwd: t.TempDir(), ConfigDir: t.TempDir(), BridgeURL: "http://127.0.0.1:1/bridge", BridgeToken: "private-token"}
}
func startFake(t *testing.T, o claude.Options) (*Process, adapterLog) {
	t.Helper()
	l := adapterLog{make(chan claude.Event, 128), make(chan string, 16), make(chan error, 16), make(chan error, 1)}
	p, err := StartProcess(o, claude.ProcessHooks{Event: func(e claude.Event) error { l.events <- e; return nil }, Replay: func(s string) { l.replays <- s }, Result: func(e error, _ bool) { l.results <- e }, Exit: func(e error) { l.exits <- e }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, l
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for fake app-server")
	}
	var zero T
	return zero
}

func TestAdapterPermissionsAndSession(t *testing.T) {
	for _, mode := range []string{"", "read-only", "workspace-write", "danger-full-access"} {
		t.Run(mode, func(t *testing.T) {
			o := fakeOptions(t)
			o.PermissionMode = mode
			t.Setenv("CODEX_ADAPTER_TEST_SANDBOX", mode)
			p, l := startFake(t, o)
			e := receive(t, l.events)
			if e.Kind != "session" || !strings.Contains(string(e.Payload), `"session_id":"actual-thread"`) {
				t.Fatalf("session: %+v", e)
			}
			if err := p.Send("hello"); err != nil {
				t.Fatal(err)
			}
			if err := receive(t, l.results); err != nil {
				t.Fatal(err)
			}
			if got := receive(t, l.replays); got != "hello" {
				t.Fatal(got)
			}
		})
	}
	o := fakeOptions(t)
	o.PermissionMode = "bypassPermissions"
	if _, err := StartProcess(o, claude.ProcessHooks{}); err == nil {
		t.Fatal("accepted Claude permission mode")
	}
}
func TestAdapterQueueInterruptCompactResume(t *testing.T) {
	o := fakeOptions(t)
	o.Resume = true
	o.SessionID = "saved-thread"
	p, l := startFake(t, o)
	for _, text := range []string{"hold", "second", "/compact"} {
		if err := p.Send(text); err != nil {
			t.Fatal(err)
		}
	}
	if got := receive(t, l.replays); got != "hold" {
		t.Fatal(got)
	}
	// Interrupt may race turn/start's reply: it still must cancel that turn.
	if err := p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, l.results); err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("interrupt: %v", err)
	}
	for _, want := range []string{"second", "/compact"} {
		if got := receive(t, l.replays); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
		if err := receive(t, l.results); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Send("/compact preserve details"); err == nil {
		t.Fatal("compact instructions were sent as model text")
	}
	if p.BackgroundTasks() != 0 || p.HasTask("missing") {
		t.Fatal("unexpected tasks")
	}
	p.Close()
	receive(t, p.Done())
	if err := p.Send("after close"); err == nil {
		t.Fatal("send after close")
	}
}
func TestAdapterToolsAndUnknownRequest(t *testing.T) {
	p, l := startFake(t, fakeOptions(t))
	_ = p.Send("tool")
	if err := receive(t, l.results); err != nil {
		t.Fatal(err)
	}
	var tool, result map[string]any
	for len(l.events) > 0 {
		e := <-l.events
		if e.Kind == "tool" {
			_ = json.Unmarshal(e.Payload, &tool)
		}
		if e.Kind == "tool_result" {
			_ = json.Unmarshal(e.Payload, &result)
		}
	}
	if tool["id"] != "tool-1" || tool["name"] != "Bash" || result["tool_use_id"] != "tool-1" || result["content"] != "hello" || result["is_error"] != false {
		t.Fatalf("tool=%v result=%v", tool, result)
	}
	_ = p.Send("unknown")
	if err := receive(t, l.results); err != nil {
		t.Fatal(err)
	}
}
func TestAdapterBridge(t *testing.T) {
	for _, text := range []string{"approval", "file", "question"} {
		t.Run(text, func(t *testing.T) {
			o := fakeOptions(t)
			var mu sync.Mutex
			var received map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer private-token" {
					http.Error(w, "unauthorized", 401)
					return
				}
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				mu.Lock()
				received = req
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(map[string]any{"decision": "allow", "answers": map[string]string{"Pick one?": "A"}})
			}))
			defer srv.Close()
			o.BridgeURL = srv.URL
			p, l := startFake(t, o)
			_ = p.Send(text)
			if err := receive(t, l.results); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if received["kind"] == nil {
				t.Fatal("missing bridge request")
			}
			if text == "question" && (received["kind"] != "question" || received["toolName"] != "AskUserQuestion") {
				t.Fatal(received)
			}
		})
	}
}
func TestAdapterBridgeFailureDeclines(t *testing.T) {
	t.Setenv("CODEX_ADAPTER_TEST_DECISION", "decline")
	p, l := startFake(t, fakeOptions(t))
	_ = p.Send("approval")
	if err := receive(t, l.results); err != nil {
		t.Fatal(err)
	}
}
func TestAdapterModelsAndTimeout(t *testing.T) {
	o := fakeOptions(t)
	t.Setenv("CODEX_ADAPTER_TEST_MODE", "models")
	models, err := Models(context.Background(), o.Executable, o.ConfigDir, o.Cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "model-a" || models[0].Label != "Model A" || models[1].ID != "model-b" {
		t.Fatal(models)
	}
	t.Setenv("CODEX_ADAPTER_TEST_MODE", "stall")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err = Models(ctx, o.Executable, o.ConfigDir, o.Cwd); err == nil {
		t.Fatal("handshake did not time out")
	}
}
func TestAdapterStartupFailureHasNoExitCallback(t *testing.T) {
	o := fakeOptions(t)
	t.Setenv("CODEX_ADAPTER_TEST_MODE", "crash")
	exit := make(chan error, 1)
	if _, err := StartProcess(o, claude.ProcessHooks{Exit: func(err error) { exit <- err }}); err == nil {
		t.Fatal("startup succeeded")
	} else if !strings.Contains(err.Error(), "synthetic startup crash") {
		t.Fatalf("lost stderr: %v", err)
	}
	select {
	case e := <-exit:
		t.Fatalf("startup invoked runtime Exit: %v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAdapterClosesProcessTree(t *testing.T) {
	o := fakeOptions(t)
	tick := o.Cwd + "/descendant-tick"
	t.Setenv("CODEX_ADAPTER_TEST_TICK", tick)
	p, l := startFake(t, o)
	if err := p.Send("tree"); err != nil {
		t.Fatal(err)
	}
	for {
		if e := receive(t, l.events); e.Text == "tree-ready" {
			break
		}
	}
	p.Close()
	receive(t, p.Done())
	before, err := os.ReadFile(tick)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	after, err := os.ReadFile(tick)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("Codex descendant survived Close")
	}
}

func TestAdapterBackgroundTaskLifecycle(t *testing.T) {
	p := &Process{tasks: map[string]bool{}, hooks: claude.ProcessHooks{}}
	item := map[string]any{"id": "agent-call", "type": "collabAgentToolCall", "agentsStates": map[string]any{"child-thread": map[string]any{"status": "running"}}}
	if err := p.itemEvent(item, false); err != nil {
		t.Fatal(err)
	}
	if p.BackgroundTasks() != 1 || !p.HasTask("child-thread") {
		t.Fatal("missing background task")
	}
	item["agentsStates"] = map[string]any{"child-thread": map[string]any{"status": "completed"}}
	if err := p.itemEvent(item, true); err != nil {
		t.Fatal(err)
	}
	if p.BackgroundTasks() != 0 || p.HasTask("child-thread") {
		t.Fatal("completed task retained")
	}
}
func TestAdapterInstalledModelsSmoke(t *testing.T) {
	if os.Getenv("CODEX_ADAPTER_REAL_SMOKE") != "1" {
		t.Skip("opt-in local catalog smoke; no inference")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := Models(ctx, "codex", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native model catalog returned %d models", len(models))
	if len(models) == 0 {
		t.Fatal("empty native catalog")
	}
}

func TestAdapterImageAttachments(t *testing.T) {
	o := fakeOptions(t)
	t.Setenv("CODEX_ADAPTER_TEST_MODE", "attachments")
	image := `C:\uploads\f6469fde-8dd6-4dc0-ba72-08cff7b350c7\3fa9ca23-c3e1-4cf9-b992-2bad76d7fdf4.png`
	t.Setenv("CODEX_ADAPTER_TEST_IMAGE", image)
	line, _ := json.Marshal(map[string]any{"path": image, "name": "shot.png", "mime": "image/png", "size": 79})
	prompt := "Describe the screenshot" + claude.AttachmentNote + string(line)
	p, l := startFake(t, o)
	if err := p.Send(prompt); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, l.replays); got != prompt {
		t.Fatal("Replay changed the original prompt")
	}
	if err := receive(t, l.results); err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []string{"ordinary text", "x" + claude.AttachmentNote + "invalid JSON", "x" + claude.AttachmentNote + `{"path":"/etc/passwd","mime":"image/png"}`} {
		items := turnInput(malformed)
		text := items[0].(map[string]any)["text"]
		if len(items) != 1 || text != malformed {
			t.Fatalf("malformed marker changed: %#v", items)
		}
	}
}

func TestAdapterCompactDelayedTurnInterrupt(t *testing.T) {
	o := fakeOptions(t)
	t.Setenv("CODEX_ADAPTER_TEST_MODE", "compact-delay")
	p, l := startFake(t, o)
	if err := p.Send("/compact"); err != nil {
		t.Fatal(err)
	}
	receive(t, l.replays)
	if err := p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, l.results); err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("compact interrupt: %v", err)
	}
}

func TestAdapterInterruptBeforeQueueWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &Process{rpc: &rpcClient{ctx: ctx}, queue: make(chan string, 1), requests: map[string]context.CancelFunc{}}
	if err := p.Send("hold"); err != nil {
		t.Fatal(err)
	}
	if err := p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if !p.interruptPending {
		t.Fatal("interrupt before queue worker was lost")
	}
}

func TestAdapterRPCWriteDeadline(t *testing.T) {
	o := fakeOptions(t)
	t.Setenv("CODEX_ADAPTER_TEST_MODE", "blocked-pipe")
	r, err := startRPC(o.Executable, o.ConfigDir, o.Cwd, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = r.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	firstCtx, firstCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer firstCancel()
	result := make(chan error, 1)
	go func() {
		result <- r.call(firstCtx, "turn/start", map[string]any{"input": strings.Repeat("x", 8<<20)}, nil)
	}()
	deadline := time.Now().Add(time.Second)
	for len(r.writeGate) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("writer did not acquire gate")
		}
		time.Sleep(time.Millisecond)
	}
	secondCtx, secondCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer secondCancel()
	started := time.Now()
	if err = r.call(secondCtx, "another/request", map[string]any{}, nil); err == nil {
		t.Fatal("writer gate ignored deadline")
	}
	if time.Since(started) > time.Second {
		t.Fatal("writer gate remained blocked")
	}
	if err = receive(t, result); err == nil {
		t.Fatal("blocked pipe write ignored deadline")
	}
	receive(t, r.done)
}

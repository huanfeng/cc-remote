package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/claude"
)

const testFirstForkTurn = "12345678-abcd-4321-9876-123456789ab1"
const testSecondForkTurn = "12345678-abcd-4321-9876-123456789ab2"
const testForkChild = "12345678-abcd-4321-9876-123456789ab9"

func TestForkNativeCodexVerifiesBoundary(t *testing.T) {
	options := fakeOptions(t)
	options.SessionID = testNativeID
	for _, anchor := range []string{"", testFirstForkTurn} {
		if child, err := ForkNativeSession(context.Background(), options, anchor); err != nil || child != testForkChild {
			t.Fatalf("fork through %q: child=%q err=%v", anchor, child, err)
		}
	}
	t.Setenv("CODEX_ADAPTER_TEST_MODE", "ignore-fork-boundary")
	if child, err := ForkNativeSession(context.Background(), options, testFirstForkTurn); err == nil || child != "" {
		t.Fatalf("accepted a child containing a later turn: %q, %v", child, err)
	}
}

func TestNativeCodexHistoryCompletesForkAnchorsAfterAppend(t *testing.T) {
	home := t.TempDir()
	prompt := nativeMessage("user", "question")
	first := codexForkMarker("task_started", testFirstForkTurn) + prompt +
		nativeMessage("user", "steered followup") + nativeMessage("assistant", "response")
	path := writeRollout(t, home, testNativeID, nativeMeta(testNativeID)+first)
	_, events, err := NativeHistory(home, testNativeID)
	if err != nil || len(events) != 3 || events[0].NativeAnchor != "" {
		t.Fatalf("unfinished turn exposed a boundary: %+v, %v", events, err)
	}
	appendText := func(text string) {
		t.Helper()
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err := file.WriteString(text); err != nil {
			t.Fatal(err)
		}
	}
	appendText(codexForkMarker("task_complete", testFirstForkTurn))
	_, events, err = NativeHistory(home, testNativeID)
	if err != nil || len(events) != 3 || events[0].NativeAnchor != testFirstForkTurn ||
		!events[0].NativeParentKnown || events[0].NativeParentAnchor != "" || events[1].NativeAnchor != "" ||
		events[2].NativeAnchor != testFirstForkTurn {
		t.Fatalf("first completed turn anchors: %+v, %v", events, err)
	}
	appendText(codexForkMarker("task_started", testSecondForkTurn) + prompt + codexForkMarker("task_complete", testSecondForkTurn))
	_, events, err = NativeHistory(home, testNativeID)
	if err != nil || len(events) != 4 || !events[3].NativeParentKnown || events[3].NativeParentAnchor != testFirstForkTurn {
		t.Fatalf("next turn lost previous boundary after cache extension: %+v, %v", events, err)
	}
}

func codexForkMarker(kind, id string) string {
	return fmt.Sprintf(`{"type":"event_msg","payload":{"type":%q,"turn_id":%q}}`+"\n", kind, id)
}

func TestNativeCodexLinkedForkHistory(t *testing.T) {
	home := t.TempDir()
	nested := "12345678-abcd-4321-9876-123456789ab8"
	write := func(id string, ordinal int, rows string) {
		t.Helper()
		var rollout strings.Builder
		for _, line := range strings.Split(strings.TrimSpace(rows), "\n") {
			fmt.Fprintf(&rollout, "{\"ordinal\":%d,%s\n", ordinal, line[1:])
			ordinal++
		}
		writeRollout(t, home, id, rollout.String())
	}
	turn := func(id, input string) string {
		return codexForkMarker("task_started", id) + nativeMessage("user", input) +
			nativeMessage("assistant", "answer") + codexForkMarker("task_complete", id)
	}
	link := func(id, parent string) string {
		return fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"forked_from_id":%q,"forked_from_ordinal_exclusive":5}}`+"\n", id, parent)
	}
	write(testNativeID, 0, nativeMeta(testNativeID)+turn(testFirstForkTurn, "shared")+turn(testSecondForkTurn, "parent only"))
	write(testForkChild, 5, link(testForkChild, testNativeID)+turn(testSecondForkTurn, "child only"))
	write(nested, 5, link(nested, testForkChild))
	_, events, err := NativeHistory(home, testForkChild)
	if err != nil || len(events) != 4 || events[0].Text != "shared" || events[2].Text != "child only" ||
		!events[2].NativeParentKnown || events[2].NativeParentAnchor != testFirstForkTurn {
		t.Fatalf("linked history: %+v, %v", events, err)
	}
	_, events, err = NativeHistory(home, nested)
	if err != nil || len(events) != 2 || events[0].Text != "shared" || events[0].NativeAnchor != testFirstForkTurn {
		t.Fatalf("linked ancestor cutoff: %+v, %v", events, err)
	}
}

func TestNativeCodexForkWithInstalledCLI(t *testing.T) {
	executable := os.Getenv("CC_REMOTE_TEST_CODEX_FORK")
	if executable == "" {
		t.Skip("set CC_REMOTE_TEST_CODEX_FORK to the installed Codex CLI")
	}
	for _, mode := range []string{"legacy", "paginated"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "paginated" {
				testNativeCodexPaginatedFork(t, executable)
			} else {
				testNativeCodexLegacyFork(t, executable)
			}
		})
	}
}

func testNativeCodexLegacyFork(t *testing.T, executable string) {
	t.Helper()
	home, cwd := t.TempDir(), t.TempDir()
	const stamp = "2026-10-24T01:00:00.000Z"
	meta, err := json.Marshal(map[string]any{"timestamp": stamp, "type": "session_meta",
		"payload": map[string]any{"timestamp": stamp, "id": testNativeID, "cwd": cwd,
			"source": "cli", "originator": "codex_cli_rs", "cli_version": "0.167.2",
			"model_provider": "openai", "history_mode": "legacy"}})
	if err != nil {
		t.Fatal(err)
	}
	turn := func(id, question, answer string) string {
		start := fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"task_started","turn_id":%q,"collaboration_mode_kind":"default"}}`+"\n", stamp, id)
		prompt := fmt.Sprintf(`{"timestamp":%q,"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":%q}]}}`+"\n", stamp, question)
		reply := fmt.Sprintf(`{"timestamp":%q,"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}}`+"\n", stamp, answer)
		end := fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"task_complete","turn_id":%q,"last_agent_message":%q}}`+"\n", stamp, id, answer)
		return start + prompt + reply + end
	}
	source := string(meta) + "\n" + turn(testFirstForkTurn, "original question", "first answer") +
		turn(testSecondForkTurn, "later question", "second answer")
	path := writeRollout(t, home, "2026-10-24T01-00-00-"+testNativeID, source)
	o := claude.Options{ConfigDir: home, Executable: executable, Cwd: cwd, SessionID: testNativeID}
	child, err := ForkNativeSession(context.Background(), o, testFirstForkTurn)
	if err != nil {
		t.Fatal(err)
	}
	_, events, err := NativeHistory(home, child)
	if err != nil || len(events) != 2 || events[0].Text != "original question" {
		t.Fatalf("forked Codex history: %+v, %v", events, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != source {
		t.Fatalf("source transcript changed: %v", err)
	}
}

func testNativeCodexPaginatedFork(t *testing.T, executable string) {
	t.Helper()
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"type":"response.created","response":{"id":"resp_test"}}`,
			`{"type":"response.output_item.done","item":{"type":"message","role":"assistant","id":"msg_test","content":[{"type":"output_text","text":"fixture response"}]}}`,
			`{"type":"response.completed","response":{"id":"resp_test"}}`,
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
	defer mock.Close()
	o := claude.Options{ConfigDir: t.TempDir(), Cwd: t.TempDir(), Executable: executable,
		ModelProvider: "fork_fixture", Model: "gpt-5.4-mini", ConfigOverrides: map[string]any{
			"model_provider": "fork_fixture", "model_providers": map[string]any{
				"fork_fixture": map[string]any{"name": "Fork fixture", "base_url": mock.URL + "/v1", "wire_api": "responses"},
			},
		}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	completed := make(chan string, 4)
	r, err := startRPC(o.Executable, o.ConfigDir, o.Cwd, func(m rpcMessage) error {
		if m.Method == "turn/completed" {
			var notification struct {
				Turn struct {
					Status string `json:"status"`
				} `json:"turn"`
			}
			_ = json.Unmarshal(m.Params, &notification)
			completed <- notification.Turn.Status
		}
		return nil
	}, nil, o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	if err := r.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	params := threadOptions(o)
	params["historyMode"] = "paginated"
	if err := r.call(ctx, "thread/start", params, &response); err != nil {
		t.Fatal(err)
	}
	o.SessionID = response.Thread.ID
	var firstTurnID string
	for _, prompt := range []string{"original question", "later question"} {
		var started struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if err := r.call(ctx, "turn/start", map[string]any{"threadId": o.SessionID, "input": turnInput(prompt)}, &started); err != nil {
			t.Fatal(err)
		}
		if firstTurnID == "" {
			firstTurnID = started.Turn.ID
		}
		select {
		case status := <-completed:
			if status != "completed" {
				t.Fatalf("mock turn ended with status %q", status)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	child, err := ForkNativeSession(ctx, o, firstTurnID)
	if err != nil {
		t.Fatal(err)
	}
	_, events, err := NativeHistory(o.ConfigDir, child)
	if err != nil || len(events) != 2 || events[0].Text != "original question" || events[0].NativeAnchor != firstTurnID {
		t.Fatalf("forked paginated Codex history: %+v, %v", events, err)
	}
	o.SessionID = child
	nested, err := ForkNativeSession(ctx, o, firstTurnID)
	if err != nil {
		t.Fatal(err)
	}
	_, events, err = NativeHistory(o.ConfigDir, nested)
	if err != nil || len(events) != 2 || events[1].Text != "fixture response" {
		t.Fatalf("nested paginated Codex history: %+v, %v", events, err)
	}
}

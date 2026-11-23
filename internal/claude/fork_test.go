package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const firstForkMessage = "11111111-2222-3333-4444-555555555556"
const secondForkMessage = "11111111-2222-3333-4444-555555555557"
const thirdForkMessage = "11111111-2222-3333-4444-555555555558"

func TestNativeClaudeForkAnchors(t *testing.T) {
	configDir, cwd := t.TempDir(), t.TempDir()
	writeNative(t, configDir, "test", claudeForkFixture(t, cwd))
	_, events, err := NativeHistory(configDir, testID)
	if err != nil || len(events) != 3 {
		t.Fatalf("Claude native history: %+v, %v", events, err)
	}
	if events[0].NativeAnchor != firstForkMessage || !events[0].NativeParentKnown || events[0].NativeParentAnchor != "" {
		t.Fatalf("first message has no verified root: %+v", events[0])
	}
	if events[2].NativeAnchor != thirdForkMessage || !events[2].NativeParentKnown || events[2].NativeParentAnchor != secondForkMessage {
		t.Fatalf("edit boundary does not use the exact native parent: %+v", events[2])
	}
}

func TestNativeClaudeForkWithInstalledCLI(t *testing.T) {
	executable := os.Getenv("CC_REMOTE_TEST_CLAUDE_FORK")
	if executable == "" {
		t.Skip("set CC_REMOTE_TEST_CLAUDE_FORK to the installed Claude CLI")
	}
	configDir, cwd := t.TempDir(), t.TempDir()
	project := regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(cwd, "-")
	source := claudeForkFixture(t, cwd)
	writeNative(t, configDir, project, source)
	options := Options{ConfigDir: configDir, Cwd: cwd, SessionID: testID, Executable: executable}

	for _, test := range []struct {
		anchor string
		want   int
	}{{firstForkMessage, 1}, {secondForkMessage, 2}, {"", 3}} {
		child, err := ForkNativeSession(context.Background(), options, test.anchor)
		if err != nil {
			t.Fatalf("fork through %q: %v", test.anchor, err)
		}
		_, events, err := NativeHistory(configDir, child)
		if err != nil || len(events) != test.want || events[0].Text != "original question" {
			t.Fatalf("fork through %q returned %+v, %v", test.anchor, events, err)
		}
		// The fork must itself be a valid resume source.
		nested := options
		nested.SessionID = child
		if _, err := ForkNativeSession(context.Background(), nested, ""); err != nil {
			t.Fatalf("resume forked Claude session: %v", err)
		}
	}
	actual, err := os.ReadFile(filepath.Join(configDir, "projects", project, testID+".jsonl"))
	if err != nil || string(actual) != source {
		t.Fatalf("source transcript changed: %v", err)
	}
}

func claudeForkFixture(t *testing.T, cwd string) string {
	t.Helper()
	var fixture strings.Builder
	var parent any
	for _, entry := range []struct {
		id, role, text string
	}{{firstForkMessage, "user", "original question"},
		{secondForkMessage, "assistant", "original response"},
		{thirdForkMessage, "user", "later question"}} {
		var content any = entry.text
		if entry.role == "assistant" {
			content = []map[string]string{{"type": "text", "text": entry.text}}
		}
		data, err := json.Marshal(map[string]any{
			"type": entry.role, "uuid": entry.id, "parentUuid": parent, "cwd": cwd, "sessionId": testID,
			"version": "2.1.330", "timestamp": "2026-07-17T12:00:00Z",
			"message": map[string]any{"id": "msg_test", "role": entry.role, "content": content},
		})
		if err != nil {
			t.Fatal(err)
		}
		fixture.Write(data)
		fixture.WriteByte('\n')
		parent = entry.id
	}
	return fixture.String()
}

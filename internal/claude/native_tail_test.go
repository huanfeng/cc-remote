package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeNative(t *testing.T, dir, project, content string) {
	t.Helper()
	p := filepath.Join(dir, "projects", project)
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, testID+".jsonl"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// A transcript over the limit loads its recent part, from a line start.
func TestNativeHistoryTail(t *testing.T) {
	old := maxNativeHistory
	maxNativeHistory = 300
	t.Cleanup(func() { maxNativeHistory = old })
	var b strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&b, `{"type":"user","message":{"content":"message %02d"}}`+"\n", i)
	}
	dir := t.TempDir()
	writeNative(t, dir, "-tmp-tail", b.String())
	s, events, err := NativeHistory(dir, testID)
	if err != nil || !s.Truncated || len(events) == 0 || len(events) >= 20 {
		t.Fatalf("%+v %d %v", s, len(events), err)
	}
	if events[len(events)-1].Text != "message 20" || !strings.HasPrefix(events[0].Text, "message ") {
		t.Fatalf("first %q last %q", events[0].Text, events[len(events)-1].Text)
	}
}

// Compaction shows as a divider; a background task's notification, added by
// Claude Code as a user turn, is not shown as typed by the user.
func TestNativeHistoryCompactAndNotifications(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "-tmp-kinds", `{"type":"user","origin":{"kind":"human"},"promptSource":"typed","message":{"content":"typed"}}
{"type":"system","subtype":"compact_boundary","content":"Conversation compacted","compactMetadata":{"trigger":"manual","preTokens":48026,"postTokens":8969}}
{"type":"user","origin":{"kind":"task-notification"},"promptSource":"system","message":{"content":"<task-notification> <task-id>x</task-id> done"}}
{"type":"user","message":{"content":"<task-notification> older Claude Code, no origin"}}
{"type":"user","promptSource":"system","message":{"content":"system text"}}
{"type":"attachment","attachment":{"type":"queued_command","prompt":"typed mid-turn","commandMode":"prompt","origin":{"kind":"human"}}}
{"type":"attachment","attachment":{"type":"queued_command","prompt":"<task-notification> x","commandMode":"prompt","origin":{"kind":"task-notification"}}}
`)
	_, all, err := NativeHistory(dir, testID)
	var events []Event // task notifications become task events (tasks_test.go)
	for _, e := range all {
		if e.Kind != "status" {
			events = append(events, e)
		}
	}
	if err != nil || len(events) != 3 || events[0].Text != "typed" || events[1].Kind != "compact" || events[2].Kind != "user" || events[2].Text != "typed mid-turn" {
		t.Fatalf("%+v %v", events, err)
	}
	if p := string(events[1].Payload); !strings.Contains(p, `"pre_tokens":48026`) || !strings.Contains(p, `"trigger":"manual"`) {
		t.Fatal(p)
	}
}

// The cache drops the least recently read sessions past its byte budget, keeping
// the one just read, and drops ones unread for historyIdle.
func TestNativeHistoryCacheBudget(t *testing.T) {
	oldBytes, oldIdle := historyCacheBytes, historyIdle
	t.Cleanup(func() { historyCacheBytes, historyIdle = oldBytes, oldIdle })
	dir := t.TempDir()
	ids := []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333"}
	p := filepath.Join(dir, "projects", "-tmp-cache")
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","message":{"content":"` + strings.Repeat("x", 1000) + `"}}` + "\n"
	for _, id := range ids {
		if err := os.WriteFile(filepath.Join(p, id+".jsonl"), []byte(strings.Repeat(line, 10)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cached := func() (paths []string) {
		historyCache.Lock()
		defer historyCache.Unlock()
		for _, path := range historyCache.order {
			paths = append(paths, strings.TrimSuffix(filepath.Base(path), ".jsonl")[:1])
		}
		return
	}
	trimHistory("")
	historyIdle = time.Hour
	historyCacheBytes = 25_000 // two sessions of about 11 KB
	for _, id := range ids {
		if _, _, err := NativeHistory(dir, id); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(cached(), ""); got != "23" {
		t.Fatalf("over budget: cached %q", got)
	}
	historyCacheBytes = 1 // only the session just read stays
	if _, _, err := NativeHistory(dir, ids[0]); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cached(), ""); got != "1" {
		t.Fatalf("one huge session: cached %q", got)
	}
	historyIdle = 0
	trimHistory("")
	if got := cached(); len(got) != 0 {
		t.Fatalf("idle: cached %q", got)
	}
}

package claude

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestNativeSearchFindsMessagesBeforeHistoryWindow(t *testing.T) {
	dir := t.TempDir()
	const first = `{"type":"user","cwd":"/project","message":{"content":"older searchable prompt"}}` + "\n"
	writeNative(t, dir, "project", first)
	path := filepath.Join(dir, "projects", "project", testID+".jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	padding := `{"type":"ignored","data":"` + strings.Repeat("x", 1<<20) + `"}` + "\n"
	for range 65 {
		if _, err := f.WriteString(padding); err != nil {
			f.Close()
			t.Fatal(err)
		}
	}
	if _, err := f.WriteString(`{"type":"assistant","timestamp":"2026-11-20T04:00:00Z","message":{"content":"newer searchable reply"}}` + "\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	var texts []string
	cursors := map[string]string{}
	err = SearchNativeAll(t.Context(), dir, func(session NativeSession) bool {
		return session.Cwd == "/project"
	}, func(session NativeSession, event Event, cursor string) bool {
		if session.ID != testID || session.Provider != "claude" {
			t.Fatalf("wrong session: %+v", session)
		}
		texts = append(texts, event.Text)
		cursors[event.Text] = cursor
		if event.Kind == "assistant" && event.CreatedAt != "2026-11-20T04:00:00Z" {
			t.Fatalf("missing original time: %+v", event)
		}
		return true
	})
	if err != nil || !slices.Equal(texts, []string{"older searchable prompt", "newer searchable reply"}) {
		t.Fatalf("search %q: %v", texts, err)
	}
	for message, cursor := range cursors {
		_, events, index, err := NativeSearchContext(t.Context(), dir, testID, cursor)
		if err != nil || len(events) == 0 || events[index].Text != message {
			t.Fatalf("context for %q: %d %+v %v", message, index, events, err)
		}
	}
}

func TestNativeSearchCursorDetectsRewrite(t *testing.T) {
	dir := t.TempDir()
	initial := `{"type":"user","message":{"content":"before"}}` + "\n" +
		`{"type":"assistant","message":{"content":"target"}}` + "\n"
	const after = `{"type":"user","message":{"content":"after"}}` + "\n"
	writeNative(t, dir, "project", initial)
	var target string
	_, err := SearchNative(t.Context(), dir, testID, func(event Event, cursor string) bool {
		if event.Text == "target" {
			target = cursor
			return false
		}
		return true
	})
	if err != nil || target == "" {
		t.Fatalf("search cursor %q: %v", target, err)
	}
	writeNative(t, dir, "project", initial+after)
	_, events, focus, err := NativeSearchContext(t.Context(), dir, testID, target)
	if err != nil || len(events) != 3 || focus != 1 || events[focus].Text != "target" {
		t.Fatalf("after append: %d %+v %v", focus, events, err)
	}
	writeNative(t, dir, "project", strings.Replace(initial+after, "target", "edited", 1))
	_, _, _, err = NativeSearchContext(t.Context(), dir, testID, target)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("after rewrite: %v", err)
	}
}

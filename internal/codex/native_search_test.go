package codex

import (
	"os"
	"slices"
	"strings"
	"testing"

	"cc-remote/internal/claude"
)

func TestNativeSearchFindsMessagesAfterHistoryWindow(t *testing.T) {
	home := t.TempDir()
	path := writeRollout(t, home, testNativeID, nativeMeta(testNativeID)+
		nativeMessage("developer", "hidden configuration")+nativeMessage("user", "first searchable prompt"))
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
	if _, err := f.WriteString(nativeMessage("assistant", "last searchable reply")); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	var texts []string
	var target string
	session, err := SearchNative(t.Context(), home, testNativeID, func(event claude.Event, cursor string) bool {
		texts = append(texts, event.Text)
		target = cursor
		return true
	})
	if err != nil || session.Provider != "codex" || !slices.Equal(texts, []string{"first searchable prompt", "last searchable reply"}) {
		t.Fatalf("search %+v %q: %v", session, texts, err)
	}
	_, events, focus, err := NativeSearchContext(t.Context(), home, testNativeID, target)
	if err != nil || len(events) == 0 || events[focus].Text != "last searchable reply" {
		t.Fatalf("context: %d %+v %v", focus, events, err)
	}
	var batchCursor string
	err = SearchNativeAll(t.Context(), home, func(s claude.NativeSession) bool {
		return s.ID == testNativeID
	}, func(_ claude.NativeSession, event claude.Event, cursor string) bool {
		batchCursor = cursor
		return event.Text != "last searchable reply"
	})
	if err != nil || batchCursor != target {
		t.Fatalf("batch cursor %q, expected %q: %v", batchCursor, target, err)
	}
}

package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeConversationSourceTimes(t *testing.T) {
	dir := t.TempDir()
	writeNative(t, dir, "clock", `{"type":"user","timestamp":"2020-01-01T01:00:00+01:00","message":{"content":"hello"}}
{"type":"assistant","timestamp":"2020-01-01T00:00:00.9Z","message":{"content":[{"type":"tool_use","id":"x","name":"Read","input":{}}]}}
{"type":"user","timestamp":"2020-01-01T00:00:00.8Z","message":{"content":[{"type":"tool_result","tool_use_id":"x","content":"done"}]}}
{"type":"custom-title","timestamp":"2030-01-01T00:00:00Z","customTitle":"renamed"}
{"type":"user","timestamp":"2030-01-01T00:00:00Z","isMeta":true,"message":{"content":"metadata"}}
{"type":"user","timestamp":"2030-01-01T00:00:00Z","message":{"content":"<system-reminder> injected"}}
`)
	want := "2020-01-01T00:00:00.9Z"
	for i := 0; i < 2; i++ {
		s, events, err := NativeHistory(dir, testID)
		if err != nil || s.LastConversationAt != want || s.ConversationTimeEstimated || len(events) != 3 {
			t.Fatalf("history: %+v %+v %v", s, events, err)
		}
		if events[0].CreatedAt != "2020-01-01T00:00:00Z" || events[1].CreatedAt != want {
			t.Fatalf("source event times: %+v", events)
		}
		list, err := ListNative(dir)
		if err != nil || len(list) != 1 || list[0].LastConversationAt != want || list[0].ConversationTimeEstimated {
			t.Fatalf("list: %+v %v", list, err)
		}
	}
	// Metadata appended after the cache was populated cannot become activity.
	path := filepath.Join(dir, "projects", "clock", testID+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"type\":\"custom-title\",\"timestamp\":\"2031-01-01T00:00:00Z\",\"customTitle\":\"again\"}\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := NativeHistory(dir, testID)
	if err != nil || s.LastConversationAt != want {
		t.Fatalf("metadata append: %+v %v", s, err)
	}
}

func TestNativeConversationFallbackAndRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects", "fallback", testID+".jsonl")
	mod := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	for _, data := range []string{
		"{\"type\":\"user\",\"timestamp\":\"invalid\",\"message\":{\"content\":\"hello\"}}\n",
		"{\"type\":\"custom-title\",\"timestamp\":\"2030-01-01T00:00:00Z\",\"customTitle\":\"metadata\"}\n",
	} {
		writeNative(t, dir, "fallback", data)
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
		s, events, err := NativeHistory(dir, testID)
		if err != nil {
			t.Fatal(err)
		}
		list, err := ListNative(dir)
		if err != nil || len(list) != 1 {
			t.Fatalf("list: %+v %v", list, err)
		}
		for _, got := range []NativeSession{s, list[0]} {
			if len(events) > 0 {
				if got.LastConversationAt != mod.Format(time.RFC3339Nano) || !got.ConversationTimeEstimated || events[0].CreatedAt != "" {
					t.Fatalf("fallback: %+v %+v", got, events)
				}
			} else if got.LastConversationAt != "" || got.ConversationTimeEstimated {
				t.Fatalf("metadata fabricated activity: %+v", got)
			}
		}
		mod = mod.Add(time.Hour)
	}
}

func TestNativeConversationStableSort(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "projects", "sort")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	ids := []string{"22345678-abcd-4321-9876-123456789abc", "12345678-abcd-4321-9876-123456789abc", "32345678-abcd-4321-9876-123456789abc"}
	for i, id := range ids {
		stamp := "2020-01-01T00:00:00Z"
		if i == 2 {
			stamp = "2020-01-01T00:00:00.1Z"
		}
		data := `{"type":"user","timestamp":"` + stamp + `","message":{"content":"hi"}}` + "\n"
		if err := os.WriteFile(filepath.Join(project, id+".jsonl"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	list, err := ListNative(dir)
	if err != nil || len(list) != 3 || list[0].ID != ids[2] || list[1].ID != ids[1] {
		t.Fatalf("sort: %+v %v", list, err)
	}
}

func TestNativeTaskOriginalTime(t *testing.T) {
	s := NativeSession{Title: "Claude session"}
	line := []byte(`{"type":"user","timestamp":"2030-01-01T00:00:00Z","message":{"content":"<teammate-message teammate_id=\"researcher\">\n{\"type\":\"idle_notification\",\"timestamp\":\"2020-01-01T00:00:00Z\",\"result\":\"done\"}\n</teammate-message>"}}`)
	events := transcriptEvents(line, &s)
	if len(events) != 1 || events[0].CreatedAt != "2020-01-01T00:00:00Z" || s.LastConversationAt != events[0].CreatedAt {
		t.Fatalf("task source time: %+v %+v", s, events)
	}
}

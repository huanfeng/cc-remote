package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testNativeID = "12345678-abcd-4321-9876-123456789abc"

func writeRollout(t *testing.T, home, name, data string) string {
	t.Helper()
	path := filepath.Join(home, "sessions", "2026", "10", "24", "rollout-"+name+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func nativeMeta(id string) string {
	return fmt.Sprintf(`{"type":"session_meta","timestamp":"2026-10-24T01:00:00Z","payload":{"id":%q,"cwd":"/synthetic/project","timestamp":"2026-10-24T01:00:00Z"}}`, id) + "\n"
}

func nativeMessage(role, text string) string {
	data, _ := json.Marshal(map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": role, "content": []any{map[string]any{"type": "input_text", "text": text}}}})
	return string(data) + "\n"
}

func TestNativeHistoryResponseItems(t *testing.T) {
	home := t.TempDir()
	writeRollout(t, home, testNativeID, nativeMeta(testNativeID)+
		nativeMessage("developer", "hidden configuration")+
		nativeMessage("user", "# AGENTS.md instructions\nsynthetic configuration")+
		`{"type":"event_msg","payload":{"type":"user_message","message":"Please fix the synthetic test"}}`+"\n"+
		nativeMessage("user", "Please fix the synthetic test")+
		`{"type":"response_item","payload":{"type":"function_call","call_id":"call-1","name":"exec_command","arguments":"{\"cmd\":\"echo synthetic\"}"}}`+"\n"+
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":"synthetic output","is_error":true}}`+"\n"+
		`{"type":"event_msg","payload":{"type":"agent_message","message":"Done"}}`+"\n"+
		`{"type":"response_item","timestamp":"2026-10-24T02:00:00Z","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done"},{"type":"output_text","text":"Verified"}]}}`+"\n"+
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":5}}}}`+"\n{invalid\n{\"type\":")
	s, events, err := NativeHistory(home, strings.ToUpper(testNativeID))
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != testNativeID || s.Provider != "codex" || s.Title != "Please fix the synthetic test" || s.Cwd != "/synthetic/project" || s.UpdatedAt != "2026-10-24T02:00:00Z" {
		t.Fatalf("session: %+v", s)
	}
	if len(events) != 5 {
		t.Fatalf("events: %+v", events)
	}
	if events[1].Kind != "user" || events[2].Kind != "tool" || events[3].Kind != "tool_result" || events[4].Text != "Done\nVerified" {
		t.Fatalf("events: %+v", events)
	}
	var tool struct {
		ID, Name string
		Input    map[string]string
	}
	if err := json.Unmarshal(events[2].Payload, &tool); err != nil {
		t.Fatal(err)
	}
	if tool.ID != "call-1" || tool.Name != "exec_command" || tool.Input["cmd"] != "echo synthetic" {
		t.Fatalf("tool: %+v", tool)
	}
	var result struct {
		ID      string `json:"tool_use_id"`
		Content string
		IsError bool `json:"is_error"`
	}
	if err := json.Unmarshal(events[3].Payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != "call-1" || result.Content != "synthetic output" || !result.IsError || events[3].Text != "synthetic output" {
		t.Fatalf("result: %+v", result)
	}
	list, err := ListNative(home)
	if err != nil || len(list) != 1 || !reflect.DeepEqual(list[0], s) {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}

func TestNativeListTitlesAndEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	second := "22345678-abcd-4321-9876-123456789abc"
	writeRollout(t, home, "old", nativeMeta(testNativeID)+nativeMessage("user", "Original title")+
		`{"type":"event_msg","payload":{"type":"session_title","title":"Saved title"}}`+"\n")
	writeRollout(t, home, "new", nativeMeta(second)+nativeMessage("user", "A\n meaningful   title")+
		`{"type":"event_msg","timestamp":"2026-10-24T03:00:00Z","payload":{"type":"ignored"}}`+"\n")
	writeRollout(t, home, "invalid", nativeMeta("../../outside"))
	list, err := ListNative("")
	if err != nil || len(list) != 2 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if list[0].ID != second || list[0].Title != "A meaningful title" || list[1].Title != "Saved title" {
		t.Fatalf("list: %+v", list)
	}
	// The explicit directory takes precedence over CODEX_HOME.
	list, err = ListNative(t.TempDir())
	if err != nil || len(list) != 0 {
		t.Fatalf("explicit home: %+v %v", list, err)
	}
}

func TestNativeMissingAndInvalidIDs(t *testing.T) {
	home := filepath.Join(t.TempDir(), "missing")
	list, err := ListNative(home)
	if err != nil || len(list) != 0 {
		t.Fatalf("missing home: %+v %v", list, err)
	}
	for _, id := range []string{"", "../outside", testNativeID + "/../outside", "12345678-abcd-4321-9876-123456789abg"} {
		if _, _, err := NativeHistory(home, id); err == nil {
			t.Fatalf("accepted id %q", id)
		}
	}
	if _, _, err := NativeHistory(t.TempDir(), testNativeID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing session: %v", err)
	}
}

func TestNativeHistoryBoundAndMalformedArguments(t *testing.T) {
	home := t.TempDir()
	data := nativeMeta(testNativeID) + nativeMessage("user", strings.Repeat("界", 100)) +
		`{"type":"response_item","payload":{"type":"function_call","call_id":"bad","name":"test","arguments":"not json"}}` + "\n"
	path := writeRollout(t, home, testNativeID, data+nativeMessage("assistant", "outside the read limit"))
	root, err := nativeRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	rel, err := filepath.Rel(home, path)
	if err != nil {
		t.Fatal(err)
	}
	s, events, err := readNative(root, filepath.ToSlash(rel), true, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Truncated || len(events) != 2 || len([]rune(s.Title)) != 80 {
		t.Fatalf("bounded session=%+v events=%+v", s, events)
	}
	var payload map[string]any
	if err := json.Unmarshal(events[1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["input"] != "not json" {
		t.Fatalf("malformed arguments: %+v", payload)
	}
}

func TestNativeSymlinksAreIgnored(t *testing.T) {
	home, outside := t.TempDir(), t.TempDir()
	path := writeRollout(t, outside, testNativeID, nativeMeta(testNativeID)+nativeMessage("user", "outside"))
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(path), filepath.Join(home, "sessions", "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(path, filepath.Join(home, "sessions", "rollout-linked.jsonl")); err != nil {
		t.Fatal(err)
	}
	list, err := ListNative(home)
	if err != nil || len(list) != 0 {
		t.Fatalf("symlink list=%+v err=%v", list, err)
	}
	if _, _, err := NativeHistory(home, testNativeID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink history: %v", err)
	}
	root, err := nativeRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if f, err := openNative(root, "../outside.jsonl"); err == nil {
		f.Close()
		t.Fatal("opened outside root")
	}
}

func TestNativeFileLimit(t *testing.T) {
	home := t.TempDir()
	for i := 0; i < nativeFileLimit+1; i++ {
		writeRollout(t, home, fmt.Sprintf("%05d", i), "")
	}
	root, err := nativeRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	files, err := nativeFiles(root)
	if err != nil || len(files) != nativeFileLimit {
		t.Fatalf("files=%d err=%v", len(files), err)
	}
}

func TestNativeMetadataTailAndStructuredTools(t *testing.T) {
	home := t.TempDir()
	data := nativeMeta(testNativeID) + nativeMessage("user", "Original synthetic prompt")
	data += strings.Repeat("not a record\n", 50000)
	data += `{"type":"event_msg","timestamp":"2026-10-24T04:00:00Z","payload":{"type":"session_renamed","session_title":"Tail title"}}` + "\n"
	data += `{"type":"response_item","payload":{"type":"function_call","call_id":"structured","name":"test","arguments":{"a":1}}}` + "\n"
	data += `{"type":"response_item","payload":{"type":"function_call_output","call_id":"structured","output":[{"text":"structured result"}]}}` + "\n"
	writeRollout(t, home, testNativeID, data)
	list, err := ListNative(home)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if list[0].Title != "Tail title" || list[0].UpdatedAt != "2026-10-24T04:00:00Z" || list[0].Truncated {
		t.Fatalf("metadata: %+v", list[0])
	}
	s, events, err := NativeHistory(home, testNativeID)
	if err != nil || s.Title != "Tail title" || len(events) != 3 {
		t.Fatalf("session=%+v events=%+v err=%v", s, events, err)
	}
	var tool map[string]any
	if err := json.Unmarshal(events[1].Payload, &tool); err != nil {
		t.Fatal(err)
	}
	if tool["input"].(map[string]any)["a"] != float64(1) {
		t.Fatalf("structured input: %+v", tool)
	}
	var result map[string]any
	if err := json.Unmarshal(events[2].Payload, &result); err != nil {
		t.Fatal(err)
	}
	if result["is_error"] != false || len(result["content"].([]any)) != 1 {
		t.Fatalf("structured output: %+v", result)
	}
}

func TestNativeDefaultHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	// UserHomeDir uses USERPROFILE on Windows and HOME on Unix.
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	writeRollout(t, filepath.Join(home, ".codex"), testNativeID, nativeMeta(testNativeID))
	list, err := ListNative("")
	if err != nil || len(list) != 1 || list[0].ID != testNativeID {
		t.Fatalf("default home=%+v err=%v", list, err)
	}
}

func TestNativeMetadataCacheFingerprint(t *testing.T) {
	home := t.TempDir()
	path := writeRollout(t, home, "cache", nativeMeta(testNativeID)+nativeMessage("user", "First title"))
	root, err := nativeRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	rel, err := filepath.Rel(home, path)
	if err != nil {
		t.Fatal(err)
	}
	cache := new(nativeMetadataStore)
	first, err := cache.metadata(root, rel)
	if err != nil {
		t.Fatal(err)
	}
	entry := cache.order.Front()
	again, err := cache.metadata(root, rel)
	if err != nil || !reflect.DeepEqual(first, again) || cache.order.Front() != entry {
		t.Fatalf("unchanged file was not reused: %+v %v", again, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// An equal-size rewrite must invalidate on mtime alone.
	writeRollout(t, home, "cache", nativeMeta(testNativeID)+nativeMessage("user", "Other title"))
	changed := info.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	updated, err := cache.metadata(root, rel)
	if err != nil || updated.Title != "Other title" || cache.order.Front() == entry {
		t.Fatalf("mtime invalidation: %+v %v", updated, err)
	}
	// Growth must invalidate even when the timestamp is unchanged.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString(`{"type":"session_title","payload":{"title":"Appended title"}}` + "\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append: %v %v", writeErr, closeErr)
	}
	if err := os.Chtimes(path, changed, changed); err != nil {
		t.Fatal(err)
	}
	updated, err = cache.metadata(root, rel)
	if err != nil || updated.Title != "Appended title" {
		t.Fatalf("size invalidation: %+v %v", updated, err)
	}
	// Deletion is checked before reusing cached metadata.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.metadata(root, rel); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file: %v", err)
	}
}

func TestNativePollingSeesGrowth(t *testing.T) {
	home := t.TempDir()
	path := writeRollout(t, home, "renamed", nativeMeta(testNativeID)+nativeMessage("user", "First prompt"))
	for i := 0; i < 2; i++ {
		if sessions, err := ListNative(home); err != nil || len(sessions) != 1 || sessions[0].Title != "First prompt" {
			t.Fatalf("initial list=%+v err=%v", sessions, err)
		}
		if _, events, err := NativeHistory(home, testNativeID); err != nil || len(events) != 1 {
			t.Fatalf("initial history=%+v err=%v", events, err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString(nativeMessage("assistant", "New reply") + `{"type":"session_title","payload":{"title":"New title"}}` + "\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append: %v %v", writeErr, closeErr)
	}
	sessions, err := ListNative(home)
	if err != nil || len(sessions) != 1 || sessions[0].Title != "New title" {
		t.Fatalf("updated list=%+v err=%v", sessions, err)
	}
	s, events, err := NativeHistory(home, testNativeID)
	if err != nil || s.Title != "New title" || len(events) != 2 || events[1].Text != "New reply" {
		t.Fatalf("updated session=%+v events=%+v err=%v", s, events, err)
	}
}

func TestNativeMetadataCacheHomeIsolationAndBound(t *testing.T) {
	cache := new(nativeMetadataStore)
	for _, title := range []string{"Home one", "Home two"} {
		home := t.TempDir()
		path := writeRollout(t, home, "same-name", nativeMeta(testNativeID)+nativeMessage("user", title))
		root, err := nativeRoot(home)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			root.Close()
			t.Fatal(err)
		}
		s, err := cache.metadata(root, rel)
		root.Close()
		if err != nil || s.Title != title {
			t.Fatalf("home isolation: %+v %v", s, err)
		}
	}
	if len(cache.entries) != 2 {
		t.Fatalf("cache keys did not separate homes: %d", len(cache.entries))
	}
	// Seed old entries to exercise eviction without generating thousands more files.
	oldest := cache.order.Front().Value.(nativeMetadataEntry).key
	for len(cache.entries) < nativeFileLimit {
		key := nativeMetadataKey{home: "synthetic", path: fmt.Sprint(len(cache.entries))}
		cache.entries[key] = cache.order.PushBack(nativeMetadataEntry{key: key})
	}
	home := t.TempDir()
	path := writeRollout(t, home, "new", nativeMeta(testNativeID))
	root, err := nativeRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	rel, err := filepath.Rel(home, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.metadata(root, rel); err != nil {
		t.Fatal(err)
	}
	if len(cache.entries) != nativeFileLimit || cache.order.Len() != nativeFileLimit || cache.entries[oldest] != nil {
		t.Fatalf("cache bound or eviction failed: %d", len(cache.entries))
	}
}

func TestNativeHistoryPrioritizesUUIDFilename(t *testing.T) {
	home := t.TempDir()
	unrelated := writeRollout(t, home, "000-unrelated", nativeMeta("22345678-abcd-4321-9876-123456789abc"))
	writeRollout(t, home, "999-"+testNativeID, nativeMeta(testNativeID)+nativeMessage("user", "Target"))
	s, _, err := NativeHistory(home, strings.ToUpper(testNativeID))
	if err != nil || s.Title != "Target" {
		t.Fatalf("target=%+v err=%v", s, err)
	}
	rel, err := filepath.Rel(home, unrelated)
	if err != nil {
		t.Fatal(err)
	}
	nativeMetadataCache.Lock()
	_, parsedUnrelated := nativeMetadataCache.entries[nativeMetadataKey{home: home, path: filepath.ToSlash(rel)}]
	nativeMetadataCache.Unlock()
	if parsedUnrelated {
		t.Fatal("parsed unrelated metadata before matching filename")
	}
	// A filename match alone must not authorize returning a different session.
	writeRollout(t, home, "999-"+testNativeID, nativeMeta("32345678-abcd-4321-9876-123456789abc"))
	writeRollout(t, home, "fallback", nativeMeta(testNativeID)+nativeMessage("user", "Fallback"))
	s, _, err = NativeHistory(home, testNativeID)
	if err != nil || s.Title != "Fallback" {
		t.Fatalf("metadata fallback=%+v err=%v", s, err)
	}
}

package claude

import (
	"strconv"
	"strings"
	"testing"
)

func TestNativeFallbackTitleUnwrapsLongPaste(t *testing.T) {
	want := "分析一下 " + strings.Repeat("中", 75)
	text := "<pasted_content id=\"test\">\n分析一下\n" + strings.Repeat("中", 90) + "\n</pasted_content id=\"test\">"
	s := NativeSession{Title: "Claude session"}
	inspectMetadata([]byte(`{"type":"user","message":{"content":`+strconv.Quote(text)+`}}`), &s)
	if s.Title != want {
		t.Fatalf("title = %q, want %q", s.Title, want)
	}
}

func TestNativeAITitleAndCustomTitlePrecedence(t *testing.T) {
	s := NativeSession{Title: "Claude session"}
	inspectMetadata([]byte(`{"type":"user","message":{"content":"first prompt"}}`), &s)
	inspectMetadata([]byte(`{"type":"ai-title","aiTitle":"原会话自动标题"}`), &s)
	if s.Title != "原会话自动标题" {
		t.Fatal(s.Title)
	}
	inspectMetadata([]byte(`{"type":"custom-title","customTitle":"用户重命名"}`), &s)
	inspectMetadata([]byte(`{"type":"ai-title","aiTitle":"后续自动标题"}`), &s)
	if s.Title != "用户重命名" {
		t.Fatal(s.Title)
	}
}

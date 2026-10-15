package claude

import "testing"

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

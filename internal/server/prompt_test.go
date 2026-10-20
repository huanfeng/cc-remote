package server

import (
	"testing"

	"cc-remote/internal/claude"
)

// A queued web message matches its transcript entry, which shows the
// attachment list as attachments rather than text.
func TestNormalizePromptAttachments(t *testing.T) {
	prompt := "看看截图\r\n" + claude.AttachmentNote + `{"path":"/u/f6469fde-8dd6-4dc0-ba72-08cff7b350c7/3fa9ca23-c3e1-4cf9-b992-2bad76d7fdf4.png","name":"a.png","mime":"image/png","size":1}`
	text, _ := claude.SplitAttachments(prompt)
	if got := normalizePrompt(prompt); got != "看看截图" || normalizePrompt(text) != got {
		t.Fatalf("%q vs %q", got, normalizePrompt(text))
	}
}

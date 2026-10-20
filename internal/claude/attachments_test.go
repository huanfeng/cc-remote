package claude

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	testUploadSession = "f6469fde-8dd6-4dc0-ba72-08cff7b350c7"
	testUpload        = "3fa9ca23-c3e1-4cf9-b992-2bad76d7fdf4"
)

func attachedPrompt(text string, paths ...string) string {
	var lines []string
	for i, p := range paths {
		line, _ := json.Marshal(map[string]any{"path": p, "name": []string{"shot.png", "需求说明.txt"}[i%2], "mime": []string{"image/png", "text/plain"}[i%2], "size": 79})
		lines = append(lines, string(line))
	}
	return text + AttachmentNote + strings.Join(lines, "\n")
}

// A web message's attachment list becomes the attachments the web shows, with
// the upload's own session in their url, from a Windows or a Unix host.
func TestSplitAttachments(t *testing.T) {
	prompt := attachedPrompt("看看这两个",
		`C:\Users\me\AppData\Roaming\cc-remote\uploads\`+testUploadSession+`\`+testUpload+`.png`,
		"/home/me/.config/cc-remote/uploads/"+testUploadSession+"/"+testUpload+"-需求说明.txt")
	text, payload := SplitAttachments(strings.ReplaceAll(prompt, "\n", "\r\n"))
	var got struct{ Attachments []promptAttachment }
	if text != "看看这两个" || json.Unmarshal(payload, &got) != nil || len(got.Attachments) != 2 {
		t.Fatalf("text %q payload %s", text, payload)
	}
	url := "/api/sessions/" + testUploadSession + "/uploads/" + testUpload
	if a := got.Attachments[0]; a.ID != testUpload || a.Kind != "image" || a.Name != "shot.png" || a.MIME != "image/png" || a.Size != 79 || a.URL != url {
		t.Fatalf("image %+v", a)
	}
	if a := got.Attachments[1]; a.Kind != "file" || a.Name != "需求说明.txt" || a.URL != url {
		t.Fatalf("file %+v", a)
	}
	// Anything else stays text.
	for _, p := range []string{"plain text", attachedPrompt("x", "/etc/passwd"), "x" + AttachmentNote + "not json"} {
		if text, payload := SplitAttachments(p); text != p || payload != nil {
			t.Fatalf("%q: %q %s", p, text, payload)
		}
	}
}

// The transcript shows a web message as sent: its text with its attachments.
func TestNativeHistoryAttachments(t *testing.T) {
	prompt := attachedPrompt("截图里是什么？", "/data/uploads/"+testUploadSession+"/"+testUpload+".png")
	line, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": prompt}})
	dir := t.TempDir()
	writeNative(t, dir, "-tmp-attach", string(line)+"\n")
	_, events, err := NativeHistory(dir, testID)
	if err != nil || len(events) != 1 {
		t.Fatalf("%v %+v", err, events)
	}
	if e := events[0]; e.Kind != "user" || e.Text != "截图里是什么？" || !strings.Contains(string(e.Payload), `"kind":"image"`) {
		t.Fatalf("%+v %s", e, e.Payload)
	}
}

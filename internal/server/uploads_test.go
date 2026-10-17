package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

func uploadImage(t *testing.T, ts *httptest.Server, sessionID, token string, data []byte) (int, []byte) {
	t.Helper()
	return uploadFile(t, ts, sessionID, token, "screenshot.png", data)
}

func uploadFile(t *testing.T, ts *httptest.Server, sessionID, token, name string, data []byte) (int, []byte) {
	t.Helper()
	var b bytes.Buffer
	m := multipart.NewWriter(&b)
	f, err := m.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(data)
	_ = m.Close()
	r, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sessionID+"/uploads", &b)
	r.Header.Set("Content-Type", m.FormDataContentType())
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestImageUploadsAreAuthenticatedValidatedAndSessionScoped(t *testing.T) {
	runs := make(chan claude.Options, 1)
	s, ts, st := setup(t, func(_ context.Context, o claude.Options, _ func(claude.Event) error) error { runs <- o; return nil })
	s.cfg.UploadDir = t.TempDir()
	a, other := createSession(t, ts), createSession(t, ts)
	pngBytes := tinyPNG(t)
	if c, _ := uploadImage(t, ts, a.ID, "", pngBytes); c != 401 {
		t.Fatalf("anonymous upload %d", c)
	}
	for _, bad := range [][]byte{append(pngBytes[:20:20], []byte("broken")...), make([]byte, maxUploadBytes+1)} {
		if c, _ := uploadImage(t, ts, a.ID, testToken, bad); c != 400 {
			t.Fatalf("invalid image accepted: %d", c)
		}
	}
	c, b := uploadImage(t, ts, a.ID, testToken, pngBytes)
	if c != 201 {
		t.Fatalf("upload: %d %s", c, b)
	}
	var item attachment
	if err := json.Unmarshal(b, &item); err != nil {
		t.Fatal(err)
	}
	if item.MIME != "image/png" || item.Kind != "image" || item.Size != len(pngBytes) || item.ID == "" {
		t.Fatalf("bad metadata %+v", item)
	}
	if c, _ = request(t, ts, "GET", item.URL, nil, ""); c != 401 {
		t.Fatal("anonymous image read")
	}
	if c, b = request(t, ts, "GET", item.URL, nil, testToken); c != 200 || !bytes.Equal(b, pngBytes) {
		t.Fatal("image did not roundtrip")
	}
	if c, _ = request(t, ts, "GET", "/api/sessions/"+other.ID+"/uploads/"+item.ID, nil, testToken); c != 404 {
		t.Fatal("cross-session image read")
	}
	if c, _ = request(t, ts, "POST", "/api/sessions/"+other.ID+"/messages", map[string]any{"text": "analyze", "attachmentIds": []string{item.ID}}, testToken); c != 400 {
		t.Fatal("cross-session image message")
	}
	if _, _, err := s.messageAttachments(i18n.EN, a.ID, "", []string{"../outside"}); err == nil {
		t.Fatal("path traversal image accepted")
	}
	if _, _, err := s.messageAttachments(i18n.EN, a.ID, "", []string{item.ID, item.ID}); err == nil {
		t.Fatal("duplicate image accepted")
	}
	if c, b = request(t, ts, "POST", "/api/sessions/"+a.ID+"/messages", map[string]any{"text": "", "attachmentIds": []string{item.ID}}, testToken); c != 202 {
		t.Fatalf("image-only message: %d %s", c, b)
	}
	o := <-runs
	_, imagePath, _ := s.loadAttachment(i18n.EN, a.ID, item.ID)
	encodedPath, _ := json.Marshal(imagePath)
	if !strings.Contains(o.Message, string(encodedPath)) || !strings.Contains(o.Message, "Read tool") {
		t.Fatalf("image not supplied to Claude: %s", o.Message)
	}
	if _, err := os.Stat(imagePath); err != nil {
		t.Fatal(err)
	}
	events, err := st.Events(a.ID, 0)
	if err != nil || len(events) < 1 || events[0].Text != "" || !bytes.Contains(events[0].Payload, []byte(item.ID)) {
		t.Fatalf("missing persistent attachment: %+v %v", events, err)
	}
	// Metadata and image remain readable after constructing a fresh server.
	reopened := New(st, Config{UploadDir: s.cfg.UploadDir})
	if _, path, err := reopened.loadAttachment(i18n.EN, a.ID, item.ID); err != nil || path != filepath.Clean(imagePath) {
		t.Fatalf("not persistent: %s %v", path, err)
	}
}

func TestChannelImageMessageUsesExistingConnection(t *testing.T) {
	s, ts, _ := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error {
		t.Error("spawned managed Claude for channel image")
		return nil
	})
	s.cfg.UploadDir = t.TempDir()
	id := store.ID()
	a, key := registerChannel(t, ts, id, t.TempDir())
	c, b := uploadImage(t, ts, a.ID, testToken, tinyPNG(t))
	if c != 201 {
		t.Fatalf("upload: %d %s", c, b)
	}
	var item attachment
	_ = json.Unmarshal(b, &item)
	if c, b = request(t, ts, "POST", "/api/sessions/"+a.ID+"/messages", map[string]any{"text": "看这张截图", "attachmentIds": []string{item.ID}}, testToken); c != 202 {
		t.Fatalf("send: %d %s", c, b)
	}
	c, b = channelReq(t, ts, "GET", "/api/channels/"+id+"/commands?after=0", key, nil)
	if c != 200 || !bytes.Contains(b, []byte(item.ID)) || !bytes.Contains(b, []byte("Read tool")) {
		t.Fatalf("missing image command: %d %s", c, b)
	}
}

func TestSanitizeUploadName(t *testing.T) {
	long := strings.Repeat("长", 80) + ".pdf"
	for raw, want := range map[string]string{
		"report.pdf":             "report.pdf",
		"../../etc/passwd":       "passwd",
		`C:\Users\me\secret.txt`: "secret.txt",
		"a\x00b\x1f<c>:\"|?*.md": "abc.md",
		" .hidden. ":             "hidden",
		"..":                     "file",
		"":                       "file",
		"/":                      "file",
		"name\u202e.txt":         "name.txt",
	} {
		if got := sanitizeUploadName(raw); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", raw, got, want)
		}
	}
	got := sanitizeUploadName(long)
	if len(got) > maxUploadNameBytes || !strings.HasSuffix(got, ".pdf") || !utf8.ValidString(got) {
		t.Fatalf("long name %q (%d bytes)", got, len(got))
	}
}

func TestFileUploadsAreStoredAsIsAndDownloaded(t *testing.T) {
	runs := make(chan claude.Options, 1)
	s, ts, _ := setup(t, func(_ context.Context, o claude.Options, _ func(claude.Event) error) error { runs <- o; return nil })
	s.cfg.UploadDir = t.TempDir()
	a := createSession(t, ts)
	text := []byte("hello from a text file\n")
	c, b := uploadFile(t, ts, a.ID, testToken, "../../笔记 notes.txt", text)
	if c != 201 {
		t.Fatalf("text upload: %d %s", c, b)
	}
	var txt attachment
	_ = json.Unmarshal(b, &txt)
	if txt.Kind != "file" || txt.Name != "笔记 notes.txt" || !strings.HasPrefix(txt.MIME, "text/plain") || txt.Size != len(text) {
		t.Fatalf("bad file metadata %+v", txt)
	}
	_, path, err := s.loadAttachment(i18n.EN, a.ID, txt.ID)
	if err != nil || filepath.Dir(path) != filepath.Join(s.cfg.UploadDir, a.ID) || !strings.HasSuffix(path, "-笔记 notes.txt") {
		t.Fatalf("stored outside session dir: %s %v", path, err)
	}
	r, _ := http.NewRequest("GET", ts.URL+txt.URL, nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	res, err := ts.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !bytes.Equal(got, text) || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment;") || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download %d %q %v", res.StatusCode, got, res.Header)
	}
	// HTML and SVG are never served with a renderable type.
	for name, data := range map[string]string{"page.html": "<html><script>alert(1)</script></html>", "x.svg": `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`} {
		c, b = uploadFile(t, ts, a.ID, testToken, name, []byte(data))
		var item attachment
		_ = json.Unmarshal(b, &item)
		if c != 201 || item.Kind != "file" {
			t.Fatalf("%s upload %d %s", name, c, b)
		}
		r, _ := http.NewRequest("GET", ts.URL+item.URL, nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		res, err := ts.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment;") {
			t.Fatalf("%s served as %v", name, res.Header)
		}
	}
	if c, _ = uploadFile(t, ts, a.ID, testToken, "big.bin", make([]byte, maxUploadBytes+1)); c != 400 {
		t.Fatalf("oversized file accepted: %d", c)
	}
	if c, b = request(t, ts, "POST", "/api/sessions/"+a.ID+"/messages", map[string]any{"text": "read it", "attachmentIds": []string{txt.ID}}, testToken); c != 202 {
		t.Fatalf("send: %d %s", c, b)
	}
	o := <-runs
	encoded, _ := json.Marshal(path)
	if !strings.Contains(o.Message, string(encoded)) || !strings.Contains(o.Message, "Read tool") || !strings.Contains(o.Message, `"name":"笔记 notes.txt"`) {
		t.Fatalf("file not supplied to Claude: %s", o.Message)
	}
	// Metadata tampered to escape the session directory is rejected.
	_, metaPath, _ := s.uploadPaths(i18n.EN, a.ID, txt.ID)
	_ = os.WriteFile(metaPath, []byte(`{"id":"`+txt.ID+`","kind":"file","name":"../../outside.txt","mime":"text/plain"}`), 0600)
	if _, _, err = s.loadAttachment(i18n.EN, a.ID, txt.ID); err == nil {
		t.Fatal("traversal name in metadata accepted")
	}
}

func TestAttachmentCountLimit(t *testing.T) {
	s, ts, _ := setup(t, func(context.Context, claude.Options, func(claude.Event) error) error { return nil })
	s.cfg.UploadDir = t.TempDir()
	a := createSession(t, ts)
	ids := []string{}
	for i := 0; i <= maxAttachments; i++ {
		c, b := uploadFile(t, ts, a.ID, testToken, "f.txt", []byte("x"))
		var item attachment
		_ = json.Unmarshal(b, &item)
		if c != 201 {
			t.Fatalf("upload %d %s", c, b)
		}
		ids = append(ids, item.ID)
	}
	if _, _, err := s.messageAttachments(i18n.EN, a.ID, "", ids); err == nil {
		t.Fatal("more than 8 attachments accepted")
	}
	if _, _, err := s.messageAttachments(i18n.EN, a.ID, "", ids[:maxAttachments]); err != nil {
		t.Fatal(err)
	}
}

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A file too large for the viewer downloads through a link: asked for with
// the token, saved without it, streamed whole or in ranges, and only for a
// file the session may open.
func TestFileDownloadLink(t *testing.T) {
	s, ts, st := setup(t, nil)
	cwd := filepath.Join(t.TempDir(), "a", "b", "app")
	big := bytes.Repeat([]byte("0123456789abcdef"), (maxViewBytes/16)+1024) // over the viewer's cap
	if err := os.MkdirAll(filepath.Join(cwd, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "out", "app release.apk"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(filepath.Dir(cwd), "secret.txt"), "secret")
	sess, err := st.Create("s", cwd, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, _ := getFile(t, ts.URL, sess.ID, "out/app release.apk"); code != 413 {
		t.Fatalf("viewer: %d, want 413", code)
	}
	link := func(path, token string) (int, map[string]any) {
		req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sess.ID+"/file/link?"+url.Values{"path": {path}}.Encode(), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, _ := link("out/app release.apk", "wrong-token-wrong-token"); code != 401 {
		t.Fatalf("link without the token: %d", code)
	}
	if code, _ := link("../secret.txt", testToken); code != 403 {
		t.Fatalf("link outside the session: %d", code)
	}
	code, out := link("out/app release.apk", testToken)
	href, _ := out["url"].(string)
	if code != 200 || !strings.HasPrefix(href, "/api/download/") || out["name"] != "app release.apk" || out["size"] != float64(len(big)) {
		t.Fatalf("link: %d %v", code, out)
	}
	// Saved without the token, whole and in a range.
	resp, err := http.Get(ts.URL + href)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(body, big) || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") || !strings.Contains(resp.Header.Get("Content-Disposition"), "app release.apk") || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("download: %d %d bytes %v", resp.StatusCode, len(body), resp.Header)
	}
	req, _ := http.NewRequest("GET", ts.URL+href, nil)
	req.Header.Set("Range", "bytes=16-31")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "0123456789abcdef" {
		t.Fatalf("range: %d %q", resp.StatusCode, body)
	}
	// An unknown or expired ticket opens nothing.
	if resp, _ = http.Get(ts.URL + "/api/download/" + strings.Repeat("0", 48)); resp.StatusCode != 404 {
		t.Fatalf("unknown ticket: %d", resp.StatusCode)
	}
	s.downloads.mu.Lock()
	for k, v := range s.downloads.m {
		v.expires = time.Now().Add(-time.Second)
		s.downloads.m[k] = v
	}
	s.downloads.mu.Unlock()
	if resp, _ = http.Get(ts.URL + href); resp.StatusCode != 404 {
		t.Fatalf("expired ticket: %d", resp.StatusCode)
	}
}

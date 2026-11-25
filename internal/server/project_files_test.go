package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestProjectFileBrowser(t *testing.T) {
	_, ts, st := setup(t, nil)
	root := t.TempDir()
	cwd := filepath.Join(root, "app")
	writeFile(t, filepath.Join(cwd, "docs", "测试 file #1.md"), "# app")
	writeFile(t, filepath.Join(cwd, "README.md"), "# readme")
	writeFile(t, filepath.Join(cwd, ".git", "config"), "internal")
	writeFile(t, filepath.Join(root, "other", "readme.md"), "other")
	if err := os.Mkdir(filepath.Join(cwd, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	sess, err := st.Create("s", cwd, "", false)
	if err != nil {
		t.Fatal(err)
	}
	// Existing conversation links can still open a named sibling directory.
	if _, err := st.Append(sess.ID, "assistant", filepath.Join(root, "other"), nil); err != nil {
		t.Fatal(err)
	}

	base := "/api/sessions/" + sess.ID
	projectQuery := func(path string) string {
		return "?" + (url.Values{"path": {path}, "scope": {"project"}}).Encode()
	}
	for _, c := range []struct {
		path string
		want []projectEntry
	}{
		{"", []projectEntry{{"docs", "docs", "directory"}, {"empty", "empty", "directory"}, {"README.md", "README.md", "file"}}},
		{"docs", []projectEntry{{"测试 file #1.md", "docs/测试 file #1.md", "file"}}},
		{"empty", []projectEntry{}},
	} {
		status, body := request(t, ts, "GET", base+"/project-files"+projectQuery(c.path), nil, testToken)
		var listing projectListing
		if status != 200 || json.Unmarshal(body, &listing) != nil || !reflect.DeepEqual(listing.Entries, c.want) || listing.Truncated {
			t.Fatalf("listing %q: %d %s", c.path, status, body)
		}
	}
	for _, c := range []struct {
		endpoint, relative, token string
		want                      int
	}{
		{"/project-files", "", "", 401},
		{"/project-files", "../other", testToken, 400},
		{"/project-files", "/docs", testToken, 400},
		{"/project-files", "C:/docs", testToken, 400},
		{"/project-files", "//server/share", testToken, 400},
		{"/project-files", "docs/.", testToken, 400},
		{"/project-files", ".git", testToken, 404},
		{"/project-files", "README.md", testToken, 404},
		{"/file", "../other/readme.md", testToken, 400},
		{"/file", ".git/config", testToken, 404},
		{"/file", "docs", testToken, 404},
	} {
		if status, body := request(t, ts, "GET", base+c.endpoint+projectQuery(c.relative), nil, c.token); status != c.want {
			t.Errorf("%s %q: %d %s; want %d", c.endpoint, c.relative, status, body, c.want)
		}
	}
	relative := "docs/测试 file #1.md"
	req, _ := http.NewRequest("GET", ts.URL+base+"/file"+projectQuery(relative), nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	response, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	wantPath := filepath.Join(cwd, filepath.FromSlash(relative))
	previewPath, _ := url.PathUnescape(response.Header.Get("X-CC-Path"))
	if response.StatusCode != 200 || previewPath != wantPath {
		t.Fatalf("preview: %d %q; want %q", response.StatusCode, previewPath, wantPath)
	}
	if status, body := request(t, ts, "GET", base+"/file?path=../other/readme.md", nil, testToken); status != 200 || string(body) != "other" {
		t.Fatalf("conversation links changed: %d %s", status, body)
	}

	status, body := request(t, ts, "POST", base+"/file/link"+projectQuery(relative), nil, testToken)
	var ticket struct{ URL, Name string }
	if status != 200 || json.Unmarshal(body, &ticket) != nil || ticket.Name != filepath.Base(wantPath) {
		t.Fatalf("download ticket: %d %s", status, body)
	}
	if status, body := request(t, ts, "GET", ticket.URL, nil, ""); status != 200 || string(body) != "# app" {
		t.Fatalf("scoped download: %d %s", status, body)
	}
	t.Run("a stale ticket rechecks the directory", func(t *testing.T) {
		if err := os.Rename(filepath.Join(cwd, "docs"), filepath.Join(cwd, "moved")); err != nil {
			t.Fatal(err)
		}
		projectTestLink(t, filepath.Join(cwd, "docs"), filepath.Join(cwd, "moved"))
		if status, body := request(t, ts, "GET", ticket.URL, nil, ""); status != 404 {
			t.Fatalf("stale ticket opened a linked file: %d %s", status, body)
		}
	})
}

func TestProjectFilesSkipLinks(t *testing.T) {
	cwd := t.TempDir()
	inside := filepath.Join(cwd, "inside")
	outside := t.TempDir()
	writeFile(t, filepath.Join(inside, "hello.txt"), "hello")
	writeFile(t, filepath.Join(outside, "secret.txt"), "secret")
	for name, target := range map[string]string{"inside-link": inside, "outside-link": outside} {
		projectTestLink(t, filepath.Join(cwd, name), target)
		if f, _, _, err := openProjectPath(cwd, name, true); err == nil {
			f.Close()
			t.Errorf("listed symlink directory %s", name)
		}
		leaf := "hello.txt"
		if target == outside {
			leaf = "secret.txt"
		}
		if f, _, _, err := openProjectPath(cwd, name+"/"+leaf, false); err == nil {
			f.Close()
			t.Errorf("opened file through symlink directory %s", name)
		}
	}
	dir, _, _, err := openProjectPath(cwd, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	listing, err := readProjectEntries(dir, "")
	if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Path != "inside" {
		t.Fatalf("listing: %+v %v", listing, err)
	}
}

func projectTestLink(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Skipf("mklink: %v %s", err, out)
		}
	} else if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

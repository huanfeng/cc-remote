package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"cc-remote/internal/store"
)

func getFile(t *testing.T, base, id, path string, bases ...string) (int, string, http.Header) {
	t.Helper()
	q := url.Values{"path": {path}, "base": bases}
	req, _ := http.NewRequest("GET", base+"/api/sessions/"+id+"/file?"+q.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp.Header
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Files open from the working directory and from directories the session
// named; anything else is refused.
func TestSessionFile(t *testing.T) {
	_, ts, st := setup(t, nil)
	// Three levels deep, so no directory counts as too broad to open.
	root := filepath.Join(t.TempDir(), "a", "b")
	cwd, worktree, other := filepath.Join(root, "app"), filepath.Join(root, "wt"), filepath.Join(root, "other")
	writeFile(t, filepath.Join(cwd, "README.md"), "# app")
	writeFile(t, filepath.Join(worktree, "docs", "design.md"), "# design")
	writeFile(t, filepath.Join(worktree, "img", "a.png"), "png")
	writeFile(t, filepath.Join(other, "secret.txt"), "secret")
	writeFile(t, filepath.Join(other, "named", "x.go"), "package x")
	sess, err := st.Create("s", cwd, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(sess.ID, "assistant", "worktree 在 "+worktree+"，提交", nil); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]any{"name": "Read", "input": map[string]string{"file_path": filepath.Join(other, "named", "x.go")}})
	if _, err := st.Append(sess.ID, "tool_use", "", input); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, path string
		bases      []string
		status     int
		body       string
	}{
		{"working directory", "README.md", nil, 200, "# app"},
		{"relative to a named directory", "docs/design.md", []string{worktree}, 200, "# design"},
		{"absolute in a named directory", filepath.Join(worktree, "docs", "design.md"), nil, 200, "# design"},
		{"directory of a named file", filepath.Join(other, "named", "x.go"), nil, 200, "package x"},
		{"outside", filepath.Join(other, "secret.txt"), nil, 403, ""},
		{"escape with ..", "../other/secret.txt", nil, 403, ""},
		{"unnamed base", "secret.txt", []string{other}, 403, ""},
		{"missing", "nope.md", nil, 404, ""},
		{"directory", "docs", []string{worktree}, 404, ""},
	}
	for _, c := range cases {
		status, body, h := getFile(t, ts.URL, sess.ID, c.path, c.bases...)
		if status != c.status || (c.body != "" && body != c.body) {
			t.Errorf("%s: %d %q", c.name, status, body)
		}
		if status == 200 && h.Get("X-CC-Path") == "" {
			t.Errorf("%s: no X-CC-Path", c.name)
		}
	}
	if _, _, h := getFile(t, ts.URL, sess.ID, "img/a.png", worktree); h.Get("Content-Type") != "image/png" {
		t.Errorf("image type %q", h.Get("Content-Type"))
	}
	if _, _, h := getFile(t, ts.URL, sess.ID, "README.md"); h.Get("Content-Type") != "application/octet-stream" {
		t.Errorf("text type %q", h.Get("Content-Type"))
	}

	// A symlink inside a root does not lead outside it.
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(other, "secret.txt"), filepath.Join(cwd, "link.txt")); err != nil {
			t.Fatal(err)
		}
		if status, _, _ := getFile(t, ts.URL, sess.ID, "link.txt"); status != 403 {
			t.Errorf("symlink out: %d", status)
		}
	}
}

func TestMentionedPaths(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"see /home/me/proj/a.go:12 now", "/home/me/proj/a.go"},
		{"worktree 在 /home/me/wt，提交", "/home/me/wt"},
		{"`/srv/app/x.md`.", "/srv/app/x.md"},
		{`at C:\work\app\main.go`, `C:\work\app\main.go`},
	} {
		got := mentionedPaths(storeEvent(c.text))
		if len(got) == 0 || got[len(got)-1] != c.want {
			t.Errorf("%q: %q", c.text, got)
		}
	}
	if got := mentionedPaths(storeEvent("https://example.com/a/b and and/or")); len(got) != 0 {
		t.Errorf("url matched: %q", got)
	}
}

func TestBroadRoot(t *testing.T) {
	home, _ := os.UserHomeDir()
	for dir, want := range map[string]bool{"/": true, "/home": true, home: true, filepath.Dir(home): true, filepath.Join(home, "proj"): false} {
		if broadRoot(dir) != want {
			t.Errorf("broadRoot(%q) = %v", dir, !want)
		}
	}
}

func storeEvent(text string) store.Event { return store.Event{Text: text} }

// A file named before it is written opens once it exists.
func TestSessionFileNamedEarly(t *testing.T) {
	_, ts, st := setup(t, nil)
	root := filepath.Join(t.TempDir(), "a", "b")
	cwd, later := filepath.Join(root, "app"), filepath.Join(root, "later", "x.md")
	writeFile(t, filepath.Join(cwd, "README.md"), "# app")
	sess, err := st.Create("s", cwd, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Append(sess.ID, "assistant", "I will write "+later, nil); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := getFile(t, ts.URL, sess.ID, "README.md"); status != 200 {
		t.Fatalf("readme: %d", status)
	}
	writeFile(t, later, "# later")
	if status, body, _ := getFile(t, ts.URL, sess.ID, later); status != 200 || body != "# later" {
		t.Fatalf("file written after it was named: %d %q", status, body)
	}
}

// UNC paths are never opened: the host would connect to that machine.
func TestNetworkPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC paths are a Windows thing")
	}
	for _, p := range []string{`\\evil\share\a.png`, `//evil/share/a.png`, `\\?\UNC\evil\share\a`} {
		if !network(filepath.Clean(p), `C:\work`) {
			t.Errorf("%s not seen as a network path", p)
		}
	}
	if network(`C:\work\a.md`, `C:\work`) || network(`\\nas\home\app\a.md`, `\\nas\home\app`) {
		t.Error("local path, or the working directory's own share, refused")
	}
	if _, _, err := openFile(`\\evil\share\a.png`, nil, `C:\work`, []string{`\\evil\share`}); err == nil {
		t.Error("opened a UNC path")
	}
}

// Windows junctions (pnpm's node_modules) resolve like symlinks.
func TestSessionFileJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions are a Windows thing")
	}
	_, ts, st := setup(t, nil)
	root := filepath.Join(t.TempDir(), "a", "b")
	cwd, store := filepath.Join(root, "app"), filepath.Join(root, "app", ".store", "pkg")
	writeFile(t, filepath.Join(store, "README.md"), "# pkg")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(cwd, "linked"), store).CombinedOutput(); err != nil {
		t.Skipf("mklink: %v %s", err, out)
	}
	sess, err := st.Create("s", cwd, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if status, body, _ := getFile(t, ts.URL, sess.ID, `linked\README.md`); status != 200 || body != "# pkg" {
		t.Fatalf("through a junction: %d %q", status, body)
	}
}

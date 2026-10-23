package server

import (
	"encoding/json"
	"net/url"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestMatchMentions(t *testing.T) {
	paths := []string{"README.md", "docs/", "docs/usage.md", "web/", "web/src/", "web/src/App.tsx", "web/src/app.css", "internal/", "internal/server/", "internal/server/mentions.go"}
	for _, c := range []struct {
		q    string
		want []string
	}{
		{"", []string{"docs/", "internal/", "web/", "README.md"}},
		{"app", []string{"web/src/App.tsx", "web/src/app.css"}},
		{"web/src/A", []string{"web/src/App.tsx", "web/src/app.css"}},
		{"ism", []string{"internal/server/mentions.go"}},
		{"docs", []string{"docs/", "docs/usage.md"}},
		{"web/", []string{"web/src/", "web/src/App.tsx", "web/src/app.css"}},
		{"zzz", []string{}},
	} {
		if got := matchMentions(paths, c.q); !slices.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.q, got, c.want)
		}
	}
}

// A git repository offers what git lists, not what it ignores; any other
// directory is walked without hidden directories and node_modules.
func TestMentionsListSessionFiles(t *testing.T) {
	_, ts, st := setup(t, nil)
	ask := func(id, q string) []string {
		code, b := request(t, ts, "GET", "/api/sessions/"+id+"/mentions?"+url.Values{"q": {q}}.Encode(), nil, testToken)
		if code != 200 {
			t.Fatalf("mentions %d %s", code, b)
		}
		var out struct{ Paths []string }
		_ = json.Unmarshal(b, &out)
		return out.Paths
	}
	plain := t.TempDir()
	writeFile(t, filepath.Join(plain, "src", "main.go"), "x")
	writeFile(t, filepath.Join(plain, ".hidden", "secret.go"), "x")
	writeFile(t, filepath.Join(plain, "node_modules", "dep", "index.js"), "x")
	a, err := st.Create("plain", plain, "claude-plain", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := ask(a.ID, ""); !slices.Equal(got, []string{"src/"}) {
		t.Fatalf("plain top level: %v", got)
	}
	if got := ask(a.ID, "go"); !slices.Equal(got, []string{"src/main.go"}) {
		t.Fatalf("plain go: %v", got)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	writeFile(t, filepath.Join(repo, ".gitignore"), "build/\n")
	writeFile(t, filepath.Join(repo, "build", "out.go"), "x")
	writeFile(t, filepath.Join(repo, "cmd", "tool.go"), "x")
	b, err := st.Create("repo", repo, "claude-repo", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := ask(b.ID, "tool"); !slices.Equal(got, []string{"cmd/tool.go"}) {
		t.Fatalf("repo tool: %v", got)
	}
	if got := ask(b.ID, "out.go"); len(got) != 0 {
		t.Fatalf("an ignored file is offered: %v", got)
	}
	if code, _ := request(t, ts, "GET", "/api/sessions/"+b.ID+"/mentions", nil, "wrong-token-wrong-token"); code != 401 {
		t.Fatalf("without the token: %d", code)
	}
}

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestProjectFileSearch(t *testing.T) {
	_, ts, st := setup(t, nil)
	cwd := filepath.Join(t.TempDir(), "project")
	files := []string{
		"README.md", "docs/guide.md", "docs/guide.md.backup", "docs/quick-guide.md", "docs/测试 file #1.md",
		".git/internal-only.txt", "node_modules/pkg/generated-only.js", ".next/generated-only.js",
	}
	for _, path := range files {
		writeFile(t, filepath.Join(cwd, filepath.FromSlash(path)), path)
	}
	writeFile(t, filepath.Join(cwd, "target", "CACHEDIR.TAG"), "Signature: 8a477f597d28d172789f06886806bc55\n# Generated cache\n")
	writeFile(t, filepath.Join(cwd, "target", "debug", "generated-only.js"), "stale output")
	writeFile(t, filepath.Join(cwd, "docs", "CACHEDIR.TAG"), "Documentation about cache tags")
	session, err := st.Create("project", cwd, "", false)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/sessions/" + session.ID + "/project-files/search?q="
	for _, test := range []struct {
		query string
		paths []string
	}{
		{"", []string{"README.md", "docs/guide.md", "docs/CACHEDIR.TAG", "docs/quick-guide.md", "docs/guide.md.backup", "docs/测试 file #1.md"}},
		{"GUIDE.md", []string{"docs/guide.md", "docs/guide.md.backup", "docs/quick-guide.md"}},
		{`.\docs\guide.md`, []string{"docs/guide.md", "docs/guide.md.backup", "docs/quick-guide.md"}},
		{"dc/gdmd", []string{"docs/guide.md", "docs/quick-guide.md", "docs/guide.md.backup"}},
		{"测试 file #1", []string{"docs/测试 file #1.md"}},
		{"generated-only", nil},
		{"internal-only", nil},
		{"missing", nil},
	} {
		status, body := request(t, ts, "GET", endpoint+url.QueryEscape(test.query), nil, testToken)
		var listing projectListing
		if status != http.StatusOK || json.Unmarshal(body, &listing) != nil || listing.Entries == nil || listing.Truncated {
			t.Errorf("query %q: %d %s", test.query, status, body)
			continue
		}
		var got []string
		for _, result := range listing.Entries {
			if result.Kind != "file" || result.Name != filepath.Base(result.Path) {
				t.Errorf("invalid result: %+v", result)
			}
			got = append(got, result.Path)
		}
		if !slices.Equal(got, test.paths) {
			t.Errorf("query %q: got %q, want %q", test.query, got, test.paths)
		}
	}
	if status, body := request(t, ts, "GET", endpoint+strings.Repeat("x", maxProjectSearchQuery+1), nil, testToken); status != http.StatusBadRequest {
		t.Errorf("overlong query: %d %s", status, body)
	}
	if status, body := request(t, ts, "GET", endpoint+"guide", nil, ""); status != http.StatusUnauthorized {
		t.Errorf("anonymous query: %d %s", status, body)
	}
	if status, body := request(t, ts, "GET", "/api/sessions/missing/project-files/search?q=guide", nil, testToken); status != http.StatusNotFound {
		t.Errorf("unknown session: %d %s", status, body)
	}
	if status, body := request(t, ts, "GET", "/api/sessions/"+session.ID+"/project-files?path=target/debug", nil, testToken); status != http.StatusOK || !strings.Contains(string(body), "generated-only.js") {
		t.Errorf("browse cache directory: %d %s", status, body)
	}
}

func TestProjectFileSearchLimitsAndCancellation(t *testing.T) {
	cwd := t.TempDir()
	for i := 0; i <= maxProjectSearchHits; i++ {
		writeFile(t, filepath.Join(cwd, fmt.Sprintf("match.txt-%03d", i)), "match")
	}
	writeFile(t, filepath.Join(cwd, "nested", "match.txt"), "exact name")
	search := func(ctx context.Context) projectListing {
		t.Helper()
		root, err := os.OpenRoot(cwd)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		listing, err := searchProjectFiles(ctx, root, "match.txt")
		if err != nil {
			t.Fatal(err)
		}
		return listing
	}
	listing := search(context.Background())
	if len(listing.Entries) != maxProjectSearchHits || !listing.Truncated || listing.Entries[0].Path != "nested/match.txt" {
		t.Fatalf("limited search: %+v", listing)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if listing := search(ctx); len(listing.Entries) != 0 || !listing.Truncated {
		t.Fatalf("cancelled search: %+v", listing)
	}
}

func TestProjectFileSearchSkipsLinkedDirectories(t *testing.T) {
	cwd := t.TempDir()
	other := t.TempDir()
	writeFile(t, filepath.Join(other, "outside.txt"), "outside")
	writeFile(t, filepath.Join(cwd, "actual", "inside.txt"), "inside")
	projectTestLink(t, filepath.Join(cwd, "elsewhere"), other)
	projectTestLink(t, filepath.Join(cwd, "alias"), filepath.Join(cwd, "actual"))
	root, err := os.OpenRoot(cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	listing, err := searchProjectFiles(context.Background(), root, ".txt")
	if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Path != "actual/inside.txt" || listing.Truncated {
		t.Fatalf("search following links: %+v, %v", listing, err)
	}
	realRoot, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if dir, err := openProjectSearchFolder(root, realRoot, "alias"); err == nil {
		dir.Close()
		t.Fatal("search opened a linked directory queued for traversal")
	}
}

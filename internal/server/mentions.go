package server

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Typing @ in the message box offers the session's files, as Claude Code's own
// prompt does; the chosen @path goes to Claude as written. The files of a git
// repository are what git lists (tracked, and untracked not ignored); any
// other directory is walked, without hidden directories and node_modules.
// A listing serves later keystrokes for mentionFresh.

const (
	mentionFresh    = 15 * time.Second
	maxMentionFiles = 50000
	maxMentionHits  = 30
	maxMentionDirs  = 64 // directories whose listings are kept
)

type mentionListing struct {
	at    time.Time
	paths []string // slash-separated, relative to the directory; directories end in /
}

type mentionIndex struct {
	mu sync.Mutex
	m  map[string]*mentionListing
}

func (x *mentionIndex) listing(dir string) []string {
	x.mu.Lock()
	if l := x.m[dir]; l != nil && time.Since(l.at) < mentionFresh {
		x.mu.Unlock()
		return l.paths
	}
	x.mu.Unlock()
	paths := listFiles(dir)
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.m == nil || len(x.m) >= maxMentionDirs {
		x.m = map[string]*mentionListing{}
	}
	x.m[dir] = &mentionListing{at: time.Now(), paths: paths}
	return paths
}

// listFiles lists dir's files, and the directories they are in.
func listFiles(dir string) []string {
	files, ok := gitFiles(dir)
	if !ok {
		files = walkFiles(dir)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(files)+len(files)/8)
	for _, f := range files {
		for d := path.Dir(f); d != "." && d != "/" && !seen[d]; d = path.Dir(d) {
			seen[d] = true
			out = append(out, d+"/")
		}
		out = append(out, f)
	}
	return out
}

func gitFiles(dir string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	hideWindow(cmd)
	b, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	var files []string
	for _, f := range bytes.Split(b, []byte{0}) {
		if len(f) == 0 {
			continue
		}
		if len(files) >= maxMentionFiles {
			break
		}
		files = append(files, string(f))
	}
	sort.Strings(files)
	return slices.Compact(files), true // a conflicted file is listed once per stage
}

func walkFiles(dir string) []string {
	var files []string
	deadline := time.Now().Add(3 * time.Second)
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || len(files) >= maxMentionFiles || time.Now().After(deadline) {
			if d != nil && d.IsDir() && p != dir {
				return fs.SkipDir
			}
			return nil
		}
		if p == dir {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if rel, err := filepath.Rel(dir, p); err == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files
}

// matchMentions ranks paths for what was typed after @: a name that starts
// with it first, then a name holding it, then a path holding it, then a path
// holding its letters in order; shorter paths first.
func matchMentions(paths []string, query string) []string {
	q := strings.ToLower(strings.TrimPrefix(filepath.ToSlash(query), "./"))
	type hit struct {
		path  string
		score int
	}
	var hits []hit
	for _, p := range paths {
		lower := strings.ToLower(p)
		if lower == q {
			continue // typed in full already
		}
		name := path.Base(strings.TrimSuffix(lower, "/"))
		score := -1
		switch {
		case q == "":
			if strings.Count(strings.TrimSuffix(p, "/"), "/") == 0 {
				score = 0
			}
		case strings.HasPrefix(lower, q):
			score = 0
		case strings.HasPrefix(name, q):
			score = 1
		case strings.Contains(name, q):
			score = 2
		case strings.Contains(lower, q):
			score = 3
		case subsequence(lower, q):
			score = 4
		}
		if score >= 0 {
			hits = append(hits, hit{p, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.score != b.score {
			return a.score < b.score
		}
		if q == "" { // the top level: directories first, then by name
			if da, db := strings.HasSuffix(a.path, "/"), strings.HasSuffix(b.path, "/"); da != db {
				return da
			}
			return strings.ToLower(a.path) < strings.ToLower(b.path)
		}
		if len(a.path) != len(b.path) {
			return len(a.path) < len(b.path)
		}
		return a.path < b.path
	})
	out := make([]string, 0, min(len(hits), maxMentionHits))
	for _, h := range hits[:min(len(hits), maxMentionHits)] {
		out = append(out, h.path)
	}
	return out
}

func subsequence(s, q string) bool {
	for _, r := range q {
		i := strings.IndexRune(s, r)
		if i < 0 {
			return false
		}
		s = s[i+len(string(r)):]
	}
	return true
}

// mentions is GET /api/sessions/{id}/mentions?q=…: the session's files and
// directories matching q, relative to its working directory.
func (s *Server) mentions(w http.ResponseWriter, r *http.Request) {
	sess, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	dir, err := realPath(sess.Cwd)
	if err != nil {
		fail(w, 404, s.lang(r).T("The working directory does not exist", "工作目录不存在"))
		return
	}
	q := r.URL.Query().Get("q")
	if len(q) > 512 {
		q = q[:512]
	}
	respond(w, 200, map[string]any{"paths": matchMentions(s.mentionIndex.listing(dir), q)})
}

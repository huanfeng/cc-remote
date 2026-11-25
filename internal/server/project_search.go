package server

import (
	"cmp"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	maxProjectSearchQuery   = 512
	maxProjectSearchHits    = 80
	maxProjectSearchScan    = 25000
	maxProjectSearchFolders = 3000
	projectSearchTimeout    = 3 * time.Second
	projectCacheTag         = "CACHEDIR.TAG"
	projectCacheSignature   = "Signature: 8a477f597d28d172789f06886806bc55"
)

type projectSearchHit struct {
	entry projectEntry
	lower string
	score int
}

type projectSearch struct {
	query     string
	remaining int
	folders   []string
	hits      []projectSearchHit
	truncated bool
}

func projectFileScore(query, name, path string) int {
	switch {
	case query == "":
		return strings.Count(path, "/")
	case path == query:
		return 0
	case name == query:
		return 1
	case strings.HasPrefix(name, query):
		return 2
	case strings.HasPrefix(path, query):
		return 3
	case strings.Contains(name, query):
		return 4
	case strings.Contains(path, query):
		return 5
	case subsequence(name, query):
		return 6
	case subsequence(path, query):
		return 7
	default:
		return -1
	}
}

func compareProjectSearchHits(a, b projectSearchHit) int {
	return cmp.Or(cmp.Compare(a.score, b.score), cmp.Compare(len(a.lower), len(b.lower)),
		cmp.Compare(a.lower, b.lower), cmp.Compare(a.entry.Path, b.entry.Path))
}

func ignoreProjectSearchFolder(name string) bool {
	switch name {
	case "node_modules", ".pnpm-store", ".next", ".nuxt", ".cache", ".venv", "__pycache__":
		return true
	default:
		return false
	}
}

func isProjectSearchCache(root *os.Root, local string) bool {
	path := filepath.Join(local, projectCacheTag)
	info, err := root.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	tag, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	defer tag.Close()
	opened, err := tag.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return false
	}
	var header [len(projectCacheSignature)]byte
	_, err = io.ReadFull(tag, header[:])
	return err == nil && string(header[:]) == projectCacheSignature
}

func openProjectSearchFolder(root *os.Root, realRoot, local string) (*os.File, error) {
	dir, err := root.OpenFile(local, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	// A folder queued during ReadDir may since have been replaced by a link.
	expected := filepath.Join(realRoot, local)
	actual, pathErr := finalPath(dir, expected)
	difference, relErr := filepath.Rel(expected, actual)
	if pathErr != nil || relErr != nil || difference != "." {
		dir.Close()
		return nil, fs.ErrNotExist
	}
	return dir, nil
}

func (s *projectSearch) visit(parent string, entry fs.DirEntry) {
	mode := entry.Type()
	if mode != fs.ModeDir && !mode.IsRegular() {
		return
	}
	name := entry.Name()
	path := name
	if parent != "" {
		path = parent + "/" + name
	}
	if mode.IsDir() {
		if ignoreProjectSearchFolder(strings.ToLower(name)) {
			return
		}
		if _, err := projectLocalPath(path, true); err != nil {
			return
		}
		if len(s.folders) == maxProjectSearchFolders {
			s.truncated = true
			return
		}
		s.folders = append(s.folders, path)
		return
	}
	lower := strings.ToLower(path)
	score := projectFileScore(s.query, strings.ToLower(name), lower)
	if score < 0 {
		return
	}
	if _, err := projectLocalPath(path, false); err != nil {
		return
	}
	hit := projectSearchHit{entry: projectEntry{Name: name, Path: path, Kind: "file"}, lower: lower, score: score}
	i, _ := slices.BinarySearchFunc(s.hits, hit, compareProjectSearchHits)
	if len(s.hits) == maxProjectSearchHits {
		s.truncated = true
		if i < len(s.hits) {
			copy(s.hits[i+1:], s.hits[i:])
			s.hits[i] = hit
		}
		return
	}
	s.hits = slices.Insert(s.hits, i, hit)
}

func (s *projectSearch) scan(ctx context.Context, dir *os.File, relative string) error {
	for ctx.Err() == nil && s.remaining > 0 {
		entries, err := dir.ReadDir(min(128, s.remaining))
		s.remaining -= len(entries)
		for _, entry := range entries {
			s.visit(relative, entry)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	s.truncated = true
	return nil
}

func searchProjectFiles(ctx context.Context, root *os.Root, query string) (projectListing, error) {
	ctx, cancel := context.WithTimeout(ctx, projectSearchTimeout)
	defer cancel()
	dir, err := root.Open(".")
	if err != nil {
		return projectListing{}, err
	}
	defer dir.Close()
	realRoot, err := finalPath(dir, root.Name())
	if err != nil {
		return projectListing{}, err
	}
	query = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(query, `\`, "/")))
	query = strings.TrimPrefix(query, "./")
	search := projectSearch{query: query, remaining: maxProjectSearchScan}
	if err := search.scan(ctx, dir, ""); err != nil {
		return projectListing{}, err
	}
	// Visit shallow folders first, so results from a partial scan start near the
	// project root. Reuse the root handle for each validated project directory.
	for i := 0; i < len(search.folders); i++ {
		if search.remaining == 0 || ctx.Err() != nil {
			search.truncated = true
			break
		}
		path := search.folders[i]
		local := filepath.FromSlash(path)
		dir, err := openProjectSearchFolder(root, realRoot, local)
		if err != nil {
			search.truncated = true
			continue
		}
		// Tools like Cargo mark generated output with a cache directory tag.
		if !isProjectSearchCache(root, local) && search.scan(ctx, dir, path) != nil {
			search.truncated = true
		}
		dir.Close()
	}
	entries := make([]projectEntry, len(search.hits))
	for i, hit := range search.hits {
		entries[i] = hit.entry
	}
	return projectListing{Entries: entries, Truncated: search.truncated}, nil
}

func (s *Server) projectFileSearch(w http.ResponseWriter, r *http.Request) {
	l := s.lang(r)
	session, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, l.T("Session not found", "会话不存在"))
		return
	}
	query := r.URL.Query().Get("q")
	if len(query) > maxProjectSearchQuery {
		fail(w, http.StatusBadRequest, l.T("File search query is too long", "文件搜索关键词过长"))
		return
	}
	if !filepath.IsAbs(session.Cwd) {
		fail(w, http.StatusNotFound, l.T("Project folder is unavailable", "无法打开项目目录"))
		return
	}
	root, err := os.OpenRoot(session.Cwd)
	if err != nil {
		fail(w, http.StatusNotFound, l.T("Project folder is unavailable", "无法打开项目目录"))
		return
	}
	defer root.Close()
	listing, err := searchProjectFiles(r.Context(), root, query)
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, l.T("Could not search project files", "无法搜索项目文件"))
		return
	}
	respond(w, http.StatusOK, listing)
}

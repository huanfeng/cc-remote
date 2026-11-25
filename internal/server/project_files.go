package server

import (
	"cmp"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"cc-remote/internal/store"
)

const (
	maxProjectEntries = 2000
	maxProjectScan    = 10000
	maxProjectPath    = 4096
)

type projectEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type projectListing struct {
	Entries   []projectEntry `json:"entries"`
	Truncated bool           `json:"truncated,omitempty"`
}

var errProjectPath = errors.New("invalid relative project path")

// Project paths are canonical, slash-delimited names supplied by the directory
// listing. Empty is the project root, and is only accepted for listings.
func projectLocalPath(relative string, directory bool) (string, error) {
	if relative == "" && directory {
		return ".", nil
	}
	if relative == "." || len(relative) > maxProjectPath || !fs.ValidPath(relative) ||
		strings.ContainsAny(relative, "\\\x00") ||
		(len(relative) > 1 && relative[1] == ':' && strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", rune(relative[0]))) {
		return "", errProjectPath
	}
	for part := range strings.SplitSeq(relative, "/") {
		if strings.EqualFold(part, ".git") {
			return "", fs.ErrNotExist
		}
		if runtime.GOOS == "windows" && strings.TrimRight(part, ". ") != part {
			return "", errProjectPath
		}
	}
	local, err := filepath.Localize(relative)
	if err != nil {
		return "", errProjectPath
	}
	return local, nil
}

// openProjectPath only lists and opens ordinary files in the session's working
// directory. os.Root keeps the lookup under that directory even if paths are
// concurrently replaced. Check each component for links and verify the file
// we return is still the same path, including on Windows with junctions.
func openProjectPath(cwd, relative string, directory bool) (*os.File, string, fs.FileInfo, error) {
	local, err := projectLocalPath(relative, directory)
	if err != nil {
		return nil, "", nil, err
	}
	if !filepath.IsAbs(cwd) {
		return nil, "", nil, fs.ErrNotExist
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return nil, "", nil, err
	}
	defer root.Close()

	var info fs.FileInfo
	var prefix string
	for part := range strings.SplitSeq(local, string(filepath.Separator)) {
		prefix = filepath.Join(prefix, part)
		info, err = root.Lstat(prefix)
		if err != nil {
			return nil, "", nil, err
		}
		wantDirectory := prefix != local || directory
		if (wantDirectory && info.Mode().Type() != fs.ModeDir) || (!wantDirectory && !info.Mode().IsRegular()) {
			return nil, "", nil, fs.ErrNotExist
		}
	}
	// A regular file may be replaced with a FIFO after Lstat; don't wait for
	// a writer before checking the opened handle.
	f, err := root.OpenFile(local, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, "", nil, err
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || (directory && opened.Mode().Type() != fs.ModeDir) || (!directory && !opened.Mode().IsRegular()) {
		f.Close()
		return nil, "", nil, fs.ErrNotExist
	}
	if local != "." {
		working, err := root.Open(".")
		if err != nil {
			f.Close()
			return nil, "", nil, err
		}
		realRoot, rootErr := finalPath(working, cwd)
		working.Close()
		expected := filepath.Join(realRoot, local)
		actual, pathErr := finalPath(f, expected)
		difference, relErr := filepath.Rel(expected, actual)
		if rootErr != nil || pathErr != nil || relErr != nil || difference != "." {
			f.Close()
			return nil, "", nil, fs.ErrNotExist
		}
	}
	return f, filepath.Join(cwd, local), opened, nil
}

// readProjectEntries reads a single directory, with independent caps on the
// number of items returned and the number scanned (including ignored entries).
func readProjectEntries(dir *os.File, relative string) (projectListing, error) {
	listing := projectListing{Entries: []projectEntry{}}
	scanned := 0
scan:
	for {
		entries, err := dir.ReadDir(128)
		for _, entry := range entries {
			scanned++
			if scanned > maxProjectScan {
				listing.Truncated = true
				break scan
			}
			mode := entry.Type()
			if mode != fs.ModeDir && !mode.IsRegular() {
				continue
			}
			path := entry.Name()
			if relative != "" {
				path = relative + "/" + path
			}
			if _, err := projectLocalPath(path, mode.IsDir()); err != nil {
				continue
			}
			if len(listing.Entries) == maxProjectEntries {
				listing.Truncated = true
				break scan
			}
			kind := "file"
			if mode.IsDir() {
				kind = "directory"
			}
			listing.Entries = append(listing.Entries, projectEntry{Name: entry.Name(), Path: path, Kind: kind})
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return projectListing{}, err
		}
	}
	slices.SortFunc(listing.Entries, func(a, b projectEntry) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.Name, b.Name))
	})
	return listing, nil
}

func (s *Server) projectFiles(w http.ResponseWriter, r *http.Request) {
	l := s.lang(r)
	session, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, l.T("Session not found", "会话不存在"))
		return
	}
	relative := r.URL.Query().Get("path")
	dir, _, _, err := openProjectPath(session.Cwd, relative, true)
	if err != nil {
		if errors.Is(err, errProjectPath) {
			fail(w, http.StatusBadRequest, l.T("Enter a path relative to the project", "请输入相对于项目的路径"))
		} else {
			fail(w, http.StatusNotFound, l.T("Project folder is unavailable", "无法打开项目目录"))
		}
		return
	}
	defer dir.Close()
	listing, err := readProjectEntries(dir, relative)
	if err != nil {
		fail(w, http.StatusInternalServerError, l.T("Could not list the project folder", "无法读取项目目录"))
		return
	}
	respond(w, http.StatusOK, listing)
}

func (s *Server) projectFile(w http.ResponseWriter, r *http.Request, session store.Session, relative string) (*os.File, string, fs.FileInfo, bool) {
	l := s.lang(r)
	f, path, info, err := openProjectPath(session.Cwd, relative, false)
	if err != nil {
		if errors.Is(err, errProjectPath) {
			fail(w, http.StatusBadRequest, l.T("Enter a path relative to the project", "请输入相对于项目的路径"))
		} else {
			fail(w, http.StatusNotFound, l.T("Project file is unavailable", "无法打开项目文件"))
		}
		return nil, "", nil, false
	}
	return f, path, info, true
}

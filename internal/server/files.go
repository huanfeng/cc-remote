package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"cc-remote/internal/store"
)

// maxViewBytes caps a file opened in the web viewer.
const maxViewBytes = 20 << 20

// Bounds on what a session's roots remember: paths looked at, and paths
// named before they existed (Claude names a file before writing it).
const maxSeenPaths, maxPendingPaths = 20000, 1000

// fileRoots are the directories a session's files may be opened from: its
// working directory, every directory named in the session, and the directory
// of every file named in it. They are gathered from the events as they come.
type fileRoots struct {
	mu       sync.Mutex
	sessions map[string]*sessionRoots
}

type sessionRoots struct {
	mu      sync.Mutex // held while the session's events are scanned
	seq     int64
	roots   map[string]bool // real paths
	seen    map[string]bool // paths already looked at
	pending map[string]bool // named paths that did not exist yet
}

func (f *fileRoots) session(id string) *sessionRoots {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessions == nil {
		f.sessions = map[string]*sessionRoots{}
	}
	r := f.sessions[id]
	if r == nil {
		r = &sessionRoots{roots: map[string]bool{}, seen: map[string]bool{}, pending: map[string]bool{}}
		f.sessions[id] = r
	}
	return r
}

// forget drops a deleted session's roots.
func (f *fileRoots) forget(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, id)
}

// absPath finds absolute paths in text: /a/b, C:\a\b or C:/a/b. Paths stop at
// whitespace, quotes, brackets and CJK punctuation.
var absPath = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.~/\\:-])((?:[A-Za-z]:[\\/]|/)[^\s"'` + "`" + `<>()\[\]{}|*?,;\x{3000}-\x{303F}\x{FF00}-\x{FFEF}]+)`)

// lineSuffix is a ":12" or ":12:5" or ":12-20" after a file name.
var lineSuffix = regexp.MustCompile(`:\d+(?:[-:]\d+)?$`)

// mentionedPaths returns the absolute paths named in an event's text and in
// the strings of its payload (tool inputs carry them whole).
func mentionedPaths(e store.Event) []string {
	var out []string
	scan := func(s string) {
		if t := strings.TrimSpace(s); t != "" && !strings.ContainsAny(t, "\n\r") && filepath.IsAbs(t) {
			out = append(out, t)
		}
		for _, m := range absPath.FindAllStringSubmatch(s, -1) {
			p := strings.TrimRight(m[1], ".:!。")
			out = append(out, lineSuffix.ReplaceAllString(p, ""))
		}
	}
	scan(e.Text)
	if len(e.Payload) > 0 {
		var v any
		if json.Unmarshal(e.Payload, &v) == nil {
			walkStrings(v, scan)
		}
	}
	return out
}

func walkStrings(v any, f func(string)) {
	switch x := v.(type) {
	case string:
		f(x)
	case []any:
		for _, item := range x {
			walkStrings(item, f)
		}
	case map[string]any:
		for _, item := range x {
			walkStrings(item, f)
		}
	}
}

// network reports a UNC path (\\host\share, //host/share, \\?\UNC\…). Opening
// one would make the host connect, and authenticate, to that machine, so
// only the working directory's own share is ever opened.
func network(path, cwd string) bool {
	v := filepath.VolumeName(path)
	if !strings.HasPrefix(v, `\\`) && !strings.HasPrefix(v, "//") {
		return false
	}
	return !strings.EqualFold(v, filepath.VolumeName(cwd))
}

// realPath is where path really is, with symlinks and junctions resolved.
func realPath(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return finalPath(f, path)
}

// within reports whether path is root or inside it. Windows and macOS
// paths compare without case.
func within(root, path string) bool {
	if runtime.GOOS == "darwin" {
		root, path = strings.ToLower(root), strings.ToLower(path)
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// homes is the home directory as named and as it really is (/home → /var/home).
var homes = sync.OnceValue(func() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := []string{filepath.Clean(home)}
	if real, err := realPath(home); err == nil {
		out = append(out, real)
	}
	return out
})

// broadRoot reports a directory too wide to open files from on a mention
// alone: a volume root, a top-level directory, or the home directory.
func broadRoot(dir string) bool {
	rest := strings.TrimPrefix(dir, filepath.VolumeName(dir))
	parts := strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) < 2 {
		return true
	}
	for _, home := range homes() {
		if within(dir, home) {
			return true // home itself or one of its parents
		}
	}
	return false
}

// rootDir is the real directory a named path opens: the path itself, or the
// directory of a file.
func rootDir(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	if !info.IsDir() {
		path = filepath.Dir(path)
	}
	real, err := realPath(path)
	return real, err == nil
}

// add looks at one named path; it reports false when the path does not exist.
func (r *sessionRoots) add(p string) bool {
	dir, ok := rootDir(p)
	if !ok {
		return false
	}
	if !broadRoot(dir) {
		r.roots[dir] = true
	}
	return true
}

// rootsFor brings a session's roots up to date with its events.
func (s *Server) rootsFor(sess store.Session) ([]string, error) {
	r := s.files.session(sess.ID)
	r.mu.Lock()
	defer r.mu.Unlock()
	for p := range r.pending {
		if r.add(p) {
			delete(r.pending, p)
		}
	}
	for {
		events, err := s.store.Events(sess.ID, r.seq)
		if err != nil {
			return nil, err
		}
		for _, e := range events {
			r.seq = e.Seq
			for _, p := range mentionedPaths(e) {
				p = filepath.Clean(p)
				if r.seen[p] || !filepath.IsAbs(p) || network(p, sess.Cwd) {
					continue
				}
				if len(r.seen) >= maxSeenPaths {
					clear(r.seen) // only saves work; the roots stay
				}
				r.seen[p] = true
				if !r.add(p) && len(r.pending) < maxPendingPaths {
					r.pending[p] = true
				}
			}
		}
		if len(events) < 10000 {
			break
		}
	}
	roots := make([]string, 0, len(r.roots)+1)
	if dir, err := realPath(sess.Cwd); err == nil {
		roots = append(roots, dir)
	}
	for dir := range r.roots {
		roots = append(roots, dir)
	}
	return roots, nil
}

var errOutsideRoots = errors.New("outside the session's directories")

// openFile opens the file a message names: an absolute path as is, a
// relative one against each base (directories named near it) and then the
// session's working directory. The containment check uses the real path of
// the opened file, so nothing can swap a link in between. It returns the
// file and the path as named.
func openFile(name string, bases []string, cwd string, roots []string) (*os.File, string, error) {
	var candidates []string
	if filepath.IsAbs(name) {
		candidates = []string{name}
	} else {
		for _, b := range append(bases, cwd) {
			if filepath.IsAbs(b) {
				candidates = append(candidates, filepath.Join(b, name))
			}
		}
	}
	notFound := error(fs.ErrNotExist)
	for _, c := range candidates {
		c = filepath.Clean(c)
		if network(c, cwd) {
			continue
		}
		f, err := os.Open(c)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		real, rerr := finalPath(f, c)
		if err != nil || rerr != nil || info.IsDir() {
			f.Close()
			continue
		}
		inside := false
		for _, root := range roots {
			inside = inside || within(root, real)
		}
		if inside {
			return f, c, nil
		}
		f.Close()
		notFound = errOutsideRoots
	}
	return nil, "", notFound
}

// viewType is the Content-Type a file is served with: never one a browser
// would run (HTML, SVG, XML, scripts); the viewer picks how to show it by name.
func viewType(name string) string {
	t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	m := strings.ToLower(t)
	switch {
	case strings.HasPrefix(m, "image/") && !strings.Contains(m, "svg"):
		return t
	case m == "application/pdf":
		return t
	}
	return "application/octet-stream"
}

// requestedFile opens the file a request names (?path=…&base=…) for session
// {id}, or answers why it cannot.
func (s *Server) requestedFile(w http.ResponseWriter, r *http.Request) (*os.File, string, os.FileInfo, bool) {
	sess, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return nil, "", nil, false
	}
	name := r.URL.Query().Get("path")
	if name == "" || strings.ContainsRune(name, 0) {
		fail(w, 400, s.lang(r).T("No file path given", "未指定文件路径"))
		return nil, "", nil, false
	}
	bases := r.URL.Query()["base"]
	if len(bases) > 16 {
		bases = bases[:16]
	}
	return s.sessionFile(w, r, sess, name, bases)
}

// sessionFile opens a file of the session (see fileRoots), or answers why not.
func (s *Server) sessionFile(w http.ResponseWriter, r *http.Request, sess store.Session, name string, bases []string) (*os.File, string, os.FileInfo, bool) {
	l := s.lang(r)
	roots, err := s.rootsFor(sess)
	if err != nil {
		fail(w, 500, err.Error())
		return nil, "", nil, false
	}
	f, path, err := openFile(name, bases, sess.Cwd, roots)
	if errors.Is(err, errOutsideRoots) {
		fail(w, 403, l.T("Only files in the session's working directory, or in directories named in the session, can be opened", "只能打开会话工作目录、或会话中出现过的目录里的文件"))
		return nil, "", nil, false
	}
	if err != nil {
		fail(w, 404, l.T("File not found", "文件不存在"))
		return nil, "", nil, false
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		fail(w, 500, err.Error())
		return nil, "", nil, false
	}
	return f, path, info, true
}

// file is GET /api/sessions/{id}/file?path=…&base=…: a file the session
// named, for the web viewer. Only files under the session's directories (see
// fileRoots) open.
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	f, path, info, ok := s.requestedFile(w, r)
	if !ok {
		return
	}
	defer f.Close()
	l := s.lang(r)
	if info.Size() > maxViewBytes {
		fail(w, 413, l.T("The file is larger than 20 MiB", "文件超过 20 MiB"))
		return
	}
	w.Header().Set("Content-Type", viewType(path))
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-CC-Path", url.PathEscape(path))
	http.ServeContent(w, r, "", info.ModTime(), f)
}

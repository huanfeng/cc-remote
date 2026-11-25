package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	projectGitTimeout   = 8 * time.Second
	maxProjectGitStatus = 512 << 10
	maxProjectGitDiff   = 256 << 10
	maxProjectChanges   = 1000
)

type projectGitChange struct {
	Path         string `json:"path"`
	OriginalPath string `json:"originalPath,omitempty"`
	Stage        string `json:"stage"`
	Status       string `json:"status"`
}

type projectGitListing struct {
	Repository bool               `json:"repository"`
	Entries    []projectGitChange `json:"entries"`
	Truncated  bool               `json:"truncated,omitempty"`
}

type projectGitPatch struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

type projectWorktree struct {
	cwd, prefix string
	filters     []string
}

// Discard inherited Git overrides that could point at another worktree or
// index. Git is deliberately read-only during project inspection.
func projectGitEnv() []string {
	env := os.Environ()
	kept := env[:0]
	for _, item := range env {
		name, _, _ := strings.Cut(item, "=")
		if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			kept = append(kept, item)
		}
	}
	return append(kept, "LC_ALL=C", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=")
}

type projectGitOutput struct {
	buffer bytes.Buffer
	limit  int
	cut    bool
	cancel context.CancelFunc
}

func (o *projectGitOutput) Write(p []byte) (int, error) {
	remaining := o.limit - o.buffer.Len()
	o.buffer.Write(p[:min(remaining, len(p))])
	if len(p) > remaining {
		o.cut = true
		if o.cancel != nil {
			o.cancel()
		}
	}
	return len(p), nil
}

func (w projectWorktree) git(ctx context.Context, limit int, args ...string) ([]byte, bool, error) {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	options := []string{"--no-pager", "--literal-pathspecs", "-c", "core.fsmonitor=false", "-c", "diff.renameLimit=200"}
	options = append(options, w.filters...)
	cmd := exec.CommandContext(run, "git", append(options, args...)...)
	cmd.Dir = w.cwd
	cmd.Env = projectGitEnv()
	cmd.WaitDelay = 250 * time.Millisecond
	hideWindow(cmd)
	out := projectGitOutput{limit: limit, cancel: cancel}
	stderr := projectGitOutput{limit: 4096}
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	if out.cut {
		return out.buffer.Bytes(), true, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%w: %s", err, stderr.buffer.String())
	}
	return out.buffer.Bytes(), false, nil
}

// Worktree comparison can call a clean/process filter selected by attributes.
// Disable the configured filter drivers before asking Git to compare files.
func (w *projectWorktree) disableProjectGitFilters(ctx context.Context) error {
	out, cut, err := w.git(ctx, 4096, "config", "--null", "--name-only", "--get-regexp", `^filter\..*\.(clean|process|required)$`)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil // No configured drivers.
		}
		return err
	}
	if cut {
		return errors.New("too many configured Git filters")
	}
	seen := map[string]bool{}
	for key := range strings.SplitSeq(string(out), "\x00") {
		dot := strings.LastIndexByte(key, '.')
		if dot < 0 || seen[key[:dot]] {
			continue
		}
		name := key[:dot]
		seen[name] = true
		w.filters = append(w.filters, "-c", name+".clean=", "-c", name+".process=", "-c", name+".required=false")
	}
	return nil
}

func newProjectWorktree(ctx context.Context, cwd string) (*projectWorktree, error) {
	dir, _, _, err := openProjectPath(cwd, "", true)
	if err != nil {
		return nil, err
	}
	actual, err := finalPath(dir, cwd)
	dir.Close()
	if err != nil {
		return nil, err
	}
	w := &projectWorktree{cwd: cwd}
	out, cut, err := w.git(ctx, maxProjectPath+16, "rev-parse", "--is-inside-work-tree", "--show-toplevel")
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") || strings.Contains(err.Error(), "must be run in a work tree") {
			return nil, nil
		}
		return nil, err
	}
	if cut {
		return nil, errProjectPath
	}
	inside, root, _ := strings.Cut(string(out), "\n")
	if strings.TrimSuffix(inside, "\r") != "true" {
		return nil, nil
	}
	root = strings.TrimSuffix(strings.TrimSuffix(root, "\n"), "\r")
	rootDir, _, _, err := openProjectPath(filepath.FromSlash(root), "", true)
	if err != nil {
		return nil, err
	}
	root, err = finalPath(rootDir, filepath.FromSlash(root))
	rootDir.Close()
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, actual)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, errProjectPath
	}
	if relative != "." {
		w.prefix = filepath.ToSlash(relative) + "/"
	}
	if err := w.disableProjectGitFilters(ctx); err != nil {
		return nil, err
	}
	return w, nil
}

func (w projectWorktree) relative(path string) string {
	path, within := strings.CutPrefix(path, w.prefix)
	if !within {
		return ""
	}
	if _, err := projectLocalPath(path, false); err != nil {
		return ""
	}
	return path
}

func projectGitStatus(code byte) string {
	switch code {
	case 'A':
		return "added"
	case 'M':
		return "modified"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	case 'T':
		return "type-changed"
	case 'U':
		return "conflicted"
	}
	return ""
}

func (w projectWorktree) changes(ctx context.Context) (projectGitListing, error) {
	out, cut, err := w.git(ctx, maxProjectGitStatus, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--find-renames", "--ignore-submodules=all", "--", ".")
	if err != nil {
		return projectGitListing{}, err
	}
	listing := projectGitListing{Repository: true, Entries: []projectGitChange{}, Truncated: cut}
	add := func(path, original, stage, status string) {
		if len(listing.Entries) == maxProjectChanges {
			listing.Truncated = true
			return
		}
		listing.Entries = append(listing.Entries, projectGitChange{Path: path, OriginalPath: original, Stage: stage, Status: status})
	}
	rest := string(out)
	for rest != "" {
		record, next, complete := strings.Cut(rest, "\x00")
		if !complete || len(record) < 4 {
			break
		}
		rest = next
		xy := record[:2]
		path := w.relative(record[3:])
		original := ""
		if strings.ContainsAny(xy, "RC") {
			name, next, complete := strings.Cut(rest, "\x00")
			if !complete {
				break
			}
			rest, original = next, w.relative(name)
		}
		if path == "" {
			continue
		}
		switch {
		case xy == "??":
			add(path, "", "untracked", "untracked")
		case strings.ContainsRune(xy, 'U') || xy == "AA" || xy == "DD":
			add(path, "", "working", "conflicted")
		default:
			for i, stage := range []string{"staged", "working"} {
				status := projectGitStatus(xy[i])
				if status == "renamed" || status == "copied" {
					if original == "" {
						status = "added"
					}
					add(path, original, stage, status)
				} else if status != "" {
					add(path, "", stage, status)
				}
			}
		}
		if listing.Truncated && len(listing.Entries) == maxProjectChanges {
			break
		}
	}
	return listing, nil
}

func (w projectWorktree) patch(ctx context.Context, change projectGitChange) (projectGitPatch, error) {
	if change.Stage == "untracked" {
		return untrackedProjectPatch(w.cwd, change.Path)
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--relative", "--find-renames", "--ignore-submodules=all", "--unified=3",
		"--src-prefix=a/", "--dst-prefix=b/", "--output-indicator-new=+", "--output-indicator-old=-", "--output-indicator-context= "}
	if change.Stage == "staged" {
		args = append(args, "--cached")
	} else {
		for _, name := range []string{change.Path, change.OriginalPath} {
			if name == "" {
				continue
			}
			if err := verifyGitWorkingPath(w.cwd, name); err != nil {
				return projectGitPatch{}, err
			}
		}
	}
	args = append(args, "--", change.Path)
	if change.OriginalPath != "" {
		args = append(args, change.OriginalPath)
	}
	out, cut, err := w.git(ctx, maxProjectGitDiff, args...)
	return projectGitPatch{Text: string(out), Truncated: cut}, err
}

// A working diff may refer to a removed file or directory. Verify its closest
// surviving parent without following symlinks or junctions inside the project.
func verifyGitWorkingPath(cwd, name string) error {
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return err
	}
	defer root.Close()
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		dir, _, _, openErr := openProjectPath(cwd, parent, true)
		if openErr == nil {
			dir.Close()
			break
		}
		if _, statErr := root.Lstat(filepath.FromSlash(parent)); !errors.Is(statErr, fs.ErrNotExist) {
			return openErr
		}
	}
	info, err := root.Lstat(filepath.FromSlash(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && !info.IsDir() && info.Mode()&fs.ModeSymlink == 0 {
		return errProjectPath
	}
	return nil
}

func quoteProjectPatchPath(path string) string {
	if strings.ContainsAny(path, "\"\\") || strings.IndexFunc(path, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return strconv.Quote(path)
	}
	return path
}

// Read new files through the same scoped file handle as the project browser.
func untrackedProjectPatch(cwd, path string) (projectGitPatch, error) {
	f, _, info, err := openProjectPath(cwd, path, false)
	if err != nil {
		return projectGitPatch{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxProjectGitDiff+1))
	if err != nil {
		return projectGitPatch{}, err
	}
	a, b := quoteProjectPatchPath("a/"+path), quoteProjectPatchPath("b/"+path)
	mode := "100644"
	if info.Mode().Perm()&0o111 != 0 {
		mode = "100755"
	}
	header := "diff --git " + a + " " + b + "\nnew file mode " + mode + "\n"
	if len(data) == 0 {
		return projectGitPatch{Text: header}, nil
	}
	cut := len(data) > maxProjectGitDiff
	if cut {
		data = data[:maxProjectGitDiff]
		for i := 0; i < utf8.UTFMax-1; i++ {
			r, n := utf8.DecodeLastRune(data)
			if r != utf8.RuneError || n != 1 {
				break
			}
			data = data[:len(data)-1]
		}
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return projectGitPatch{Text: header + "Binary files /dev/null and " + b + " differ\n"}, nil
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	withoutNewline := !strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")
	header += "--- /dev/null\n+++ " + b + "\n@@ -0,0 +1," + strconv.Itoa(strings.Count(text, "\n")+1) + " @@\n"
	patch := header + "+" + strings.ReplaceAll(text, "\n", "\n+") + "\n"
	if withoutNewline && !cut {
		patch += "\\ No newline at end of file\n"
	}
	return projectGitPatch{Text: patch[:min(len(patch), maxProjectGitDiff)], Truncated: cut || len(patch) > maxProjectGitDiff}, nil
}

func (s *Server) projectGitChanges(w http.ResponseWriter, r *http.Request) {
	l := s.lang(r)
	session, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, l.T("Session not found", "会话不存在"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), projectGitTimeout)
	defer cancel()
	worktree, err := newProjectWorktree(ctx, session.Cwd)
	if err != nil {
		s.projectGitError(w, r, err)
		return
	}
	if worktree == nil {
		respond(w, http.StatusOK, projectGitListing{Entries: []projectGitChange{}})
		return
	}
	listing, err := worktree.changes(ctx)
	if err != nil {
		s.projectGitError(w, r, err)
		return
	}
	respond(w, http.StatusOK, listing)
}

func (s *Server) projectGitDiff(w http.ResponseWriter, r *http.Request) {
	l := s.lang(r)
	session, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusNotFound, l.T("Session not found", "会话不存在"))
		return
	}
	query := r.URL.Query()
	path, stage := query.Get("path"), query.Get("stage")
	if _, err := projectLocalPath(path, false); err != nil || (stage != "working" && stage != "staged" && stage != "untracked") {
		fail(w, http.StatusBadRequest, l.T("Enter a project-relative path and a valid change type", "请输入项目相对路径和有效的改动类型"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), projectGitTimeout)
	defer cancel()
	worktree, err := newProjectWorktree(ctx, session.Cwd)
	if err != nil {
		s.projectGitError(w, r, err)
		return
	}
	if worktree != nil {
		listing, err := worktree.changes(ctx)
		if err != nil {
			s.projectGitError(w, r, err)
			return
		}
		for _, change := range listing.Entries {
			if change.Path != path || change.Stage != stage {
				continue
			}
			patch, err := worktree.patch(ctx, change)
			if err != nil {
				s.projectGitError(w, r, err)
				return
			}
			respond(w, http.StatusOK, patch)
			return
		}
	}
	fail(w, http.StatusNotFound, l.T("This Git change is no longer available. Refresh the changes list.", "此 Git 改动已不可用，请刷新改动列表。"))
}

func (s *Server) projectGitError(w http.ResponseWriter, r *http.Request, err error) {
	l := s.lang(r)
	if errors.Is(err, exec.ErrNotFound) {
		fail(w, http.StatusNotImplemented, l.T("Git is unavailable on this server", "该服务器尚未安装 Git"))
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		fail(w, http.StatusGatewayTimeout, l.T("Reading Git changes timed out", "读取 Git 改动超时"))
		return
	}
	fail(w, http.StatusUnprocessableEntity, l.T("Could not read Git changes for this project", "无法读取此项目的 Git 改动"))
}

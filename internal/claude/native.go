package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type NativeSession struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Cwd         string `json:"cwd"`
	UpdatedAt   string `json:"updatedAt"`
	customTitle bool
}

type nativeFile struct {
	path string
	size int64
	mod  time.Time
}

func nativeFiles(configDir string) ([]string, error) {
	files, err := nativeFileInfos(configDir)
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.path
	}
	return paths, err
}

func projectsDir(configDir string) (string, error) {
	if configDir == "" {
		configDir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(home, ".claude")
	}
	return filepath.Join(configDir, "projects"), nil
}

func nativeFileInfos(configDir string) ([]nativeFile, error) {
	projects, err := projectsDir(configDir)
	if err != nil {
		return nil, err
	}
	dirs, err := os.ReadDir(projects)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []nativeFile
	for _, d := range dirs {
		if !d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(projects, d.Name()))
		if err != nil {
			continue
		}
		for _, e := range entries {
			id := strings.TrimSuffix(e.Name(), ".jsonl")
			if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".jsonl") || !uuidPattern.MatchString(id) {
				continue
			}
			// Stat, not the directory entry: on Windows the entry's size and
			// mtime can lag for a file a writer keeps open.
			path := filepath.Join(projects, d.Name(), e.Name())
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			files = append(files, nativeFile{path, info.Size(), info.ModTime()})
			if len(files) >= 5000 {
				return files, nil
			}
		}
	}
	return files, nil
}

// listCache keeps each transcript's metadata until its size or mtime changes:
// the session list is polled every few seconds and there can be thousands of
// transcripts, so only files that changed are read again.
var listCache struct {
	sync.Mutex
	entries map[string]listEntry
}

type listEntry struct {
	size    int64
	mod     time.Time
	session NativeSession
}

// ListNative reads transcript metadata. It does not attach to running terminals.
func ListNative(configDir string) ([]NativeSession, error) {
	files, err := nativeFileInfos(configDir)
	if err != nil {
		return nil, err
	}
	listCache.Lock()
	defer listCache.Unlock()
	if listCache.entries == nil {
		listCache.entries = map[string]listEntry{}
	}
	seen := make(map[string]bool, len(files))
	found := make([]listEntry, 0, len(files))
	for _, file := range files {
		seen[file.path] = true
		e, ok := listCache.entries[file.path]
		if !ok || e.size != file.size || !e.mod.Equal(file.mod) {
			session, ok := inspectFile(file)
			if !ok {
				continue
			}
			e = listEntry{file.size, file.mod, session}
			listCache.entries[file.path] = e
		}
		found = append(found, e)
	}
	for path := range listCache.entries {
		if !seen[path] {
			delete(listCache.entries, path)
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].mod.After(found[j].mod) })
	out := make([]NativeSession, len(found))
	for i, e := range found {
		out[i] = e.session
	}
	return out, nil
}

// inspectFile reads bounded head/tail windows rather than the full transcript.
func inspectFile(file nativeFile) (NativeSession, bool) {
	f, err := os.Open(file.path)
	if err != nil {
		return NativeSession{}, false
	}
	defer f.Close()
	session := NativeSession{ID: strings.TrimSuffix(filepath.Base(file.path), ".jsonl"), Title: "Claude session", UpdatedAt: file.mod.UTC().Format(time.RFC3339Nano)}
	data, _ := io.ReadAll(io.LimitReader(f, 256*1024))
	inspectMetadata(data, &session)
	if file.size > 256*1024 {
		_, _ = f.Seek(max(0, file.size-256*1024), io.SeekStart)
		tail, _ := io.ReadAll(io.LimitReader(f, 256*1024))
		if i := bytes.IndexByte(tail, '\n'); i >= 0 {
			inspectMetadata(tail[i+1:], &session)
		}
	}
	return session, true
}

func inspectMetadata(data []byte, s *NativeSession) {
	for _, line := range bytes.Split(data, []byte("\n")) {
		inspectLine(line, s)
	}
}

func inspectLine(line []byte, s *NativeSession) {
	var v struct {
		Type        string `json:"type"`
		Cwd         string `json:"cwd"`
		CustomTitle string `json:"customTitle"`
		AITitle     string `json:"aiTitle"`
		IsMeta      bool   `json:"isMeta"`
		IsSidechain bool   `json:"isSidechain"`
		Message     struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &v) != nil {
		return
	}
	if v.Cwd != "" && s.Cwd == "" {
		s.Cwd = v.Cwd
	}
	if v.CustomTitle != "" {
		s.Title = v.CustomTitle
		s.customTitle = true
	}
	if v.AITitle != "" && !s.customTitle {
		s.Title = v.AITitle
	}
	if v.Type == "user" && !v.IsMeta && !v.IsSidechain && s.Title == "Claude session" {
		for _, e := range contentEvents(v.Message.Content, "user") {
			if e.Kind == "user" && !syntheticText(e.Text) {
				title := []rune(strings.ReplaceAll(strings.TrimSpace(e.Text), "\n", " "))
				if len(title) > 80 {
					title = title[:80]
				}
				s.Title = string(title)
				break
			}
		}
	}
}

const maxNativeHistory = 64 * 1024 * 1024

// historyCache keeps parsed transcripts of recently opened sessions. A
// transcript only grows, so a changed file is parsed from where the last read
// stopped; one that shrank (rewritten) is parsed again from the start.
var historyCache struct {
	sync.Mutex
	entries map[string]*historyEntry // by path
	order   []string                 // least recently used first
}

const historyCacheSize = 8

type historyEntry struct {
	mu      sync.Mutex
	size    int64
	mod     time.Time
	offset  int64 // bytes consumed: complete lines only
	session NativeSession
	events  []Event
}

func NativeHistory(configDir, id string) (NativeSession, []Event, error) {
	if !uuidPattern.MatchString(id) {
		return NativeSession{}, nil, errors.New("invalid Claude session UUID")
	}
	path, err := nativePath(configDir, id)
	if err != nil {
		return NativeSession{}, nil, err
	}
	e := cachedHistory(path)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refresh(path, id); err != nil {
		return NativeSession{}, nil, err
	}
	// The full slice expression makes a caller's append copy instead of
	// writing into the cached array.
	return e.session, e.events[:len(e.events):len(e.events)], nil
}

// nativePath finds a transcript by session id, trying the last known path first.
func nativePath(configDir, id string) (string, error) {
	projects, err := projectsDir(configDir)
	if err != nil {
		return "", err
	}
	known := ""
	historyCache.Lock()
	for path := range historyCache.entries {
		if strings.EqualFold(strings.TrimSuffix(filepath.Base(path), ".jsonl"), id) && strings.HasPrefix(path, projects+string(filepath.Separator)) {
			known = path
		}
	}
	historyCache.Unlock()
	if known != "" {
		if _, err := os.Stat(known); err == nil {
			return known, nil
		}
	}
	files, err := nativeFileInfos(configDir)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if strings.EqualFold(strings.TrimSuffix(filepath.Base(f.path), ".jsonl"), id) {
			return f.path, nil
		}
	}
	return "", os.ErrNotExist
}

func cachedHistory(path string) *historyEntry {
	historyCache.Lock()
	defer historyCache.Unlock()
	if historyCache.entries == nil {
		historyCache.entries = map[string]*historyEntry{}
	}
	for i, p := range historyCache.order {
		if p == path {
			historyCache.order = append(historyCache.order[:i], historyCache.order[i+1:]...)
			break
		}
	}
	historyCache.order = append(historyCache.order, path)
	e := historyCache.entries[path]
	if e == nil {
		e = &historyEntry{}
		historyCache.entries[path] = e
	}
	for len(historyCache.order) > historyCacheSize {
		delete(historyCache.entries, historyCache.order[0])
		historyCache.order = historyCache.order[1:]
	}
	return e
}

func (e *historyEntry) refresh(path, id string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxNativeHistory {
		return errors.New("transcript exceeds 64 MiB history limit")
	}
	if info.Size() == e.size && info.ModTime().Equal(e.mod) && e.offset > 0 {
		return nil
	}
	if info.Size() < e.offset || e.offset == 0 {
		e.offset, e.events = 0, nil
		e.session = NativeSession{ID: id, Title: "Claude session"}
	}
	if _, err := f.Seek(e.offset, io.SeekStart); err != nil {
		return err
	}
	scanner := bufio.NewScanner(io.LimitReader(f, info.Size()-e.offset))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	// Only complete lines are consumed, so offset never includes a partial
	// last line that Claude is still writing.
	consumed := e.offset
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			consumed += int64(i) + 1
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 && json.Valid(bytes.TrimSpace(data)) {
			consumed += int64(len(data)) // a complete last line without newline
			return len(data), data, nil
		}
		return 0, nil, nil // a partial last line waits for the next read
	})
	for scanner.Scan() {
		e.events = append(e.events, transcriptEvents(scanner.Bytes(), &e.session)...)
	}
	e.offset = consumed
	if err := scanner.Err(); err != nil {
		e.offset, e.events = 0, nil
		return err
	}
	e.size, e.mod = info.Size(), info.ModTime()
	e.session.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
	return nil
}

// transcriptEvents updates the session metadata from one transcript line and
// returns the events it shows in history.
func transcriptEvents(line []byte, session *NativeSession) []Event {
	inspectLine(line, session)
	var v struct {
		Type        string `json:"type"`
		IsMeta      bool   `json:"isMeta"`
		IsSidechain bool   `json:"isSidechain"`
		Message     struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &v) != nil || v.IsMeta || v.IsSidechain || (v.Type != "user" && v.Type != "assistant") {
		return nil
	}
	var out []Event
	for _, e := range contentEvents(v.Message.Content, v.Type) {
		if e.Kind == "user" && syntheticText(e.Text) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func syntheticText(text string) bool {
	text = strings.TrimSpace(text)
	for _, p := range []string{"<local-command", "<command-name>", "<system-reminder>", "[Request interrupted", "This session is being continued from a previous conversation"} {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return text == ""
}

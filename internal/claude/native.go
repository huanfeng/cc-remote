package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cc-remote/internal/i18n"
)

type NativeSession struct {
	Provider  string `json:"provider,omitempty"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	UpdatedAt string `json:"updatedAt"`
	// Truncated: the transcript is too large and only its recent part was read.
	Truncated   bool `json:"truncated,omitempty"`
	customTitle bool
	launches    map[string]launch // background tool calls awaiting their result, by id
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

var maxNativeHistory int64 = 64 * 1024 * 1024 // a variable for tests

// historyCache keeps parsed transcripts of recently opened sessions. A
// transcript only grows, so a changed file is parsed from where the last read
// stopped; one that shrank (rewritten) is parsed again from the start. It holds
// at most historyCacheSize sessions and about historyCacheBytes of events (the
// session just read stays however large), and forgets one unread for
// historyIdle: a page showing a session reads it every few seconds.
var historyCache struct {
	sync.Mutex
	entries map[string]*historyEntry // by path
	order   []string                 // least recently used first
	janitor sync.Once
}

const historyCacheSize = 8

// Variables for tests.
var (
	historyCacheBytes int64 = 64 << 20
	historyIdle             = 10 * time.Minute
)

type historyEntry struct {
	mu      sync.Mutex
	size    int64
	mod     time.Time
	offset  int64 // bytes consumed: complete lines only
	session NativeSession
	events  []Event
	weight  atomic.Int64 // about the bytes events hold; read without mu by the cache
	used    time.Time    // guarded by historyCache
}

// eventWeight is about the memory one event holds.
func eventWeight(ev Event) int64 {
	return int64(len(ev.Kind)+len(ev.Text)+len(ev.Payload)+len(ev.Parent)) + 64
}

func NativeHistory(configDir, id string) (NativeSession, []Event, error) {
	if !uuidPattern.MatchString(id) {
		return NativeSession{}, nil, errors.New(i18n.T("invalid Claude session UUID", "无效的 Claude 会话 UUID"))
	}
	path, err := nativePath(configDir, id)
	if err != nil {
		return NativeSession{}, nil, err
	}
	e := cachedHistory(path)
	e.mu.Lock()
	defer e.mu.Unlock()
	err = e.refresh(path, id)
	trimHistory(path) // its new size may push older sessions out
	if err != nil {
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
	e.used = time.Now()
	historyCache.janitor.Do(func() {
		go func() {
			for range time.Tick(time.Minute) {
				trimHistory("")
			}
		}()
	})
	return e
}

// trimHistory forgets the least recently read sessions while the cache holds
// too many, too much, or ones unread for historyIdle; never keep (just read).
func trimHistory(keep string) {
	historyCache.Lock()
	defer historyCache.Unlock()
	var total int64
	for _, e := range historyCache.entries {
		total += e.weight.Load()
	}
	now := time.Now()
	kept := historyCache.order[:0]
	for i, path := range historyCache.order {
		e := historyCache.entries[path]
		over := len(historyCache.order)-i > historyCacheSize || total > historyCacheBytes
		if path != keep && (over || now.Sub(e.used) > historyIdle) {
			total -= e.weight.Load()
			delete(historyCache.entries, path)
			continue
		}
		kept = append(kept, path)
	}
	historyCache.order = kept
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
	if info.Size() == e.size && info.ModTime().Equal(e.mod) && e.offset > 0 {
		return nil
	}
	if info.Size() < e.offset || e.offset == 0 {
		e.offset, e.events = 0, nil
		e.weight.Store(0)
		e.session = NativeSession{ID: id, Title: "Claude session"}
		// A huge transcript is read from a line start in its last
		// maxNativeHistory bytes: the recent part is what is wanted.
		if info.Size() > maxNativeHistory {
			start, err := lineStart(f, info.Size()-maxNativeHistory)
			if err != nil {
				return err
			}
			e.offset, e.session.Truncated = start, true
		}
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
		for _, ev := range transcriptEvents(scanner.Bytes(), &e.session) {
			e.events = append(e.events, ev)
			e.weight.Add(eventWeight(ev))
		}
	}
	e.offset = consumed
	if err := scanner.Err(); err != nil {
		e.offset, e.events = 0, nil
		e.weight.Store(0)
		return err
	}
	e.size, e.mod = info.Size(), info.ModTime()
	e.session.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
	return nil
}

// lineStart is the offset of the first line that starts at or after pos.
func lineStart(f *os.File, pos int64) (int64, error) {
	if pos <= 0 {
		return 0, nil
	}
	if _, err := f.Seek(pos-1, io.SeekStart); err != nil {
		return 0, err
	}
	r := bufio.NewReaderSize(f, 64*1024)
	n := pos - 1
	for {
		chunk, err := r.ReadSlice('\n')
		n += int64(len(chunk))
		if err == nil {
			return n, nil
		}
		if err == io.EOF {
			return n, nil // no line starts after pos: nothing to read yet
		}
		if err != bufio.ErrBufferFull {
			return 0, err
		}
	}
}

// transcriptEvents updates the session metadata from one transcript line and
// returns the events it shows in history.
func transcriptEvents(line []byte, session *NativeSession) []Event {
	inspectLine(line, session)
	var v struct {
		Type        string          `json:"type"`
		Subtype     string          `json:"subtype"`
		Content     json.RawMessage `json:"content"`
		IsMeta      bool            `json:"isMeta"`
		IsSidechain bool            `json:"isSidechain"`
		Message     struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		// Origin tells typed prompts (kind "human") from text Claude Code adds
		// as a user turn, such as a background task's <task-notification>.
		Origin struct {
			Kind string `json:"kind"`
		} `json:"origin"`
		PromptSource  string          `json:"promptSource"`
		ToolUseResult json.RawMessage `json:"toolUseResult"`
		Attachment    struct {
			Type        string `json:"type"`
			Prompt      string `json:"prompt"`
			CommandMode string `json:"commandMode"`
			Origin      struct {
				Kind string `json:"kind"`
			} `json:"origin"`
		} `json:"attachment"`
		Compact struct {
			Trigger    string `json:"trigger"`
			PreTokens  *int64 `json:"preTokens"`
			PostTokens *int64 `json:"postTokens"`
		} `json:"compactMetadata"`
	}
	if json.Unmarshal(line, &v) != nil || v.IsMeta || v.IsSidechain {
		return nil
	}
	// /compact (or auto-compact): the web shows a divider, shaped like the
	// stream-json compact_boundary of managed sessions.
	if v.Type == "system" && v.Subtype == "compact_boundary" {
		payload, _ := json.Marshal(map[string]any{"compact_metadata": map[string]any{"trigger": v.Compact.Trigger, "pre_tokens": v.Compact.PreTokens, "post_tokens": v.Compact.PostTokens}})
		return []Event{{Kind: "compact", Payload: payload}}
	}
	// Claude Code's recap of the session, written while its user was away.
	if v.Type == "system" && v.Subtype == "away_summary" {
		var text string
		if json.Unmarshal(v.Content, &text) != nil {
			return nil
		}
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "(disable recaps in /config)"))
		if text == "" {
			return nil
		}
		return []Event{{Kind: "recap", Text: text}}
	}
	// Background work reports as turns Claude Code adds: a task's end, or a
	// teammate's messages (see tasks.go).
	if v.Type == "attachment" && v.Attachment.Type == "queued_command" {
		if events, ok := noticeEvents(v.Attachment.Prompt); ok {
			return events
		}
	}
	// A message typed while Claude worked and taken in mid-turn: Claude Code
	// records it as a queued_command attachment, never as a user turn.
	if v.Type == "attachment" && v.Attachment.Type == "queued_command" && v.Attachment.CommandMode == "prompt" && (v.Attachment.Origin.Kind == "" || v.Attachment.Origin.Kind == "human") {
		if text := strings.TrimSpace(v.Attachment.Prompt); text != "" && !syntheticText(text) {
			text, payload := SplitAttachments(UnwrapPastes(v.Attachment.Prompt))
			return []Event{{Kind: "user", Text: text, Payload: payload}}
		}
		return nil
	}
	if v.Type != "user" && v.Type != "assistant" {
		return nil
	}
	var out []Event
	for _, e := range contentEvents(v.Message.Content, v.Type) {
		if e.Kind == "user" {
			e.Text = UnwrapPastes(e.Text)
			if events, ok := noticeEvents(e.Text); ok {
				out = append(out, events...)
				continue
			}
			// A web message's files show as attachments, as when it was sent.
			if text, payload := SplitAttachments(e.Text); payload != nil && len(e.Payload) == 0 {
				e.Text, e.Payload = text, payload
			}
		}
		if e.Kind == "user" && (syntheticText(e.Text) || v.Origin.Kind != "" && v.Origin.Kind != "human" || v.PromptSource == "system") {
			continue
		}
		out = append(out, e)
		switch e.Kind {
		case "tool":
			noteLaunch(session, e.Payload)
		case "tool_result":
			if len(v.ToolUseResult) > 0 {
				if started := launchEvent(session, e.Payload, v.ToolUseResult); started != nil {
					out = append(out, *started)
				}
			}
		}
	}
	return out
}

// pastedContent is how Claude Code records pasted text in a prompt, on lines
// of their own: <pasted_content id="3dad">, the text, </pasted_content id="3dad">.
var pastedContent = regexp.MustCompile(`(?s)<pasted_content id="([^"]*)">\n?(.*?)\n?</pasted_content id="([^"]*)">`)

// UnwrapPastes is a prompt as its user wrote it: pasted text without the tags
// Claude Code wraps it in.
func UnwrapPastes(text string) string {
	if !strings.Contains(text, "<pasted_content id=") {
		return text
	}
	out := pastedContent.ReplaceAllStringFunc(text, func(m string) string {
		g := pastedContent.FindStringSubmatch(m)
		if g[1] != g[3] {
			return m
		}
		return g[2]
	})
	return strings.TrimSpace(out)
}

func syntheticText(text string) bool {
	text = strings.TrimSpace(text)
	for _, p := range []string{"<local-command", "<command-name>", "<system-reminder>", "[Request interrupted", "This session is being continued from a previous conversation", "<task-notification>", "<bash-stdout>", "<bash-stderr>"} {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return text == ""
}

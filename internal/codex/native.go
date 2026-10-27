// Package codex reads local Codex rollout transcripts.
package codex

import (
	"bufio"
	"bytes"
	"container/list"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"cc-remote/internal/claude"
	_ "modernc.org/sqlite"
)

const nativeHistoryLimit int64 = 64 << 20
const nativeFileLimit = 5000

var nativeUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var nativeStateDatabase = regexp.MustCompile(`^state_[0-9]+\.sqlite$`)

type nativeMetadataKey struct {
	home, path string
}

type nativeMetadataEntry struct {
	key      nativeMetadataKey
	info     fs.FileInfo
	session  claude.NativeSession
	events   []claude.Event
	weight   int64
	prefix   []byte
	complete bool
}

type nativeMetadataStore struct {
	sync.Mutex
	entries map[nativeMetadataKey]*list.Element
	order   list.List // least recently used first
	bytes   int64
}

var nativeMetadataCache nativeMetadataStore
var nativeHistoryCache nativeMetadataStore

// metadata opens through the confined root even on cache hits, so removed files
// and paths replaced with symlinks cannot resurrect a cached session. The lock
// also avoids duplicate metadata reads by concurrent polling requests.
func (c *nativeMetadataStore) metadata(root *os.Root, path string) (claude.NativeSession, error) {
	s, _, err := c.read(root, path, false)
	return s, err
}

func (c *nativeMetadataStore) read(root *os.Root, path string, history bool) (claude.NativeSession, []claude.Event, error) {
	f, err := openNative(root, path)
	if err != nil {
		return claude.NativeSession{}, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return claude.NativeSession{}, nil, err
	}
	key := nativeMetadataKey{home: root.Name(), path: filepath.ToSlash(filepath.Clean(path))}
	c.Lock()
	defer c.Unlock()
	if c.entries == nil {
		c.entries = make(map[nativeMetadataKey]*list.Element)
	}
	var previous *nativeMetadataEntry
	prefix := make([]byte, min(info.Size(), 256))
	_, _ = f.ReadAt(prefix, 0)
	if e := c.entries[key]; e != nil {
		cached := e.Value.(nativeMetadataEntry)
		if cached.info.Size() == info.Size() && cached.info.ModTime().Equal(info.ModTime()) && os.SameFile(cached.info, info) {
			c.order.MoveToBack(e)
			return cached.session, cached.events[:len(cached.events):len(cached.events)], nil
		}
		c.order.Remove(e)
		c.bytes -= cached.weight
		delete(c.entries, key)
		if history && cached.complete && info.Size() > cached.info.Size() && os.SameFile(cached.info, info) && len(prefix) >= len(cached.prefix) && bytes.Equal(prefix[:len(cached.prefix)], cached.prefix) {
			previous = &cached
		}
	}
	var session claude.NativeSession
	var events []claude.Event
	if previous != nil && !previous.session.Truncated && info.Size() <= nativeHistoryLimit {
		seed := previous.session
		if seed.UpdatedAt == previous.info.ModTime().UTC().Format(time.RFC3339Nano) {
			seed.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
		}
		session, events, err = parseNativeFrom(f, info, true, nativeHistoryLimit, seed, previous.info.Size())
		events = append(previous.events[:len(previous.events):len(previous.events)], events...)
	} else {
		session, events, err = parseNative(f, info, history, nativeHistoryLimit)
	}
	if err != nil {
		return claude.NativeSession{}, nil, err
	}
	// Keep the fingerprint from before parsing. If a writer appends during the
	// read, the next poll sees a mismatch and reads the transcript again.
	var weight int64
	for _, ev := range events {
		weight += int64(len(ev.Text)+len(ev.Payload)+len(ev.CreatedAt)) + 96
	}
	var last [1]byte
	_, _ = f.ReadAt(last[:], info.Size()-1)
	c.entries[key] = c.order.PushBack(nativeMetadataEntry{key: key, info: info, session: session, events: events, weight: weight, prefix: prefix, complete: last[0] == '\n'})
	c.bytes += weight
	maxEntries := nativeFileLimit
	if history {
		maxEntries = 8
	}
	for len(c.entries) > maxEntries || history && c.bytes > nativeHistoryLimit && len(c.entries) > 1 {
		oldest := c.order.Front()
		c.bytes -= oldest.Value.(nativeMetadataEntry).weight
		delete(c.entries, oldest.Value.(nativeMetadataEntry).key)
		c.order.Remove(oldest)
	}
	return session, events[:len(events):len(events)], nil
}

// nativeRoot confines all reads to the selected Codex home, including when a
// directory entry changes between enumeration and opening a transcript.
func nativeRoot(configDir string) (*os.Root, error) {
	if configDir == "" {
		configDir = os.Getenv("CODEX_HOME")
	}
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configDir = filepath.Join(home, ".codex")
	}
	configDir, err := filepath.Abs(configDir)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(configDir)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Codex home must not be a symlink")
	}
	return os.OpenRoot(configDir)
}

func nativeFiles(root *os.Root) ([]string, error) {
	var files []string
	err := fs.WalkDir(root.FS(), "sessions", func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type().IsRegular() && strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
			files = append(files, path)
			if len(files) == nativeFileLimit {
				return fs.SkipAll
			}
		}
		return nil
	})
	return files, err
}

func openNative(root *os.Root, path string) (*os.File, error) {
	// WalkDir never follows symlinks; recheck each component before opening.
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symlink in Codex transcript path")
		}
	}
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		if err == nil {
			err = errors.New("Codex transcript is not a regular file")
		}
		return nil, err
	}
	return f, nil
}

// nativeTitles reads the Codex-maintained session index. An index entry is only
// used after its UUID has been matched to a discovered rollout.
func nativeTitles(root *os.Root) map[string]string {
	titles := make(map[string]string)
	// Codex also maintains titles in its versioned state database. Open it
	// read-only (including its WAL), and tolerate absent or older schemas.
	entries, _ := os.ReadDir(root.Name())
	var database string
	var version int
	for _, entry := range entries {
		var n int
		if entry.Type().IsRegular() && nativeStateDatabase.MatchString(entry.Name()) {
			_, _ = fmt.Sscanf(entry.Name(), "state_%d.sqlite", &n)
			if n > version {
				version, database = n, entry.Name()
			}
		}
	}
	if database != "" {
		path := filepath.Join(root.Name(), database)
		u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
		if !strings.HasPrefix(u.Path, "/") {
			u.Path = "/" + u.Path
		}
		u.RawQuery = url.Values{"mode": {"ro"}, "_pragma": {"query_only(1)", "busy_timeout(1000)"}}.Encode()
		if db, err := sql.Open("sqlite", u.String()); err == nil {
			rows, err := db.Query("SELECT id, COALESCE(NULLIF(name, ''), title) FROM threads")
			if err != nil {
				rows, err = db.Query("SELECT id, title FROM threads")
			}
			if err == nil {
				for rows.Next() {
					var id, title string
					if rows.Scan(&id, &title) == nil && nativeUUID.MatchString(id) && strings.TrimSpace(title) != "" {
						titles[strings.ToLower(id)] = strings.TrimSpace(title)
					}
				}
				rows.Close()
			}
			db.Close()
		}
	}
	f, err := openNative(root, "session_index.jsonl")
	if err != nil {
		return titles
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, nativeHistoryLimit))
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var record struct {
			ID    string `json:"id"`
			Title string `json:"thread_name"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) == nil && nativeUUID.MatchString(record.ID) {
			if title := strings.TrimSpace(record.Title); title != "" {
				titles[strings.ToLower(record.ID)] = title
			}
		}
	}
	return titles
}

// ListNative lists Codex sessions and their titles from the local session index.
func ListNative(configDir string) ([]claude.NativeSession, error) {
	root, err := nativeRoot(configDir)
	if errors.Is(err, os.ErrNotExist) {
		return []claude.NativeSession{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files, err := nativeFiles(root)
	if err != nil {
		return nil, err
	}
	out := make([]claude.NativeSession, 0, len(files))
	titles := nativeTitles(root)
	seen := make(map[string]int)
	for _, path := range files {
		s, err := nativeMetadataCache.metadata(root, path)
		if err != nil || !nativeUUID.MatchString(s.ID) {
			continue
		}
		key := strings.ToLower(s.ID)
		if title := titles[key]; title != "" {
			s.Title = title
		}
		if i, ok := seen[key]; ok {
			if nativeSessionTime(s).After(nativeSessionTime(out[i])) {
				out[i] = s
			}
		} else {
			seen[key] = len(out)
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := nativeSessionTime(out[i]), nativeSessionTime(out[j])
		if !a.Equal(b) {
			return a.After(b)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// NativeHistory returns response items as the shared Claude history event shape.
func NativeHistory(configDir, id string) (claude.NativeSession, []claude.Event, error) {
	if !nativeUUID.MatchString(id) {
		return claude.NativeSession{}, nil, errors.New("invalid Codex session UUID")
	}
	root, err := nativeRoot(configDir)
	if err != nil {
		return claude.NativeSession{}, nil, err
	}
	defer root.Close()
	files, err := nativeFiles(root)
	if err != nil {
		return claude.NativeSession{}, nil, err
	}
	// Normal rollout filenames contain the UUID. Try those first so opening a
	// session does not parse metadata for unrelated files on a cold cache. Keep
	// the metadata fallback for older or renamed transcripts.
	for _, matchingName := range []bool{true, false} {
		for _, path := range files {
			if strings.Contains(strings.ToLower(filepath.Base(path)), strings.ToLower(id)) != matchingName {
				continue
			}
			// Metadata remains authoritative, even for a matching filename.
			s, err := nativeMetadataCache.metadata(root, path)
			if err != nil || !strings.EqualFold(s.ID, id) {
				continue
			}
			s, events, err := nativeHistoryCache.read(root, path, true)
			if err == nil {
				if title := nativeTitles(root)[strings.ToLower(s.ID)]; title != "" {
					s.Title = title
				}
			}
			return s, events, err
		}
	}
	return claude.NativeSession{}, nil, os.ErrNotExist
}

type rolloutRecord struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type         string          `json:"type"`
		ID           string          `json:"id"`
		Cwd          string          `json:"cwd"`
		Timestamp    string          `json:"timestamp"`
		Title        string          `json:"title"`
		SessionTitle string          `json:"session_title"`
		Role         string          `json:"role"`
		Content      json.RawMessage `json:"content"`
		Name         string          `json:"name"`
		CallID       string          `json:"call_id"`
		Arguments    json.RawMessage `json:"arguments"`
		Output       json.RawMessage `json:"output"`
		IsError      bool            `json:"is_error"`
		Message      string          `json:"message"`
	} `json:"payload"`
}

func readNative(root *os.Root, path string, history bool, limit int64) (claude.NativeSession, []claude.Event, error) {
	f, err := openNative(root, path)
	if err != nil {
		return claude.NativeSession{}, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return claude.NativeSession{}, nil, err
	}
	return parseNative(f, info, history, limit)
}

func parseNative(f *os.File, info fs.FileInfo, history bool, limit int64) (claude.NativeSession, []claude.Event, error) {
	s := claude.NativeSession{Provider: "codex", Title: "Codex session", UpdatedAt: info.ModTime().UTC().Format(time.RFC3339Nano)}
	return parseNativeFrom(f, info, history, limit, s, 0)
}

func parseNativeFrom(f *os.File, info fs.FileInfo, history bool, limit int64, s claude.NativeSession, offset int64) (claude.NativeSession, []claude.Event, error) {
	var events []claude.Event
	customTitle, userTitle := s.Title != "Codex session", false
	hasConversation := s.LastConversationAt != ""
	noteActivity := func(stamp string) {
		hasConversation = true
		if stamp = nativeTimestamp(stamp); stamp != "" {
			a, _ := time.Parse(time.RFC3339Nano, stamp)
			b, _ := time.Parse(time.RFC3339Nano, s.LastConversationAt)
			if s.ConversationTimeEstimated || a.After(b) {
				s.LastConversationAt = stamp
				s.ConversationTimeEstimated = false
			}
		}
	}
	var reader io.Reader = io.NewSectionReader(f, offset, min(limit, info.Size()-offset))
	if !history {
		// Listing is polled frequently. Read only bounded metadata windows,
		// including the tail where a later rename or recent timestamp appears.
		const window = 256 << 10
		reader = io.NewSectionReader(f, 0, window)
		if info.Size() > window {
			start := max(int64(window), info.Size()-window)
			tail := io.NewSectionReader(f, start, info.Size()-start)
			if start > window {
				// Discard a partial first tail line without reading past the window.
				buffered := bufio.NewReader(tail)
				for {
					_, err := buffered.ReadSlice('\n')
					if err != bufio.ErrBufferFull {
						break
					}
				}
				reader = io.MultiReader(reader, strings.NewReader("\n"), buffered)
			} else {
				reader = io.MultiReader(reader, tail)
			}
		}
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), int(limit)+1)
	for scanner.Scan() {
		var r rolloutRecord
		if json.Unmarshal(scanner.Bytes(), &r) != nil {
			continue
		}
		p := r.Payload
		if r.Type == "session_meta" {
			if s.CreatedAt == "" {
				s.CreatedAt = nativeTimestamp(p.Timestamp)
				if s.CreatedAt == "" {
					s.CreatedAt = nativeTimestamp(r.Timestamp)
				}
			}
			if nativeUUID.MatchString(p.ID) && s.ID == "" {
				s.ID = p.ID
			}
			if p.Cwd != "" {
				s.Cwd = p.Cwd
			}
			if p.Timestamp != "" {
				s.UpdatedAt = p.Timestamp
			} else if r.Timestamp != "" {
				s.UpdatedAt = r.Timestamp
			}
		}
		if r.Timestamp != "" {
			s.UpdatedAt = r.Timestamp
		}
		if p.Title != "" || p.SessionTitle != "" {
			if r.Type == "session_meta" || r.Type == "session_title" || r.Type == "event_msg" && (p.Type == "session_title" || p.Type == "session_renamed") {
				s.Title = p.Title
				if p.SessionTitle != "" {
					s.Title = p.SessionTitle
				}
				customTitle = true
			}
		}
		// event_msg user_message/agent_message mirror response_item messages.
		// Some versions only timestamp these mirrors, so retain their source time
		// without duplicating the visible response_item history.
		if r.Type == "event_msg" && (p.Type == "user_message" || p.Type == "agent_message") && meaningfulNativeText(p.Message) {
			noteActivity(r.Timestamp)
		}
		if r.Type != "response_item" {
			continue
		}
		var ev claude.Event
		switch p.Type {
		case "reasoning", "custom_tool_call", "custom_tool_call_output":
			// These execution records have no shared visible event shape yet.
			noteActivity(r.Timestamp)
			continue
		case "message":
			if p.Role != "user" && p.Role != "assistant" {
				continue
			}
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(p.Content, &blocks) != nil {
				continue
			}
			var texts []string
			for _, b := range blocks {
				if b.Type == "input_text" || b.Type == "output_text" {
					texts = append(texts, b.Text)
				}
			}
			ev = claude.Event{Kind: p.Role, Text: strings.Join(texts, "\n")}
			if strings.TrimSpace(ev.Text) == "" {
				continue
			}
			if p.Role == "user" && !meaningfulNativeText(ev.Text) {
				continue
			}
			if p.Role == "user" && !customTitle && !userTitle {
				title := []rune(strings.Join(strings.Fields(ev.Text), " "))
				if len(title) > 80 {
					title = title[:80]
				}
				s.Title, userTitle = string(title), true
			}
		case "function_call":
			var input any
			var arguments string
			if json.Unmarshal(p.Arguments, &arguments) == nil {
				if json.Unmarshal([]byte(arguments), &input) != nil {
					input = arguments
				}
			} else if json.Unmarshal(p.Arguments, &input) != nil {
				input = string(p.Arguments)
			}
			payload, _ := json.Marshal(map[string]any{"id": p.CallID, "name": p.Name, "input": input})
			ev = claude.Event{Kind: "tool", Payload: payload}
		case "function_call_output":
			var content any
			if json.Unmarshal(p.Output, &content) != nil {
				content = string(p.Output)
			}
			text, ok := content.(string)
			if !ok {
				text = string(p.Output)
			}
			payload, _ := json.Marshal(map[string]any{"tool_use_id": p.CallID, "content": content, "is_error": p.IsError})
			ev = claude.Event{Kind: "tool_result", Text: text, Payload: payload}
		default:
			continue
		}
		ev.CreatedAt = nativeTimestamp(r.Timestamp)
		noteActivity(r.Timestamp)
		if history {
			events = append(events, ev)
		}
	}
	s.Truncated = history && info.Size()-offset > limit
	if hasConversation && (s.LastConversationAt == "" || s.ConversationTimeEstimated) {
		s.LastConversationAt = info.ModTime().UTC().Format(time.RFC3339Nano)
		s.ConversationTimeEstimated = true
	}
	if s.Truncated {
		// History retains its existing size bound; metadata windows still give
		// the recent source activity even when visible events were truncated.
		if recent, _, err := parseNative(f, info, false, limit); err == nil {
			s.LastConversationAt, s.ConversationTimeEstimated = recent.LastConversationAt, recent.ConversationTimeEstimated
		}
	}
	return s, events, scanner.Err()
}

func nativeTimestamp(value string) string {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func nativeSessionTime(s claude.NativeSession) time.Time {
	value := s.LastConversationAt
	if value == "" {
		value = s.CreatedAt
	}
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}

func meaningfulNativeText(text string) bool {
	text = strings.TrimSpace(text)
	for _, prefix := range []string{"<environment_context>", "<permissions instructions>", "<INSTRUCTIONS>", "# AGENTS.md instructions", "<turn_aborted>", "<system-reminder>"} {
		if strings.HasPrefix(text, prefix) {
			return false
		}
	}
	return text != ""
}

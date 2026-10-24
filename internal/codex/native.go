// Package codex reads local Codex rollout transcripts.
package codex

import (
	"bufio"
	"container/list"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"cc-remote/internal/claude"
)

const nativeHistoryLimit int64 = 64 << 20
const nativeFileLimit = 5000

var nativeUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type nativeMetadataKey struct {
	home, path string
}

type nativeMetadataEntry struct {
	key     nativeMetadataKey
	info    fs.FileInfo
	session claude.NativeSession
}

type nativeMetadataStore struct {
	sync.Mutex
	entries map[nativeMetadataKey]*list.Element
	order   list.List // least recently used first
}

var nativeMetadataCache nativeMetadataStore

// metadata opens through the confined root even on cache hits, so removed files
// and paths replaced with symlinks cannot resurrect a cached session. The lock
// also avoids duplicate metadata reads by concurrent polling requests.
func (c *nativeMetadataStore) metadata(root *os.Root, path string) (claude.NativeSession, error) {
	f, err := openNative(root, path)
	if err != nil {
		return claude.NativeSession{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return claude.NativeSession{}, err
	}
	key := nativeMetadataKey{home: root.Name(), path: filepath.ToSlash(filepath.Clean(path))}
	c.Lock()
	defer c.Unlock()
	if c.entries == nil {
		c.entries = make(map[nativeMetadataKey]*list.Element)
	}
	if e := c.entries[key]; e != nil {
		cached := e.Value.(nativeMetadataEntry)
		if cached.info.Size() == info.Size() && cached.info.ModTime().Equal(info.ModTime()) && os.SameFile(cached.info, info) {
			c.order.MoveToBack(e)
			return cached.session, nil
		}
		c.order.Remove(e)
		delete(c.entries, key)
	}
	session, _, err := parseNative(f, info, false, nativeHistoryLimit)
	if err != nil {
		return claude.NativeSession{}, err
	}
	// Keep the fingerprint from before parsing. If a writer appends during the
	// read, the next poll sees a mismatch and reads the transcript again.
	c.entries[key] = c.order.PushBack(nativeMetadataEntry{key, info, session})
	if len(c.entries) > nativeFileLimit {
		oldest := c.order.Front()
		delete(c.entries, oldest.Value.(nativeMetadataEntry).key)
		c.order.Remove(oldest)
	}
	return session, nil
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

// ListNative lists Codex sessions without reading any other local history.
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
	seen := make(map[string]int)
	for _, path := range files {
		s, err := nativeMetadataCache.metadata(root, path)
		if err != nil || !nativeUUID.MatchString(s.ID) {
			continue
		}
		key := strings.ToLower(s.ID)
		if i, ok := seen[key]; ok {
			if s.UpdatedAt > out[i].UpdatedAt {
				out[i] = s
			}
		} else {
			seen[key] = len(out)
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
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
			return readNative(root, path, true, nativeHistoryLimit)
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
	var events []claude.Event
	customTitle, userTitle := false, false
	var reader io.Reader = io.LimitReader(f, limit)
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
		if r.Type != "response_item" {
			continue
		}
		var ev claude.Event
		switch p.Type {
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
			if p.Role == "user" && !customTitle && !userTitle && meaningfulNativeText(ev.Text) {
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
		if history {
			events = append(events, ev)
		}
	}
	s.Truncated = history && info.Size() > limit
	return s, events, scanner.Err()
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

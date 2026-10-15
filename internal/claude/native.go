package claude

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type NativeSession struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Cwd         string `json:"cwd"`
	UpdatedAt   string `json:"updatedAt"`
	customTitle bool
}

func nativeFiles(configDir string) ([]string, error) {
	if configDir == "" {
		configDir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configDir = filepath.Join(home, ".claude")
	}
	projects := filepath.Join(configDir, "projects")
	dirs, err := os.ReadDir(projects)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
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
			if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".jsonl") && uuidPattern.MatchString(id) {
				files = append(files, filepath.Join(projects, d.Name(), e.Name()))
				if len(files) >= 5000 {
					return files, nil
				}
			}
		}
	}
	return files, nil
}

// ListNative reads transcript metadata. It does not attach to running terminals.
func ListNative(configDir string) ([]NativeSession, error) {
	files, err := nativeFiles(configDir)
	if err != nil {
		return nil, err
	}
	out := make([]NativeSession, 0, len(files))
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			continue
		}
		session := NativeSession{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Title: "Claude session", UpdatedAt: info.ModTime().UTC().Format(time.RFC3339Nano)}
		// Inspect bounded head/tail windows rather than loading every full transcript.
		data, _ := io.ReadAll(io.LimitReader(f, 256*1024))
		inspectMetadata(data, &session)
		if info.Size() > 256*1024 {
			_, _ = f.Seek(max(0, info.Size()-256*1024), io.SeekStart)
			tail, _ := io.ReadAll(io.LimitReader(f, 256*1024))
			if i := strings.IndexByte(string(tail), '\n'); i >= 0 {
				inspectMetadata(tail[i+1:], &session)
			}
		}
		f.Close()
		out = append(out, session)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, out[i].UpdatedAt)
		b, _ := time.Parse(time.RFC3339Nano, out[j].UpdatedAt)
		return a.After(b)
	})
	return out, nil
}

func inspectMetadata(data []byte, s *NativeSession) {
	for _, line := range strings.Split(string(data), "\n") {
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
		if json.Unmarshal([]byte(line), &v) != nil {
			continue
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
}

func NativeHistory(configDir, id string) (NativeSession, []Event, error) {
	if !uuidPattern.MatchString(id) {
		return NativeSession{}, nil, errors.New("invalid Claude session UUID")
	}
	files, err := nativeFiles(configDir)
	if err != nil {
		return NativeSession{}, nil, err
	}
	for _, path := range files {
		if !strings.EqualFold(strings.TrimSuffix(filepath.Base(path), ".jsonl"), id) {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return NativeSession{}, nil, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return NativeSession{}, nil, err
		}
		if info.Size() > 64*1024*1024 {
			return NativeSession{}, nil, errors.New("transcript exceeds 64 MiB history limit")
		}
		session := NativeSession{ID: id, Title: "Claude session", UpdatedAt: info.ModTime().UTC().Format(time.RFC3339Nano)}
		var events []Event
		scanner := bufio.NewScanner(io.LimitReader(f, 64*1024*1024))
		scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			inspectMetadata(line, &session)
			var v struct {
				Type        string `json:"type"`
				IsMeta      bool   `json:"isMeta"`
				IsSidechain bool   `json:"isSidechain"`
				Message     struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(line, &v) != nil || v.IsMeta || v.IsSidechain || (v.Type != "user" && v.Type != "assistant") {
				continue
			}
			for _, e := range contentEvents(v.Message.Content, v.Type) {
				if e.Kind == "user" && syntheticText(e.Text) {
					continue
				}
				events = append(events, e)
			}
		}
		return session, events, scanner.Err()
	}
	return NativeSession{}, nil, os.ErrNotExist
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

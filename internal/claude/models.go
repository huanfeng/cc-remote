package claude

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// recentTTL bounds how often the transcript directory is walked and tails are read;
// every distinct working directory asks for the same machine-wide list.
const recentTTL = 2 * time.Minute

type recentKey struct {
	configDir string
	maxFiles  int
}

var recentCache struct {
	sync.Mutex // held during a scan so concurrent callers wait for one walk
	entries    map[recentKey]recentEntry
}

type recentEntry struct {
	models []string
	at     time.Time
}

// RecentModels returns model IDs from assistant messages in the most recently
// modified native transcripts, newest first. Only a bounded tail of each file is
// read, and results are reused for recentTTL.
func RecentModels(configDir string, maxFiles int) []string {
	recentCache.Lock()
	defer recentCache.Unlock()
	key := recentKey{configDir, maxFiles}
	if e, ok := recentCache.entries[key]; ok && time.Since(e.at) < recentTTL {
		return slices.Clone(e.models)
	}
	out := recentModels(configDir, maxFiles)
	if recentCache.entries == nil {
		recentCache.entries = map[recentKey]recentEntry{}
	}
	recentCache.entries[key] = recentEntry{out, time.Now()}
	return slices.Clone(out)
}

func recentModels(configDir string, maxFiles int) []string {
	files, err := nativeFiles(configDir)
	if err != nil {
		return nil
	}
	type entry struct {
		path string
		mod  time.Time
	}
	entries := make([]entry, 0, len(files))
	for _, path := range files {
		if info, err := os.Stat(path); err == nil {
			entries = append(entries, entry{path, info.ModTime()})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mod.After(entries[j].mod) })
	if len(entries) > maxFiles {
		entries = entries[:maxFiles]
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		for _, id := range transcriptModels(e.path) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

const modelTail = 128 * 1024

// transcriptModels returns distinct models in the file tail, latest message first.
func transcriptModels(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	if info.Size() > modelTail {
		_, _ = f.Seek(info.Size()-modelTail, io.SeekStart)
	}
	data, _ := io.ReadAll(io.LimitReader(f, modelTail))
	if info.Size() > modelTail {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	lines := bytes.Split(data, []byte("\n"))
	var out []string
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"model"`)) {
			continue
		}
		var v struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if json.Unmarshal(lines[i], &v) != nil || v.Type != "assistant" || !plausibleModel(v.Message.Model) {
			continue
		}
		if !slices.Contains(out, v.Message.Model) {
			out = append(out, v.Message.Model)
		}
	}
	return out
}

// plausibleModel rejects synthetic placeholders such as "<synthetic>".
func plausibleModel(id string) bool {
	if id == "" || len(id) > 200 || strings.ContainsAny(id, "<> \t\r\n") {
		return false
	}
	return true
}

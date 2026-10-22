package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// claudeTrusts reports whether Claude Code already trusts dir: the folder trust
// dialog was accepted for it or a folder above it, so Claude Code starts there
// without asking. A terminal session may then start without trustFolder.
func (s *Server) claudeTrusts(dir string) bool {
	path := filepath.Join(s.cfg.ClaudeConfigEnv, ".claude.json")
	if s.cfg.ClaudeConfigEnv == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		path = filepath.Join(home, ".claude.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var state struct {
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	if json.Unmarshal(data, &state) != nil {
		return false
	}
	trusted := map[string]bool{}
	for key, p := range state.Projects {
		if p.Trusted {
			trusted[trustKey(key)] = true
		}
	}
	for d := filepath.Clean(dir); ; {
		if trusted[trustKey(d)] {
			return true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return false
		}
		d = parent
	}
}

// trustKey is a path as Claude Code's project keys write it (forward slashes).
func trustKey(path string) string {
	key := strings.TrimSuffix(filepath.ToSlash(filepath.Clean(path)), "/")
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		key = strings.ToLower(key)
	}
	return key
}

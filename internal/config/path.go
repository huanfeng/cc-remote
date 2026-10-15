package config

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultDataDir is independent of the directory in which Claude is launched.
func DefaultDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("CC_REMOTE_DATA_DIR")); dir != "" {
		return dir
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "cc-remote")
	}
	if dir, err := os.UserHomeDir(); err == nil {
		return filepath.Join(dir, ".cc-remote")
	}
	return "data"
}

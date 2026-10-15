package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDataDir(t *testing.T) {
	t.Setenv("CC_REMOTE_DATA_DIR", "")
	user, err := os.UserConfigDir()
	if err != nil {
		t.Skip(err)
	}
	if got := DefaultDataDir(); got != filepath.Join(user, "cc-remote") {
		t.Fatal(got)
	}
	t.Setenv("CC_REMOTE_DATA_DIR", filepath.Join(t.TempDir(), "custom"))
	if DefaultDataDir() != os.Getenv("CC_REMOTE_DATA_DIR") {
		t.Fatal("ignored explicit data directory")
	}
}

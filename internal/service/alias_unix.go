//go:build !windows

package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

func defaultAliasDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

func ensureOnPath(dir string, _ bool, out io.Writer) error {
	if PathContains(os.Getenv("PATH"), dir, ":") {
		return nil
	}
	rc := "~/.profile (or ~/.bashrc)"
	if runtime.GOOS == "darwin" {
		rc = "~/.zshrc"
	}
	fmt.Fprintf(out, "warning: %s is not on PATH; add this line to %s:\n  export PATH=\"%s:$PATH\"\n", dir, rc, dir)
	return nil
}

func removeFromPath(string, bool, io.Writer) error { return nil }

func pathStatus(string, io.Writer) {}

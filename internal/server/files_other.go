//go:build !windows

package server

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// finalPath is the real path of the open file f, opened as path: symlinks
// resolved, and checked to still be the file that was opened.
func finalPath(f *os.File, path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	now, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !os.SameFile(opened, now) {
		return "", errors.New("the file changed while it was opened")
	}
	return real, nil
}

func hideWindow(*exec.Cmd) {}

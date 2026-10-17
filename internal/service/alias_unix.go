//go:build !windows

package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"cc-remote/internal/i18n"
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
	rc := i18n.T("~/.profile (or ~/.bashrc)", "~/.profile（或 ~/.bashrc）")
	if runtime.GOOS == "darwin" {
		rc = "~/.zshrc"
	}
	fmt.Fprint(out, warning(i18n.F("%s is not on PATH; add this line to %s:\n  export PATH=\"%s:$PATH\"", "%s 不在 PATH 中；请在 %s 中加入这一行：\n  export PATH=\"%s:$PATH\"", dir, rc, dir)))
	return nil
}

func removeFromPath(string, bool, io.Writer) error { return nil }

func pathStatus(string, io.Writer) {}

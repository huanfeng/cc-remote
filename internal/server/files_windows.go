package server

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// finalPath is the path of an open file as Windows resolves it, through
// symlinks and junctions (which filepath.EvalSymlinks cannot follow).
func finalPath(f *os.File, _ string) (string, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	p := windows.UTF16ToString(buf[:n])
	if rest, ok := strings.CutPrefix(p, `\\?\UNC\`); ok {
		return `\\` + rest, nil
	}
	return strings.TrimPrefix(p, `\\?\`), nil
}

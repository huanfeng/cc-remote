//go:build !windows

package terminal

import (
	"os/exec"
	"runtime"
)

// openFile opens a file with its default program.
func openFile(path string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	cmd := exec.Command(name, path)
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

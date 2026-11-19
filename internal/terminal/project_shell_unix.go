//go:build !windows

package terminal

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func projectShellCommand() (string, []string, error) {
	if path, err := exec.LookPath(os.Getenv("SHELL")); err == nil {
		return path, []string{"-i"}, nil
	}
	path, err := exec.LookPath("/bin/sh")
	return path, []string{"-i"}, err
}

func stopProjectShellProcess(process *os.Process, exited <-chan struct{}) {
	// Ask the interactive shell to hang up the jobs it knows about before
	// falling back to a forced exit. The PTY close hangs up its foreground job.
	_ = process.Signal(syscall.SIGHUP)
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-exited:
	case <-timer.C:
		_ = process.Kill()
	}
}

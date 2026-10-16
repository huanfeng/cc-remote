//go:build !windows

package terminal

import "syscall"

// killTree ends a process and every process it started: a process on a
// pseudo-terminal leads its own session and process group.
func killTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

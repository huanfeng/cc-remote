//go:build !windows

package channel

import (
	"os/exec"
	"syscall"
)

func detachHost(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

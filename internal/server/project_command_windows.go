package server

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

func configureProjectCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Cancel = func() error {
		// Retain the process handle while using its PID to terminate descendants.
		_ = cmd.Process.WithHandle(func(handle uintptr) {
			if code, err := windows.WaitForSingleObject(windows.Handle(handle), 0); err != nil || code != uint32(windows.WAIT_TIMEOUT) {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			killer := exec.CommandContext(ctx, "taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
			killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			_ = killer.Run()
		})
		return cmd.Process.Kill()
	}
}

func projectCommandExitCode(err *exec.ExitError) int {
	return err.ExitCode()
}

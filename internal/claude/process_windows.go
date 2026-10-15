package claude

import (
	"os/exec"
	"strconv"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	killer := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
	killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = killer.Run()
	_ = cmd.Process.Kill()
}

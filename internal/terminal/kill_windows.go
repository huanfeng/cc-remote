package terminal

import (
	"os/exec"
	"strconv"
)

// killTree ends a process and every process it started.
func killTree(pid int) {
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}

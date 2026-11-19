package terminal

import (
	"os"
	"os/exec"

	"golang.org/x/sys/windows"
)

func projectShellCommand() (string, []string, error) {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, []string{"-NoLogo"}, nil
		}
	}
	if path, err := exec.LookPath(os.Getenv("ComSpec")); err == nil {
		return path, []string{"/D"}, nil
	}
	path, err := exec.LookPath("cmd.exe")
	return path, []string{"/D"}, err
}

func stopProjectShellProcess(process *os.Process, _ <-chan struct{}) {
	// Holding the Windows process handle keeps this process identity valid
	// while taskkill uses its PID to terminate the remaining process tree.
	_ = process.WithHandle(func(handle uintptr) {
		if result, err := windows.WaitForSingleObject(windows.Handle(handle), 0); err == nil && result == uint32(windows.WAIT_TIMEOUT) {
			killTree(process.Pid)
			_ = process.Kill()
		}
	})
}

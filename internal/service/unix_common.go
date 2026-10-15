//go:build linux || darwin

package service

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func runQuiet(name string, args ...string) (string, error) {
	b, err := exec.Command(name, args...).CombinedOutput()
	out := strings.TrimSpace(string(b))
	if err != nil {
		if out != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

func installBinaries(src string, dryRun bool, out io.Writer) error {
	dst, err := InstalledExe()
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Fprintf(out, "[dry-run] copy %s -> %s\n", src, dst)
		return nil
	}
	if !samePath(src, dst) {
		if err := copyExecutable(src, dst, nil); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Installed %s\n", dst)
	return nil
}

// serviceEnv captures the install-time PATH: service managers start with a
// minimal PATH that usually lacks claude and node.
func serviceEnv(manager string) map[string]string {
	env := map[string]string{"CC_REMOTE_SERVICE_MANAGER": manager}
	if p := os.Getenv("PATH"); p != "" {
		env["PATH"] = p
	}
	return env
}

func homeDir(fallback string) string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return fallback
}

func firewallHint([]string, io.Writer) {}

// Supervise is only used by the Windows scheduled task.
func Supervise([]string) error { return fmt.Errorf("supervise is only used on Windows") }

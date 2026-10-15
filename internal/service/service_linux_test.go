//go:build linux

package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderUnitWorkingDirectoryIsLiteral(t *testing.T) {
	t.Setenv("HOME", "/home/me/50% dir")
	unit := renderUnit(&Config{Exe: "/bin/true", DataDir: "/home/me/.config/cc-remote", Listen: []string{"127.0.0.1:8787"}})
	if !strings.Contains(unit, "\nWorkingDirectory=/home/me/50%% dir\n") {
		t.Fatalf("WorkingDirectory must be unquoted with %% escaped:\n%s", unit)
	}
}

// TestRenderUnitAcceptedBySystemd lets systemd itself parse the generated unit
// (catches settings that are valid for our quoting rules but not for systemd).
func TestRenderUnitAcceptedBySystemd(t *testing.T) {
	analyze, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := &Config{
		Exe:     "/bin/true",
		DataDir: filepath.Join(home, ".config", "cc-remote"),
		Listen:  []string{"127.0.0.1:8787", "lan:8787"},
		Env:     map[string]string{"HTTPS_PROXY": "http://127.0.0.1:7890", "NO_PROXY": "localhost,127.0.0.1,192.168.0.0/16"},
	}
	path := filepath.Join(t.TempDir(), unitName)
	if err := os.WriteFile(path, []byte(renderUnit(cfg)), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(analyze, "verify", path).CombinedOutput()
	// verify also reports unrelated host problems (e.g. missing dependencies of
	// other units); only complaints about our unit file matter here.
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, unitName) || strings.Contains(line, path) {
			t.Fatalf("systemd rejected the unit:\n%s\n--- unit ---\n%s", out, renderUnit(cfg))
		}
	}
	if err != nil {
		t.Logf("systemd-analyze exited with %v (no complaints about %s):\n%s", err, unitName, out)
	}
}

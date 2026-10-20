package service

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An update run from a shell whose PATH lacks node (ssh without the login
// profile) keeps the installed unit's directories, and warns only when node is
// not found on the result.
func TestServiceEnvKeepsInstalledPath(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	nvm := filepath.Join(t.TempDir(), "nvm", "bin")
	if err := os.MkdirAll(nvm, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	var out bytes.Buffer
	pathHint(&Config{}, &out)
	if !strings.Contains(out.String(), "node") {
		t.Fatalf("no node anywhere: want a warning, got %q", out.String())
	}
	unit := SystemdUnit("/bin/cc-remote", nil, map[string]string{"PATH": nvm + ":/usr/bin"})
	if err := os.MkdirAll(filepath.Dir(unitPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unitPath(), []byte(unit), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := serviceEnv(managerName)["PATH"], "/usr/bin:/bin:"+nvm; got != want {
		t.Fatalf("PATH = %q, want %q", got, want)
	}
	if err := os.WriteFile(filepath.Join(nvm, "node"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	pathHint(&Config{}, &out)
	if out.Len() != 0 {
		t.Fatalf("node in the kept PATH: want no warning, got %q", out.String())
	}
}

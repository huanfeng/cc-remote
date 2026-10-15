//go:build !windows

package service

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkCommandOnUnix(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(t.TempDir(), "cc-remote")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "cc-remote")

	if err := linkCommand(dir, exe, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(link); err != nil || got != exe {
		t.Fatalf("link = %q, %v; want %q", got, err, exe)
	}

	// Re-linking replaces an existing symlink (e.g. after moving the install).
	other := filepath.Join(t.TempDir(), "cc-remote")
	if err := os.WriteFile(other, nil, 0755); err != nil {
		t.Fatal(err)
	}
	if err := linkCommand(dir, other, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(link); got != other {
		t.Fatalf("relink = %q, want %q", got, other)
	}

	unlinkCommand(dir, false, io.Discard)
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("link still present after unlink: %v", err)
	}
}

func TestLinkCommandKeepsUserFile(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "cc-remote")
	if err := os.WriteFile(link, []byte("user script"), 0755); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := linkCommand(dir, "/opt/cc-remote/bin/cc-remote", false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not a symlink") {
		t.Fatalf("expected a warning, got %q", out.String())
	}
	unlinkCommand(dir, false, io.Discard)
	if b, err := os.ReadFile(link); err != nil || string(b) != "user script" {
		t.Fatalf("user file changed: %q, %v", b, err)
	}
}

package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.log")
	w, err := Open(path, 32)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("a", 15) + "\n"
	for i := 0; i < 2; i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Write([]byte("b\n")); err != nil {
		t.Fatal(err)
	}
	w.Close()
	backup, err := os.ReadFile(path + ".1")
	if err != nil || string(backup) != line+line {
		t.Fatalf("backup = %q, %v", backup, err)
	}
	current, _ := os.ReadFile(path)
	if string(current) != "b\n" {
		t.Fatalf("current = %q", current)
	}
}

func TestAppendsExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.log")
	os.WriteFile(path, []byte("old\n"), 0600)
	w, err := Open(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("new\n"))
	w.Close()
	b, _ := os.ReadFile(path)
	if string(b) != "old\nnew\n" {
		t.Fatalf("got %q", b)
	}
}

// The crash-output handle must not block rotation (Windows refuses to rename
// a file that another handle keeps open).
func TestCrashOutputKeepsRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.log")
	w, err := Open(path, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.SetCrashOutput(); err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("c", 20) + "\n"
	w.Write([]byte(line))
	w.Write([]byte(line))
	if b, err := os.ReadFile(path + ".1"); err != nil || string(b) != line {
		t.Fatalf("backup = %q, %v", b, err)
	}
}

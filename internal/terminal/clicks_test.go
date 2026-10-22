package terminal

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestClickOpensPath(t *testing.T) {
	file := "/tmp/cc/v2.png"
	if runtime.GOOS == "windows" {
		file = `C:\tmp\v2.png`
	}
	opened := make(chan string, 4)
	c := newClicker(60, 5, "", func(path string) { opened <- path })
	c.l.exists = func(path string) bool { return path == file }
	// Row 2: "  ⎿ 浅色：<path> 已读取", drawn as Claude Code does, with colors and a cursor move.
	c.observe([]byte("\x1b[2J\x1b[H● Read(1 file)\r\n  ⎿ 浅色：\x1b[1m" + file + "\x1b[22m\x1b[1C已读取\r\n"))
	start := 11 // "  ⎿ " takes 4 cells and "浅色：" 6: the path starts at column 11 (1-based)
	click := func(x, y int, button int) string {
		b := strconv.Itoa(button)
		return "\x1b[<" + b + ";" + strconv.Itoa(x) + ";" + strconv.Itoa(y) + "M\x1b[<" + b + ";" + strconv.Itoa(x) + ";" + strconv.Itoa(y) + "m"
	}
	for _, x := range []int{start, start + len(file) - 1} {
		if got := string(c.filter([]byte("a" + click(x, 2, mouseCtrl) + "b"))); got != "ab" {
			t.Fatalf("click at %d on the path reached Claude Code: %q", x, got)
		}
		select {
		case p := <-opened:
			if p != file {
				t.Fatalf("opened %q", p)
			}
		case <-time.After(time.Second):
			t.Fatal("the file was not opened")
		}
	}
	// Next to the path, on another row, a plain click (Claude Code expands a tool call with it),
	// another button or another modifier too, or the wheel: Claude Code's.
	for _, in := range []string{click(start-1, 2, mouseCtrl), click(start+len(file), 2, mouseCtrl), click(start, 1, mouseCtrl), click(start, 2, 0), click(start, 2, 2), click(start, 2, mouseCtrl|4), "\x1b[<64;12;2M", "\x1b[<35;12;2M"} {
		if got := string(c.filter([]byte(in))); got != in {
			t.Errorf("%q changed to %q", in, got)
		}
	}
	if len(opened) > 0 {
		t.Fatalf("opened %q", <-opened)
	}
	if !slices.Equal(c.filter([]byte("plain keys")), []byte("plain keys")) {
		t.Fatal("keys changed")
	}
}

// Claude Code shows a tool's file relative to the session's directory.
func TestClickOpensRelativePath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "web", "src", "App.tsx")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	opened := make(chan string, 4)
	c := newClicker(60, 5, dir, func(path string) { opened <- path })
	c.observe([]byte("\x1b[2J\x1b[H● Update(web/src/App.tsx)\r\n  ⎿ Read 2 files\r\n"))
	// "● Update(" is 9 cells: the path is columns 10 to 24.
	for _, x := range []int{10, 24} {
		if got := string(c.filter([]byte("\x1b[<16;" + strconv.Itoa(x) + ";1M\x1b[<16;" + strconv.Itoa(x) + ";1m"))); got != "" {
			t.Fatalf("click at %d reached Claude Code: %q", x, got)
		}
		if p := <-opened; p != file {
			t.Fatalf("opened %q, want %q", p, file)
		}
	}
	// "Update", "Read" and "files" are no files: Claude Code gets those clicks.
	for _, at := range [][2]int{{4, 1}, {25, 1}, {6, 2}, {12, 2}} {
		in := "\x1b[<16;" + strconv.Itoa(at[0]) + ";" + strconv.Itoa(at[1]) + "M"
		if got := string(c.filter([]byte(in))); got != in {
			t.Errorf("click at %v taken: %q", at, got)
		}
	}
}

// A long path Claude Code broke at the screen's edge opens from either row.
func TestClickOpensWrappedPath(t *testing.T) {
	file := "/home/me/projects/screenshots/v2.png"
	if runtime.GOOS == "windows" {
		file = `C:\Users\me\projects\shots\v2.png`
	}
	opened := make(chan string, 4)
	c := newClicker(20, 4, "", func(path string) { opened <- path })
	c.l.exists = func(path string) bool { return path == file }
	// "● " then the path to the edge (20 cells), the rest in the next row after a two-space indent.
	c.observe([]byte("\x1b[2J\x1b[H● " + file[:18] + "\x1b[2;3H" + file[18:] + "\x1b[3;1H────────────────────"))
	for _, at := range [][2]int{{3, 1}, {20, 1}, {3, 2}} {
		in := "\x1b[<16;" + strconv.Itoa(at[0]) + ";" + strconv.Itoa(at[1]) + "M"
		if got := string(c.filter([]byte(in))); got != "" {
			t.Fatalf("click at %v reached Claude Code: %q", at, got)
		}
		if p := <-opened; p != file {
			t.Fatalf("opened %q", p)
		}
	}
}

// Ctrl may be let go before the button: the release is taken all the same.
func TestCtrlClickReleaseWithoutCtrl(t *testing.T) {
	c := newClicker(30, 2, "", func(string) {})
	c.l.exists = func(path string) bool { return true }
	c.observe([]byte("\x1b[H/tmp/x.png " + `C:\tmp\x.png`))
	x := 2
	if runtime.GOOS == "windows" {
		x = 15
	}
	press := "\x1b[<16;" + strconv.Itoa(x) + ";1M"
	if got := string(c.filter([]byte(press))); got != "" {
		t.Fatalf("press reached Claude Code: %q", got)
	}
	if got := string(c.filter([]byte("\x1b[<0;" + strconv.Itoa(x) + ";1m"))); got != "" {
		t.Fatalf("release reached Claude Code: %q", got)
	}
	if got := string(c.filter([]byte("\x1b[<0;1;1m"))); got == "" {
		t.Fatal("a later release was taken too")
	}
}

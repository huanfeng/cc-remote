package terminal

import (
	"runtime"
	"slices"
	"strings"
	"testing"
)

func testLinker(files ...string) *linker {
	l := newLinker()
	l.exists = func(path string) bool { return slices.Contains(files, path) }
	return l
}

func linked(path string) string {
	return "\x1b]8;;" + fileURL(path) + "\x1b\\" + path + "\x1b]8;;\x1b\\"
}

func TestLinkPaths(t *testing.T) {
	file, dir, url := "/home/me/shots/v2.png", "/home/me/新建 文件夹", "https://example.com/home/me/shots/v2.png"
	if runtime.GOOS == "windows" {
		file, dir, url = `C:\Users\me\shots\v2.png`, `D:\新建 文件夹`, "https://example.com/C:/Users/me/shots/v2.png"
	}
	l := testLinker(file, dir)
	for _, c := range []struct{ in, want string }{
		{"截图在：" + file + "。", "截图在：" + linked(file) + "。"},
		{"浅色：" + file + "里面", "浅色：" + linked(file) + "里面"},
		{"see " + file + ".", "see " + linked(file) + "."},
		{"\x1b[1m" + file + "\x1b[22m:12", "\x1b[1m" + linked(file) + "\x1b[22m:12"},
		{"(" + file + ")", "(" + linked(file) + ")"},
		// ConPTY sets the colors again where it wraps a long line.
		{"\x1b[1m" + file[:9] + "\x1b[m\x1b[38;5;246m" + file[9:] + "\x1b[22m", "\x1b[1m\x1b]8;;" + fileURL(file) + "\x1b\\" + file[:9] + "\x1b[m\x1b[38;5;246m" + file[9:] + "\x1b]8;;\x1b\\\x1b[22m"},
		{url, url},
		{"missing " + file + "x", "missing " + file + "x"},
		{dir, dir}, // a space ends a path: "新建" is no file
	} {
		if got := string(l.write([]byte(c.in))) + string(l.flush()); got != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	if runtime.GOOS == "windows" {
		if got := string(testLinker(file).write([]byte("/c/Users/me/x " + file + " "))); got != "/c/Users/me/x "+linked(file)+" " {
			t.Errorf("unix-style path on Windows: %q", got)
		}
	}
}

// A chunk may end inside a path or an escape sequence: the rest comes next.
func TestLinkAcrossChunks(t *testing.T) {
	file := "/tmp/cc/a.png"
	if runtime.GOOS == "windows" {
		file = `C:\tmp\a.png`
	}
	l := testLinker(file)
	whole := "x " + file + " \x1b[31my\x1b]0;title\x07"
	for cut := 1; cut < len(whole); cut++ {
		got := string(l.write([]byte(whole[:cut]))) + string(l.write([]byte(whole[cut:]))) + string(l.flush())
		if want := "x " + linked(file) + " \x1b[31my\x1b]0;title\x07"; got != want {
			t.Fatalf("cut at %d:\n got %q\nwant %q", cut, got, want)
		}
	}
}

// Inside a hyperlink the stream opened itself, paths stay as they are.
func TestLinkInsideLink(t *testing.T) {
	file := "/tmp/cc/a.png"
	if runtime.GOOS == "windows" {
		file = `C:\tmp\a.png`
	}
	l := testLinker(file)
	in := "\x1b]8;;https://x.test\x1b\\" + file + "\x1b]8;;\x1b\\ " + file + " "
	got := string(l.write([]byte(in)))
	if want := "\x1b]8;;https://x.test\x1b\\" + file + "\x1b]8;;\x1b\\ " + linked(file) + " "; got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if !strings.Contains(fileURL(`C:\a b\图.png`), "%20") && runtime.GOOS == "windows" {
		t.Fatal("spaces must be escaped in the URL")
	}
}

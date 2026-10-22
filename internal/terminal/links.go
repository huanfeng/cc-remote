package terminal

import (
	"bytes"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Most terminals do not see a bare path as a link (Windows Terminal detects
// only URLs), so ccr marks the absolute paths of files on this machine in the
// session's output as OSC 8 hyperlinks: a click opens the file. An OSC 8
// sequence takes no cell, so nothing on the screen moves; it is added only for
// a host on this machine (the paths are its paths) and a terminal known to
// support it, where any other would print it.

// hyperlinks reports whether the local terminal shows OSC 8 hyperlinks;
// CC_REMOTE_HYPERLINKS=1 or 0 decides for any terminal.
func hyperlinks() bool {
	switch os.Getenv("CC_REMOTE_HYPERLINKS") {
	case "1":
		return true
	case "0":
		return false
	}
	// A multiplexer drops them or needs its own setting.
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		return false
	}
	if os.Getenv("WT_SESSION") != "" || os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("WEZTERM_PANE") != "" || os.Getenv("ALACRITTY_WINDOW_ID") != "" || os.Getenv("TERMINAL_EMULATOR") == "JetBrains-JediTerm" {
		return true
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "vscode", "iTerm.app", "WezTerm", "ghostty", "Hyper", "Tabby":
		return true
	}
	v, _ := strconv.Atoi(os.Getenv("VTE_VERSION"))
	return v >= 5000
}

// A path runs to a space, quote, bracket or Chinese punctuation; ":12" after a
// file name is not part of it. Whether it is a path at all is up to the disk.
var pathRE = regexp.MustCompile(`(?:[A-Za-z]:[\\/]|/)[^\s"'` + "`" + `<>|*?:;,()\[\]{}，。、：；！？（）【】《》「」“”‘’]+`)

// Cut from the end of a path: sentence punctuation.
const trailing = ".!'\""

// The longest held-back tail: past it, output goes out as it is.
const maxPending = 4096

// statWait bounds the wait for a path's file: a disconnected network drive
// would hold the screen for seconds. Such a path stays plain.
const statWait = 50 * time.Millisecond

// linker adds the hyperlinks to the output stream. A chunk may end inside an
// escape sequence or a path, so that tail waits for the next chunk.
type linker struct {
	pending []byte
	inLink  bool // inside a hyperlink the stream itself opened
	exists  func(string) bool
}

// A path found missing is looked up again after this: Claude names a file
// before it writes it.
const missingFor = 2 * time.Second

type found struct {
	ok      bool
	at      time.Time
	running bool // a lookup still waits for the disk: no second one meanwhile
}

func newLinker() *linker {
	seen := map[string]found{}
	var mu sync.Mutex
	return &linker{exists: func(path string) bool {
		mu.Lock()
		f, known := seen[path]
		if known && (f.ok || f.running || time.Since(f.at) < missingFor) {
			mu.Unlock()
			return f.ok
		}
		if len(seen) > 4096 {
			clear(seen)
		}
		seen[path] = found{running: true}
		mu.Unlock()
		done := make(chan bool, 1)
		go func() {
			_, err := os.Stat(path)
			done <- err == nil
			mu.Lock()
			seen[path] = found{ok: err == nil, at: time.Now()}
			mu.Unlock()
		}()
		select {
		case ok := <-done:
			return ok
		case <-time.After(statWait):
			return false
		}
	}}
}

// write returns what of data (after what was held back) can go out now.
func (l *linker) write(data []byte) []byte {
	buf := append(l.pending, data...)
	l.pending = nil
	var out bytes.Buffer
	for i := 0; i < len(buf); {
		b := buf[i]
		if b == 0x1b {
			end := escapeEnd(buf, i)
			if end < 0 { // the rest of the sequence comes in the next chunk
				l.hold(&out, buf[i:])
				break
			}
			if bytes.HasPrefix(buf[i:], []byte("\x1b]8;")) {
				// ESC ] 8 ; params ; URI ST: an empty URI ends the link.
				body := strings.TrimRight(string(buf[i+4:end]), "\x07\x1b\\")
				_, uri, _ := strings.Cut(body, ";")
				l.inLink = uri != ""
			}
			out.Write(buf[i:end])
			i = end
			continue
		}
		if b < 0x20 || b == 0x7f {
			out.WriteByte(b)
			i++
			continue
		}
		// Text, with the colors set inside it: ConPTY sets them again where
		// it wraps a long line, in the middle of a path.
		j := i
		for j < len(buf) {
			if buf[j] == 0x1b {
				end := escapeEnd(buf, j)
				if end < 0 || buf[j+1] != '[' || buf[end-1] != 'm' {
					break
				}
				j = end
			} else if buf[j] < 0x20 || buf[j] == 0x7f {
				break
			} else {
				j++
			}
		}
		run := buf[i:j]
		if j == len(buf) || buf[j] == 0x1b && escapeEnd(buf, j) < 0 {
			// A path may go on in the next chunk: hold its last word back.
			text, at := plain(run)
			k := bytes.LastIndexByte(text, ' ') + 1
			if pathStart(text[k:]) || driveStart(text) {
				out.Write(l.link(run[:at[k]]))
				l.hold(&out, buf[i+at[k]:])
				break
			}
		}
		out.Write(l.link(run))
		i = j
	}
	return out.Bytes()
}

// plain returns run's text without its color sequences, and where each byte
// of it is in run (one more for the end).
func plain(run []byte) (text []byte, at []int) {
	for i := 0; i < len(run); {
		if run[i] == 0x1b {
			if i = escapeEnd(run, i); i < 0 { // cut off: what flush returns as it ends
				break
			}
			continue
		}
		text = append(text, run[i])
		at = append(at, i)
		i++
	}
	return text, append(at, len(run))
}

// flush returns what was held back: no more of it is coming.
func (l *linker) flush() []byte {
	p := l.pending
	l.pending = nil
	if len(p) > 0 && p[0] != 0x1b {
		return l.link(p)
	}
	return p
}

func (l *linker) hold(out *bytes.Buffer, tail []byte) {
	if len(tail) > maxPending {
		out.Write(tail)
		return
	}
	l.pending = append([]byte(nil), tail...)
}

func pathStart(word []byte) bool {
	if runtime.GOOS == "windows" {
		return bytes.Contains(word, []byte(`:\`)) || bytes.Contains(word, []byte(":/"))
	}
	return bytes.IndexByte(word, '/') >= 0
}

// driveStart reports whether text ends in what may begin a Windows path: "C" or "C:".
func driveStart(text []byte) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	text = bytes.TrimSuffix(text, []byte(":"))
	n := len(text)
	if n == 0 || !isLetter(text[n-1]) {
		return false
	}
	return n == 1 || !isWord(text[n-2])
}

func isLetter(b byte) bool { return b|0x20 >= 'a' && b|0x20 <= 'z' }

// isWord reports whether b may come right before a path that is part of something else.
func isWord(b byte) bool {
	return isLetter(b) || b >= '0' && b <= '9' || strings.IndexByte(`_.~-/\:`, b) >= 0
}

// link wraps the paths of existing files in run (text and colors) in hyperlinks.
func (l *linker) link(run []byte) []byte {
	if l.inLink {
		return run
	}
	text, at := plain(run)
	if !utf8.Valid(text) {
		return run
	}
	var out bytes.Buffer
	last := 0
	for _, p := range l.paths(string(text)) {
		path := string(text[p[0]:p[1]])
		// The link runs from the path's first byte to just after its last.
		from, to := at[p[0]], at[p[1]-1]+1
		out.Write(run[last:from])
		out.WriteString("\x1b]8;;" + fileURL(path) + "\x1b\\")
		out.Write(run[from:to])
		out.WriteString("\x1b]8;;\x1b\\")
		last = to
	}
	if last == 0 {
		return run
	}
	out.Write(run[last:])
	return out.Bytes()
}

// paths returns where the paths of existing files are in s, as [start, end) byte offsets.
func (l *linker) paths(s string) [][2]int {
	var out [][2]int
	for _, m := range pathRE.FindAllStringIndex(s, -1) {
		start := m[0]
		// Inside a word or a URL ("https://x/a"): not a path of its own.
		if start > 0 && isWord(s[start-1]) {
			continue
		}
		if (runtime.GOOS == "windows") != (s[start+1] == ':') {
			continue
		}
		if path := l.existing(s[start:m[1]]); path != "" {
			out = append(out, [2]int{start, start + len(path)})
		}
	}
	return out
}

// existing returns the longest start of candidate that names a file here:
// without sentence punctuation, or before Chinese text running into it.
func (l *linker) existing(candidate string) string {
	if len(candidate) > 1024 {
		return ""
	}
	tries := 0
	for end := len(candidate); end > 0 && tries < 4; {
		path := strings.TrimRight(candidate[:end], trailing)
		if len(path) > 3 && l.exists(path) {
			return path
		}
		tries++
		// Next: up to where non-ASCII text follows ASCII ("a.png里").
		next := -1
		for i := end - 1; i > 0; i-- {
			if candidate[i] >= utf8.RuneSelf && candidate[i-1] < utf8.RuneSelf {
				next = i
				break
			}
		}
		end = next
	}
	return ""
}

func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// escapeEnd returns the end of the escape sequence at buf[i], or -1 when buf
// ends before it does.
func escapeEnd(buf []byte, i int) int {
	if i+1 >= len(buf) {
		return -1
	}
	switch buf[i+1] {
	case '[': // CSI: parameters and intermediates, then a final byte
		j := i + 2
		for j < len(buf) && buf[j] >= 0x20 && buf[j] <= 0x3f {
			j++
		}
		if j == len(buf) {
			return -1
		}
		return j + 1
	case ']', 'P', 'X', '^', '_': // strings, ended by BEL (OSC) or ESC \
		for j := i + 2; j < len(buf); j++ {
			if buf[j] == 0x07 {
				return j + 1
			}
			if buf[j] == 0x1b {
				if j+1 == len(buf) {
					return -1
				}
				if buf[j+1] == '\\' {
					return j + 2
				}
			}
		}
		return -1
	}
	j := i + 1
	for j < len(buf) && buf[j] >= 0x20 && buf[j] <= 0x2f {
		j++
	}
	if j == len(buf) {
		return -1
	}
	return j + 1
}

// linkWriter writes the session's output through a linker; what it holds
// back goes out on its own after a moment when no more output comes.
type linkWriter struct {
	mu     sync.Mutex
	w      io.Writer
	l      *linker
	timer  *time.Timer
	closed bool // the screen is handed back: output passes as it is
}

func newLinkWriter(w io.Writer) *linkWriter {
	lw := &linkWriter{w: w, l: newLinker()}
	lw.timer = time.AfterFunc(time.Hour, func() {
		lw.mu.Lock()
		defer lw.mu.Unlock()
		if p := lw.l.flush(); len(p) > 0 {
			_, _ = lw.w.Write(p)
		}
	})
	lw.timer.Stop()
	return lw
}

func (lw *linkWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if lw.closed {
		return lw.w.Write(p)
	}
	if _, err := lw.w.Write(lw.l.write(p)); err != nil {
		return 0, err
	}
	if len(lw.l.pending) > 0 {
		lw.timer.Reset(30 * time.Millisecond)
	}
	return len(p), nil
}

// close writes what is held back, before the screen is handed back.
func (lw *linkWriter) close() {
	lw.timer.Stop()
	lw.mu.Lock()
	defer lw.mu.Unlock()
	lw.closed = true
	_, _ = lw.w.Write(lw.l.flush())
}

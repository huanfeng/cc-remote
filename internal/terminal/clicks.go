package terminal

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"unicode/utf8"
)

// Claude Code turns on mouse reporting for its full-screen view, so the
// terminal hands a click to Claude Code (which expands a tool call with it)
// instead of following the hyperlink under it. ccr keeps a copy of the
// screen it shows; Ctrl+click on the path of a file on this machine (as
// Windows Terminal follows links) opens the file here, and Claude Code does
// not see that click. A plain click stays Claude Code's.

// An SGR mouse report: ESC [ < button ; column ; row, then M (press) or m (release).
var mouseRE = regexp.MustCompile(`\x1b\[<(\d+);(\d+);(\d+)([Mm])`)

// A relative path, as Claude Code shows a tool's file ("Update(web/src/App.tsx)"):
// path characters, with a separator or a file extension (relFile).
var (
	relRE   = regexp.MustCompile(`[^\s"'` + "`" + `<>|*?:;,()\[\]{}，。、：；！？（）【】《》「」“”‘’]+`)
	relFile = regexp.MustCompile(`[\\/]|\.[A-Za-z][A-Za-z0-9]{0,9}$`)
)

type clicker struct {
	mu      sync.Mutex
	screen  *screen
	l       *linker
	cwd     string // the session's directory, which relative paths are in
	open    func(path string)
	release bool // the press was taken: so is its release
}

// Button codes in a mouse report: the left button, and Ctrl held (Shift is 4, Alt 8).
const (
	mouseLeft = 0
	mouseCtrl = 16
)

func newClicker(cols, rows int, cwd string, open func(string)) *clicker {
	return &clicker{screen: newScreen(cols, rows), l: newLinker(), cwd: cwd, open: open}
}

// observe follows what the screen shows.
func (c *clicker) observe(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.screen.write(data)
}

func (c *clicker) resize(cols, rows int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.screen.resize(cols, rows)
}

// filter returns input without the clicks it handled: a left press with Ctrl
// (and nothing else) on a file's path, and that press's release.
func (c *clicker) filter(input []byte) []byte {
	if !bytes.Contains(input, []byte("\x1b[<")) {
		return input
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return mouseRE.ReplaceAllFunc(input, func(report []byte) []byte {
		m := mouseRE.FindSubmatch(report)
		button, _ := strconv.Atoi(string(m[1]))
		x, _ := strconv.Atoi(string(m[2]))
		y, _ := strconv.Atoi(string(m[3]))
		if m[4][0] == 'm' {
			// Ctrl may be let go first: any left release ends the press taken.
			if c.release && button&^(4|8|mouseCtrl) == mouseLeft {
				c.release = false
				return nil
			}
			return report
		}
		if button != mouseLeft|mouseCtrl { // a plain click, another button or modifier, motion or the wheel
			return report
		}
		if path := c.pathAt(x-1, y-1); path != "" {
			c.release = true
			go c.open(path)
			return nil
		}
		return report
	})
}

// pathAt returns the path of the existing file shown at cell (x, y), or "".
// Claude Code breaks a long path at the screen's edge and goes on in the next
// row, indented: rows that run to the edge are read as one line first.
func (c *clicker) pathAt(x, y int) string {
	if y < 0 || y >= c.screen.rows {
		return ""
	}
	first, last := y, y
	for first > 0 && y-first < 3 && c.wraps(first-1) {
		first--
	}
	for last < c.screen.rows-1 && last-y < 3 && c.wraps(last) {
		last++
	}
	if path := c.pathIn(first, last, x, y); path != "" || first == last {
		return path
	}
	return c.pathIn(y, y, x, y)
}

// wraps reports whether row y has text up to the screen's edge.
func (c *clicker) wraps(y int) bool {
	row := c.screen.cells[y]
	for i := len(row) - 1; i >= 0 && i >= len(row)-3; i-- {
		if row[i].r != ' ' {
			return !isRule(row[i].r)
		}
	}
	return false
}

// isRule: Claude Code's horizontal lines run to the edge too.
func isRule(r rune) bool { return r >= 0x2500 && r <= 0x257f }

// pathIn looks for the path at (x, y) in rows first to last read as one line
// (the indent of each following row left out).
func (c *clicker) pathIn(first, last, x, y int) string {
	var text []byte
	var at []int // the cell each byte of text is in, as row*cols+column
	for row := first; row <= last; row++ {
		cells := c.screen.cells[row]
		start := 0
		if row > first {
			for start < len(cells) && cells[start].r == ' ' {
				start++
			}
		}
		for col := start; col < len(cells); col++ {
			if cells[col].r == 0 { // the right half of a wide character
				continue
			}
			n := len(text)
			text = append(text, string(cells[col].r)...)
			for range len(text) - n {
				at = append(at, row*c.screen.cols+col)
			}
		}
		if row < last { // up to its last character
			for len(text) > 0 && text[len(text)-1] == ' ' {
				text, at = text[:len(text)-1], at[:len(at)-1]
			}
		}
	}
	s, click := string(text), y*c.screen.cols+x
	// Whether the click is on s[from:to].
	on := func(from, to int) bool {
		end, _ := utf8.DecodeLastRuneInString(s[:to])
		return at[from] <= click && click <= at[to-1]+runeWidth(end)-1
	}
	for _, p := range c.l.paths(s) {
		if on(p[0], p[1]) {
			return s[p[0]:p[1]]
		}
	}
	if c.cwd == "" {
		return ""
	}
	for _, m := range relRE.FindAllStringIndex(s, -1) {
		word := s[m[0]:m[1]]
		if !on(m[0], m[1]) || !relFile.MatchString(word) || filepath.IsAbs(word) {
			continue
		}
		// Cut like an absolute path, but never back to the directory itself.
		if path := c.l.existing(filepath.Join(c.cwd, word)); len(path) > len(filepath.Clean(c.cwd))+1 {
			return path
		}
	}
	return ""
}

package terminal

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// screen keeps the text on the pseudo-terminal's screen. ConPTY (and a TUI)
// send only what changed, e.g. one digit of a timer, so reading the current
// state — such as Claude Code's "✽ Composing… (8m 33s · ↓ 26.2k tokens)"
// line — needs the screen itself. It follows the cursor and erase sequences
// these programs use and ignores colors and modes.
type screen struct {
	cols, rows  int
	cells       [][]rune // 0 marks the right half of a wide character
	x, y        int
	pendingWrap bool
	state       int
	seq         []byte // CSI parameters, or a partial UTF-8 character
	savedX      int
	savedY      int
}

const (
	stGround = iota
	stEsc
	stCSI
	stString // OSC, DCS, ...: skipped until BEL or ST
	stStringEsc
	stCharset // ESC ( B and friends: one more byte
)

func newScreen(cols, rows int) *screen {
	s := &screen{}
	s.resize(cols, rows)
	return s
}

func (s *screen) resize(cols, rows int) {
	cells := make([][]rune, rows)
	for y := range cells {
		cells[y] = blankLine(cols)
		if y < len(s.cells) {
			copy(cells[y], s.cells[y])
		}
	}
	s.cols, s.rows, s.cells = cols, rows, cells
	s.x, s.y = min(s.x, cols-1), min(s.y, rows-1)
	s.pendingWrap = false
}

func blankLine(cols int) []rune {
	line := make([]rune, cols)
	for i := range line {
		line[i] = ' '
	}
	return line
}

func (s *screen) write(b []byte) {
	for len(b) > 0 {
		c := b[0]
		switch s.state {
		case stEsc:
			b = b[1:]
			switch c {
			case '[':
				s.state, s.seq = stCSI, s.seq[:0]
			case ']', 'P', 'X', '^', '_':
				s.state = stString
			case '(', ')', '*', '+', '#', '%':
				s.state = stCharset
			case '7':
				s.savedX, s.savedY, s.state = s.x, s.y, stGround
			case '8':
				s.x, s.y, s.state = s.savedX, s.savedY, stGround
			case 'M': // reverse index
				if s.y == 0 {
					s.scrollDown(1)
				} else {
					s.y--
				}
				s.state = stGround
			case 'c':
				s.resize(s.cols, s.rows)
				s.erase(0, 0, s.cols, s.rows)
				s.x, s.y, s.state = 0, 0, stGround
			default:
				s.state = stGround
			}
		case stCharset:
			b, s.state = b[1:], stGround
		case stCSI:
			b = b[1:]
			if c >= 0x40 && c <= 0x7e {
				s.csi(c)
				s.state, s.seq = stGround, s.seq[:0]
			} else if len(s.seq) < 64 {
				s.seq = append(s.seq, c)
			}
		case stString:
			b = b[1:]
			if c == 0x07 {
				s.state = stGround
			} else if c == 0x1b {
				s.state = stStringEsc
			}
		case stStringEsc:
			b = b[1:]
			s.state = stString
			if c == '\\' {
				s.state = stGround
			}
		default:
			if len(s.seq) > 0 { // the rest of a UTF-8 character split across reads
				s.seq = append(s.seq, c)
				b = b[1:]
				if utf8.FullRune(s.seq) {
					r, _ := utf8.DecodeRune(s.seq)
					s.seq = s.seq[:0]
					s.print(r)
				}
				continue
			}
			switch {
			case c == 0x1b:
				s.state, b = stEsc, b[1:]
			case c == '\r':
				s.x, s.pendingWrap, b = 0, false, b[1:]
			case c == '\n' || c == 0x0b || c == 0x0c:
				s.lineFeed()
				b = b[1:]
			case c == '\b':
				if s.x > 0 {
					s.x--
				}
				s.pendingWrap, b = false, b[1:]
			case c == '\t':
				s.x, b = min((s.x/8+1)*8, s.cols-1), b[1:]
			case c < 0x20 || c == 0x7f:
				b = b[1:]
			case c < 0x80:
				s.print(rune(c))
				b = b[1:]
			default:
				if !utf8.FullRune(b) {
					s.seq = append(s.seq[:0], b...)
					return
				}
				r, n := utf8.DecodeRune(b)
				s.print(r)
				b = b[n:]
			}
		}
	}
}

func (s *screen) print(r rune) {
	w := runeWidth(r)
	if w == 0 {
		return
	}
	if s.pendingWrap || s.x+w > s.cols {
		s.x = 0
		s.lineFeed()
	}
	s.cells[s.y][s.x] = r
	if w == 2 && s.x+1 < s.cols {
		s.cells[s.y][s.x+1] = 0
	}
	s.x += w
	if s.x >= s.cols {
		s.x, s.pendingWrap = s.cols-1, true
	}
}

func (s *screen) lineFeed() {
	s.pendingWrap = false
	if s.y == s.rows-1 {
		s.scrollUp(1)
		return
	}
	s.y++
}

func (s *screen) scrollUp(n int) {
	n = min(n, s.rows)
	copy(s.cells, s.cells[n:])
	for y := s.rows - n; y < s.rows; y++ {
		s.cells[y] = blankLine(s.cols)
	}
}

func (s *screen) scrollDown(n int) {
	n = min(n, s.rows)
	copy(s.cells[n:], s.cells[:s.rows-n])
	for y := 0; y < n; y++ {
		s.cells[y] = blankLine(s.cols)
	}
}

// erase blanks the cells from (x0, y0) up to, not including, (x1, y1) in reading order.
func (s *screen) erase(x0, y0, x1, y1 int) {
	for y := y0; y <= y1 && y < s.rows; y++ {
		from, to := 0, s.cols
		if y == y0 {
			from = x0
		}
		if y == y1 {
			to = x1
		}
		for x := max(from, 0); x < min(to, s.cols); x++ {
			s.cells[y][x] = ' '
		}
	}
}

func (s *screen) csi(final byte) {
	if len(s.seq) > 0 && (s.seq[0] == '?' || s.seq[0] == '>' || s.seq[0] == '<' || s.seq[0] == '=') {
		return // private modes, keyboard protocols
	}
	var params []int
	for _, p := range strings.Split(string(s.seq), ";") {
		n, _ := strconv.Atoi(strings.TrimRight(p, " !\"#$%&'()*+,-./"))
		params = append(params, n)
	}
	arg := func(i, def int) int {
		if i < len(params) && params[i] > 0 {
			return params[i]
		}
		return def
	}
	s.pendingWrap = false
	switch final {
	case 'H', 'f':
		s.y, s.x = clamp(arg(0, 1)-1, s.rows), clamp(arg(1, 1)-1, s.cols)
	case 'A':
		s.y = clamp(s.y-arg(0, 1), s.rows)
	case 'B', 'e':
		s.y = clamp(s.y+arg(0, 1), s.rows)
	case 'C', 'a':
		s.x = clamp(s.x+arg(0, 1), s.cols)
	case 'D':
		s.x = clamp(s.x-arg(0, 1), s.cols)
	case 'E':
		s.x, s.y = 0, clamp(s.y+arg(0, 1), s.rows)
	case 'F':
		s.x, s.y = 0, clamp(s.y-arg(0, 1), s.rows)
	case 'G', '`':
		s.x = clamp(arg(0, 1)-1, s.cols)
	case 'd':
		s.y = clamp(arg(0, 1)-1, s.rows)
	case 'K':
		switch arg(0, 0) {
		case 0:
			s.erase(s.x, s.y, s.cols, s.y)
		case 1:
			s.erase(0, s.y, s.x+1, s.y)
		default:
			s.erase(0, s.y, s.cols, s.y)
		}
	case 'J':
		switch arg(0, 0) {
		case 0:
			s.erase(s.x, s.y, s.cols, s.rows-1)
		case 1:
			s.erase(0, 0, s.x+1, s.y)
		default:
			s.erase(0, 0, s.cols, s.rows-1)
		}
	case 'X':
		s.erase(s.x, s.y, s.x+arg(0, 1), s.y)
	case 'S':
		s.scrollUp(arg(0, 1))
	case 'T':
		s.scrollDown(arg(0, 1))
	case 'L', 'M': // insert / delete lines at the cursor
		n := min(arg(0, 1), s.rows-s.y)
		region := s.cells[s.y:]
		if final == 'L' {
			copy(region[n:], region[:len(region)-n])
			for i := 0; i < n; i++ {
				region[i] = blankLine(s.cols)
			}
		} else {
			copy(region, region[n:])
			for i := len(region) - n; i < len(region); i++ {
				region[i] = blankLine(s.cols)
			}
		}
	case '@', 'P': // insert / delete characters
		n := min(arg(0, 1), s.cols-s.x)
		line := s.cells[s.y][s.x:]
		if final == '@' {
			copy(line[n:], line[:len(line)-n])
			for i := 0; i < n; i++ {
				line[i] = ' '
			}
		} else {
			copy(line, line[n:])
			for i := len(line) - n; i < len(line); i++ {
				line[i] = ' '
			}
		}
	case 's':
		s.savedX, s.savedY = s.x, s.y
	case 'u':
		s.x, s.y = s.savedX, s.savedY
	}
}

func clamp(v, n int) int { return max(0, min(v, n-1)) }

// lines returns the screen text, one string per row.
func (s *screen) lines() []string {
	out := make([]string, s.rows)
	for y, row := range s.cells {
		var b strings.Builder
		for _, r := range row {
			if r != 0 {
				b.WriteRune(r)
			}
		}
		out[y] = strings.TrimRight(b.String(), " ")
	}
	return out
}

// runeWidth is the number of cells a character takes, as ConPTY and common
// terminals count it: East Asian wide characters and most emoji take two,
// combining marks none.
func runeWidth(r rune) int {
	switch {
	case r == 0 || (r >= 0x300 && r <= 0x36f) || (r >= 0x200b && r <= 0x200f) || (r >= 0xfe00 && r <= 0xfe0f) || r == 0x20e3:
		return 0
	case (r >= 0x1100 && r <= 0x115f) || (r >= 0x2e80 && r <= 0x303e) || (r >= 0x3041 && r <= 0x33ff) ||
		(r >= 0x3400 && r <= 0x4dbf) || (r >= 0x4e00 && r <= 0x9fff) || (r >= 0xa000 && r <= 0xa4cf) ||
		(r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe30 && r <= 0xfe4f) ||
		(r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) || (r >= 0x1f300 && r <= 0x1f64f) ||
		(r >= 0x1f900 && r <= 0x1f9ff) || (r >= 0x20000 && r <= 0x3fffd):
		return 2
	}
	return 1
}

// activityLine matches Claude Code's spinner line while it works, such as
// "✽ Composing… (8m 33s · ↓ 26.2k tokens · esc to interrupt)".
var activityLine = regexp.MustCompile(`^\s*\S\s+(\p{Lu}[\p{L}'-]*(?: [\p{L}'-]+)*…)\s*(?:\((.*)\))?\s*$`)

// activity is the spinner line on the screen, without the key hint, or "".
func (s *screen) activity() string {
	lines := s.lines()
	for y := len(lines) - 1; y >= 0; y-- {
		line := lines[y]
		if strings.Contains(line, "(") && !strings.Contains(line, ")") && y+1 < len(lines) {
			line += lines[y+1] // wrapped on a narrow terminal
		}
		m := activityLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var parts []string
		for _, p := range strings.Split(m[2], "·") {
			if p = strings.TrimSpace(p); p != "" && !strings.Contains(p, "esc to interrupt") && !strings.Contains(p, "ctrl+") {
				parts = append(parts, p)
			}
		}
		if len(parts) == 0 {
			return m[1]
		}
		return m[1] + " (" + strings.Join(parts, " · ") + ")"
	}
	return ""
}

package terminal

import (
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Claude Code's TUI (and ConPTY) turn terminal modes on once, at start:
// bracketed paste, focus reports, mouse reporting, synchronized output,
// win32-input-mode, the kitty keyboard protocol, modifyOtherKeys. A front end
// that attaches later never saw those sequences, so its terminal would encode
// keys differently (Shift+Enter, pastes). modeTracker records the modes in
// force so each new front end can be put into the same state first.
type modeTracker struct {
	decset map[int]bool
	kitty  []int // kitty keyboard flags stack (CSI > n u pushes, CSI < n u pops)
	mok    int   // modifyOtherKeys level (CSI > 4 ; n m)
}

// trackedModes are the private modes replayed to a new front end.
var trackedModes = map[int]bool{1: true, 1000: true, 1002: true, 1003: true, 1004: true, 1006: true, 2004: true, 2031: true, 9001: true}

var (
	decsetSeq = regexp.MustCompile(`\x1b\[\?([0-9;]+)([hl])`)
	kittySeq  = regexp.MustCompile(`\x1b\[([<>])([0-9]*)u`)
	mokSeq    = regexp.MustCompile(`\x1b\[>4(?:;([0-9]+))?m`)
)

func newModeTracker() *modeTracker { return &modeTracker{decset: map[int]bool{}} }

func (m *modeTracker) observe(chunk []byte) {
	if !bytes.Contains(chunk, []byte("\x1b[")) {
		return
	}
	for _, g := range decsetSeq.FindAllSubmatch(chunk, -1) {
		on := string(g[2]) == "h"
		for _, part := range strings.Split(string(g[1]), ";") {
			if n, err := strconv.Atoi(part); err == nil && trackedModes[n] {
				m.decset[n] = on
			}
		}
	}
	for _, g := range kittySeq.FindAllSubmatch(chunk, -1) {
		n, _ := strconv.Atoi(string(g[2]))
		if string(g[1]) == ">" {
			m.kitty = append(m.kitty, n)
			continue
		}
		if n < 1 {
			n = 1
		}
		m.kitty = m.kitty[:max(0, len(m.kitty)-n)]
	}
	for _, g := range mokSeq.FindAllSubmatch(chunk, -1) {
		m.mok, _ = strconv.Atoi(string(g[1]))
	}
}

// replay returns the sequences that put a fresh terminal into the current modes.
func (m *modeTracker) replay() []byte {
	var b strings.Builder
	modes := make([]int, 0, len(m.decset))
	for n, on := range m.decset {
		if on {
			modes = append(modes, n)
		}
	}
	sort.Ints(modes)
	for _, n := range modes {
		b.WriteString("\x1b[?" + strconv.Itoa(n) + "h")
	}
	for _, flags := range m.kitty {
		b.WriteString("\x1b[>" + strconv.Itoa(flags) + "u")
	}
	if m.mok > 0 {
		b.WriteString("\x1b[>4;" + strconv.Itoa(m.mok) + "m")
	}
	return []byte(b.String())
}

// detachSeq matches Ctrl+] as a terminal may encode it: the plain byte, the
// win32-input-mode key-down record (character code 29), the kitty keyboard
// protocol, and modifyOtherKeys.
var detachSeq = regexp.MustCompile(`\x1d|\x1b\[[0-9]+;[0-9]+;29;1;[0-9]+;[0-9]+_|\x1b\[93;5(?::[0-9]+)?u|\x1b\[27;5;93~`)

// redrawSeq matches Ctrl+L in the same encodings.
var redrawSeq = regexp.MustCompile(`\x0c|\x1b\[[0-9]+;[0-9]+;12;1;[0-9]+;[0-9]+_|\x1b\[108;5(?::[0-9]+)?u|\x1b\[27;5;108~`)

// HasRedraw reports whether keyboard input contains Ctrl+L, which also makes
// the host repaint the whole screen.
func HasRedraw(b []byte) bool { return redrawSeq.Match(b) }

// DetachIndex returns where Ctrl+] starts in keyboard input, or -1.
func DetachIndex(b []byte) int {
	loc := detachSeq.FindIndex(b)
	if loc == nil {
		return -1
	}
	return loc[0]
}

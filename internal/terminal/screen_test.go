package terminal

import (
	"strings"
	"testing"
)

func TestScreenFollowsConPTYUpdates(t *testing.T) {
	s := newScreen(40, 6)
	// A full paint, then ConPTY's partial updates: one digit, skipped cells.
	s.write([]byte("\x1b[H\x1b[2J\x1b[38;2;1;2;3m● 回答\x1b[m\r\n\r\n\x1b[38;2;215;119;87m✽\x1b[1CComposing… \x1b[38;2;153;153;153m(8m 33s · ↓ 26.2k tokens · esc to interrupt)\x1b[K"))
	if got := s.activity(); got != "Composing… (8m 33s · ↓ 26.2k tokens)" {
		t.Fatalf("activity = %q\n%s", got, strings.Join(s.lines(), "\n"))
	}
	s.write([]byte("\x1b[3;19H4")) // the timer ticks: only the changed cell is sent
	if got := s.activity(); got != "Composing… (8m 34s · ↓ 26.2k tokens)" {
		t.Fatalf("after tick = %q\n%s", got, strings.Join(s.lines(), "\n"))
	}
	if got := s.lines()[0]; got != "● 回答" {
		t.Fatalf("wide characters: %q", got)
	}
	// The turn ends: the spinner becomes a past-tense summary.
	s.write([]byte("\x1b[3;1H\x1b[K✻ Brewed for 8m 40s"))
	if got := s.activity(); got != "" {
		t.Fatalf("activity after the turn = %q", got)
	}
	// UTF-8 split across reads, scrolling at the bottom.
	b := []byte("\x1b[6;1H✽ Thinking…")
	s.write(b[:9])
	s.write(b[9:])
	s.write([]byte("\r\n"))
	if got := s.lines()[4]; got != "✽ Thinking…" {
		t.Fatalf("split UTF-8 / scroll: %q", s.lines())
	}
	if got := s.activity(); got != "Thinking…" {
		t.Fatalf("activity without details = %q", got)
	}
}

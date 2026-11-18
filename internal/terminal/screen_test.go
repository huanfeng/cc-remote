package terminal

import (
	"fmt"
	"strings"
	"testing"
)

func TestScreenRetryActivity(t *testing.T) {
	s := newScreen(100, 8)
	s.write([]byte("\x1b[3;1H✻ Connection dropped (ECONNRESET) · retrying… · attempt 6/10"))
	if got := s.activity(); got != "Connection dropped (ECONNRESET) · retrying… · attempt 6/10" {
		t.Fatalf("retry activity missing: %q", got)
	}
}

func TestScreenRetryDetailsAndRecovery(t *testing.T) {
	const message = "Connection dropped (ECONNRESET) · retrying… · attempt 6/10"
	for _, width := range []int{20, 32, 100} {
		s := newScreen(width, 12)
		s.write([]byte("\x1b[3;1H\x1b[33m✻ " + message + "\x1b[m"))
		activity, retry := s.activityState()
		if retry == nil || retry.Message != message || retry.Attempt != 6 || retry.MaxAttempts != 10 || activity != message {
			t.Fatalf("width %d: activity=%q retry=%+v screen=%q", width, activity, retry, s.lines())
		}
		s.write([]byte("\x1b[H\x1b[2J✽ Thinking… (2s · esc to interrupt)"))
		activity, retry = s.activityState()
		if retry != nil || activity != "Thinking… (2s)" {
			t.Fatalf("recovery retained retry: %q %+v", activity, retry)
		}
		s.write([]byte("\x1b[H\x1b[2J✻ Brewed for 5s"))
		if activity, retry = s.activityState(); retry != nil || activity != "" {
			t.Fatalf("completion retained retry: %q %+v", activity, retry)
		}
	}
}

func TestScreenRetryPartialCounterUpdate(t *testing.T) {
	s := newScreen(100, 8)
	const prefix = "✻ Connection dropped (ECONNRESET) · retrying… · attempt "
	s.write([]byte(prefix + "6/10"))
	s.write([]byte(fmt.Sprintf("\x1b[1;%dH7", len([]rune(prefix))+1)))
	_, retry := s.activityState()
	if retry == nil || retry.Attempt != 7 || retry.MaxAttempts != 10 {
		t.Fatalf("partial update: %+v", retry)
	}
}

func TestScreenRetryDoesNotSwallowAdjacentLines(t *testing.T) {
	s := newScreen(100, 8)
	const message = "Connection dropped (ECONNRESET) · retrying… · attempt 6/10"
	s.write([]byte("✻ " + message + "\r\n? for shortcuts"))
	if _, retry := s.activityState(); retry == nil || retry.Message != message {
		t.Fatalf("adjacent footer included: %+v", retry)
	}
	s.write([]byte("\x1b[H\x1b[2J✽ Thinking…\r\n? for shortcuts"))
	if activity, retry := s.activityState(); activity != "Thinking…" || retry != nil {
		t.Fatalf("normal activity changed by footer: %q %+v", activity, retry)
	}
}

func TestScreenRetryFormatsAndFalsePositives(t *testing.T) {
	for _, text := range []string{
		"✻ API Error: 529 overloaded · retrying in 5s… · attempt 2/10",
		"✽ Connection dropped (ETIMEDOUT) · retrying... · attempt 10/10",
		"✻ Retrying…",
	} {
		s := newScreen(120, 8)
		s.write([]byte(text))
		if _, retry := s.activityState(); retry == nil {
			t.Fatalf("missing retry: %s", text)
		}
	}
	for _, text := range []string{
		"Connection dropped (ECONNRESET) · retrying… · attempt 6/10",
		"● The log contains retrying… · attempt 6/10",
		"❯ explain retrying… · attempt 6/10",
		"✻ Retrying the failed tests…",
	} {
		s := newScreen(120, 8)
		s.write([]byte(text))
		if _, retry := s.activityState(); retry != nil {
			t.Fatalf("ordinary content treated as retry: %s", text)
		}
	}
	s := newScreen(100, 8)
	s.write([]byte("✻ Connection dropped · retrying… · attempt 6/10\r\n✽ Composing…"))
	if activity, retry := s.activityState(); retry != nil || activity != "Composing…" {
		t.Fatalf("newer activity must supersede old retry: %q %+v", activity, retry)
	}
}

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

// Claude Code writes its suggested next prompt faintly after the prompt.
func TestScreenSuggestion(t *testing.T) {
	s := newScreen(60, 8)
	// A past prompt (gray, not faint), then the input box as Claude Code draws it.
	s.write([]byte("\x1b[38;2;153;153;153m\x1b[48;2;55;55;55m\x1b[2;1H❯ \x1b[38;2;255;255;255mlook at calc.py\x1b[m"))
	s.write([]byte("\x1b[6;1H❯ "))
	if got := s.suggestion(); got != "" {
		t.Fatalf("empty box: %q", got)
	}
	s.write([]byte("\x1b[2mfix the bug\x1b[6;3H\x1b[22m"))
	if got := s.suggestion(); got != "fix the bug" {
		t.Fatalf("suggestion = %q\n%s", got, strings.Join(s.lines(), "\n"))
	}
	// Typing replaces it.
	s.write([]byte("\x1b[6;3Hrun the tests\x1b[K"))
	if got := s.suggestion(); got != "" {
		t.Fatalf("typed text taken for a suggestion: %q", got)
	}
	// The placeholder of an empty box is not a suggestion; a 24-bit gray is not faint.
	s.write([]byte("\x1b[6;3H\x1b[K\x1b[2mTry \"how does <filepath> work?\"\x1b[22m"))
	if got := s.suggestion(); got != "" {
		t.Fatalf("placeholder: %q", got)
	}
	s.write([]byte("\x1b[6;3H\x1b[K\x1b[38;2;2;2;2mtyped\x1b[m"))
	if got := s.suggestion(); got != "" {
		t.Fatalf("38;2;2;2;2 parsed as faint: %q", got)
	}
}

// The input box is a "❯" row between two rules; a dialog option or a past
// prompt is not, and faint text in the box is not typed.
func TestScreenInput(t *testing.T) {
	rule := strings.Repeat("─", 30)
	s := newScreen(40, 10)
	// While Claude Code starts the terminal echoes keys: no box yet.
	s.write([]byte("hello"))
	if _, ok := s.input(); ok {
		t.Fatalf("echo taken for the box:\n%s", strings.Join(s.lines(), "\n"))
	}
	s.write([]byte("\x1b[H\x1b[2J\x1b[2;1H❯ \x1b[38;2;255;255;255mlook at calc.py\x1b[m")) // a past prompt
	s.write([]byte("\x1b[5;1H" + rule + "\x1b[6;1H❯ \x1b[2mTry \"fix lint errors\"\x1b[22m\x1b[7;1H" + rule))
	if text, ok := s.input(); !ok || text != "" {
		t.Fatalf("empty box = %q %v\n%s", text, ok, strings.Join(s.lines(), "\n"))
	}
	// Typed text wraps to the next row; the bottom rule moves down.
	s.write([]byte("\x1b[6;3H\x1b[Krun the\x1b[7;1H\x1b[K  tests\x1b[8;1H" + rule))
	if text, ok := s.input(); !ok || flat(text) != "runthetests" {
		t.Fatalf("typed = %q %v\n%s", text, ok, strings.Join(s.lines(), "\n"))
	}
	// A dialog replaces the box: its selected option has no rule above.
	s.write([]byte("\x1b[H\x1b[2J\x1b[2;1H" + rule + "\x1b[3;1H Switch model?\x1b[5;1H❯ 1. Yes\x1b[6;1H  2. No"))
	if text, ok := s.input(); ok {
		t.Fatalf("dialog taken for the box: %q", text)
	}
}

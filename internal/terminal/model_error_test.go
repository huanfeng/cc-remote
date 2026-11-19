package terminal

import (
	"strings"
	"testing"
)

const selectedModelError = "There's an issue with the selected model (glm-5.3-flash[1m]). It may not exist or you may not have access to it. Run /model to pick a different model."

func TestScreenLoginExpiredError(t *testing.T) {
	for _, message := range []string{"Login expired · Please run /login", "Login expired . Please run /login", "Login expired. Please run /login"} {
		for _, prefix := range []string{"", "● "} {
			for _, cols := range []int{16, 24, 80} {
				s := newScreen(cols, 12)
				s.write([]byte("\x1b[31m" + prefix + message + "\x1b[m\r\n\r\n" + strings.Repeat("─", cols) + "\r\n❯ \r\n" + strings.Repeat("─", cols)))
				if got, revision := s.modelErrorState(); got != message || revision == 0 {
					t.Fatalf("width %d %q => %q revision %d", cols, prefix+message, got, revision)
				}
			}
		}
	}
}

func TestScreenLoginExpiredNotQuotedOrStale(t *testing.T) {
	message := "Login expired · Please run /login"
	for _, output := range []string{
		"❯ " + message,
		"● > " + message,
		"● An example: " + message,
		"Login expired",
		"Please run /login",
		message + "\r\n✽ Thinking…",
		message + "\r\n✻ API Error: retrying… · attempt 1/10",
		message + "\r\n● New answer",
		message + "\r\n❯ New prompt",
	} {
		s := newScreen(100, 12)
		s.write([]byte(output))
		if got := s.modelError(); got != "" {
			t.Fatalf("ordinary/stale content became failure: %q => %q", output, got)
		}
	}
}

func TestScreenLoginExpiredPaintRevision(t *testing.T) {
	s := newScreen(100, 10)
	message := "Login expired · Please run /login"
	s.write([]byte(message))
	_, first := s.modelErrorState()
	s.write([]byte("\x1b[8;1H? for shortcuts"))
	if _, footer := s.modelErrorState(); footer != first {
		t.Fatalf("unrelated footer advanced login diagnostic: %d -> %d", first, footer)
	}
	s.write([]byte("\x1b[H\x1b[2J" + message))
	if got, repaint := s.modelErrorState(); got != message || repaint <= first {
		t.Fatalf("fresh identical login diagnostic ignored: %q revision %d <= %d", got, repaint, first)
	}
}

func TestScreenSelectedModelError(t *testing.T) {
	for _, cols := range []int{24, 40, 100, 200} {
		s := newScreen(cols, 20)
		s.write([]byte("\x1b[31m● " + selectedModelError + "\x1b[m\r\n\r\n" + strings.Repeat("─", min(20, cols)) + "\r\n❯ \r\n" + strings.Repeat("─", min(20, cols))))
		if got := s.modelError(); got != selectedModelError {
			t.Fatalf("width %d error = %q\n%s", cols, got, strings.Join(s.lines(), "\n"))
		}
	}
}

func TestScreenSelectedModelErrorNotStaleOrQuoted(t *testing.T) {
	for _, output := range []string{
		"❯ " + selectedModelError,
		"● > " + selectedModelError,
		"● An example error: " + selectedModelError,
		"● " + selectedModelError + "\r\n✽ Thinking…",
		"● " + selectedModelError + "\r\n✻ Connection dropped · retrying… · attempt 1/10",
		"● " + selectedModelError + "\r\n● New answer",
		"● " + selectedModelError + "\r\n❯ New prompt",
		"● There's an issue with the selected model (glm-5.3-flash[1m]).",
		"✻ API Error: 529 overloaded · retrying in 5s…",
	} {
		s := newScreen(200, 20)
		s.write([]byte(output))
		if got := s.modelError(); got != "" {
			t.Fatalf("ordinary/stale content became failure: %q => %q", output, got)
		}
	}
}

func TestRunnerModelErrorSnapshot(t *testing.T) {
	r := &Runner{scr: newScreen(200, 8)}
	r.scr.write([]byte("● " + selectedModelError))
	message, revision := r.ModelError()
	if message != selectedModelError || revision == 0 {
		t.Fatalf("snapshot = %q %v", message, revision)
	}
	r.scr.write([]byte("\x1b[H\x1b[2J✽ Thinking…"))
	if message, _ := r.ModelError(); message != "" {
		t.Fatalf("recovered terminal still has error: %q", message)
	}
}

func TestRunnerModelErrorPaintRevision(t *testing.T) {
	r := &Runner{scr: newScreen(200, 8)}
	r.scr.write([]byte("● " + selectedModelError))
	_, first := r.ModelError()
	// A footer update must not make an old error look like a new failure.
	r.scr.write([]byte("\x1b[7;1H? for shortcuts"))
	_, footer := r.ModelError()
	if footer != first {
		t.Fatalf("footer changed diagnostic revision: %v -> %v", first, footer)
	}
	// Both clear and repaint can happen between host polls. Identical text
	// must still be observable as a fresh diagnostic in the next prompt.
	r.scr.write([]byte("\x1b[H\x1b[2J● " + selectedModelError))
	message, repaint := r.ModelError()
	if message != selectedModelError || repaint <= first {
		t.Fatalf("identical repaint lost: %q revision %v <= %v", message, repaint, first)
	}
	r.scr.write([]byte("\x1b[3;1H✽ Thinking…"))
	if message, revision := r.ModelError(); message != "" || revision != 0 {
		t.Fatalf("recovery snapshot = %q %v", message, revision)
	}
}

func TestScreenModelErrorRevisionFollowsRows(t *testing.T) {
	s := newScreen(200, 10)
	s.write([]byte("\x1b[3;1H● " + selectedModelError))
	_, first := s.modelErrorState()
	for _, sequence := range []string{"\x1b[1S", "\x1b[H\x1b[1L", "\x1b[1M", "\x1b[1T"} {
		s.write([]byte(sequence))
		message, revision := s.modelErrorState()
		if message != selectedModelError || revision != first {
			t.Fatalf("row move %q changed diagnostic snapshot: %q %v != %v", sequence, message, revision, first)
		}
	}
	s.resize(200, 12)
	if _, revision := s.modelErrorState(); revision != first {
		t.Fatalf("resize changed revision: %v != %v", revision, first)
	}
}

func TestScreenSelectedModelErrorRendererWraps(t *testing.T) {
	s := newScreen(100, 10)
	s.write([]byte("● There's an issue with the selected model (glm-5.3-flash[1m]).\r\n  It may not exist or you may not have access to it.\r\n  Run /model to pick a different model."))
	if got := s.modelError(); got != selectedModelError {
		t.Fatalf("indented renderer wrap = %q", got)
	}
}

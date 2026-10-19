package claude

import "testing"

// Claude Code wraps pasted text in <pasted_content> tags; the web shows the
// prompt as written, and text typed around the paste stays apart from it.
func TestUnwrapPastes(t *testing.T) {
	for in, want := range map[string]string{
		"\n\n<pasted_content id=\"3dad\">\nfirst line\nsecond line\n</pasted_content id=\"3dad\">\n":                                "first line\nsecond line",
		"look at this:\n\n<pasted_content id=\"a1\">\nlog\n</pasted_content id=\"a1\">\nthanks":                                     "look at this:\n\nlog\nthanks",
		"<pasted_content id=\"a\">\none\n</pasted_content id=\"a\"> and <pasted_content id=\"b\">\ntwo\n</pasted_content id=\"b\">": "one and two",
		"plain prompt": "plain prompt",
		// Mismatched ids are not Claude Code's: left alone.
		"<pasted_content id=\"a\">\nx\n</pasted_content id=\"b\">": "<pasted_content id=\"a\">\nx\n</pasted_content id=\"b\">",
	} {
		if got := UnwrapPastes(in); got != want {
			t.Errorf("UnwrapPastes(%q) = %q, want %q", in, got, want)
		}
	}
}

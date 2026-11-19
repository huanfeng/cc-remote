package terminal

import (
	"regexp"
	"strings"
)

// A model validation or expired-login failure leaves Claude Code alive at its
// input prompt and may not emit a Stop hook. Match complete diagnostics, not
// prose about a failed tool or a retrying request.
var selectedModelFailure = regexp.MustCompile(`^There's an issue with the selected model \([^\r\n]+\)\. It may not exist or you may not have access to it\. Run /model to pick a different model\.$`)
var expiredLoginFailure = regexp.MustCompile(`^Login expired\s*[.·]\s*Please run /login\.?$`)

func terminalFailure(message string) bool {
	return selectedModelFailure.MatchString(message) || expiredLoginFailure.MatchString(message)
}

func (s *screen) modelError() string {
	message, _ := s.modelErrorState()
	return message
}

func (s *screen) modelErrorState() (string, uint64) {
	lines := s.lines()
	for y := len(lines) - 1; y >= 0; y-- {
		line := strings.TrimSpace(lines[y])
		// A newer assistant turn or active spinner supersedes any earlier error.
		if retrySpinner.MatchString(line) {
			return "", 0
		}
		if strings.HasPrefix(line, "❯") {
			if y == 0 || !s.rule(y-1) {
				return "", 0 // a later submitted prompt, not the current input box
			}
			continue
		}
		if !strings.HasPrefix(line, "● ") && !strings.HasPrefix(line, "Login expired") {
			continue
		}
		candidate := strings.TrimPrefix(line, "● ")
		revision := s.rowPaint[y]
		for next := y + 1; next < len(lines); next++ {
			if terminalFailure(strings.TrimSpace(candidate)) {
				break // a footer below a complete diagnostic is not its continuation
			}
			continuation := strings.TrimSpace(lines[next])
			if continuation == "" || strings.HasPrefix(continuation, "●") || strings.HasPrefix(continuation, "❯") || retrySpinner.MatchString(continuation) || s.rule(next) {
				break
			}
			width := 0
			for _, r := range lines[next-1] {
				width += runeWidth(r)
			}
			if width >= s.cols-1 {
				candidate += strings.Repeat(" ", max(0, s.cols-width)) + lines[next]
			} else {
				candidate += " " + continuation
			}
			revision = max(revision, s.rowPaint[next])
		}
		candidate = strings.TrimSpace(candidate)
		if terminalFailure(candidate) {
			return candidate, revision
		}
		return "", 0 // only the newest assistant output can be a terminal failure
	}
	return "", 0
}

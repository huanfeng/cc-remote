package server

import "strings"

// Read only the current screen, never the output history: the menu must
// disappear as soon as the user answers it in the terminal.
func terminalUsageLimit(screen string) string {
	lines := strings.Split(strings.TrimSpace(screen), "\n")
	lines = lines[max(0, len(lines)-32):]
	text := strings.Join(lines, "\n")
	lower := strings.ToLower(text)
	start := strings.LastIndex(lower, "you've hit your")
	if start < 0 {
		return ""
	}
	text = text[start:]
	flat := strings.ToLower(flatText(text))
	if !strings.Contains(flat, "limit") || !strings.Contains(flat, "whatdoyouwanttodo?") ||
		!strings.Contains(flat, "stopandwaitforlimittoreset") || !strings.Contains(flat, "switchtousagecredits") ||
		!strings.Contains(text, "❯") {
		return ""
	}
	// A later input prompt means this is a previous dialog left in scrollback.
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "❯" {
			return ""
		}
	}
	return strings.TrimSpace(text)
}

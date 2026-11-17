package terminal

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// UsageLimitMenu is the currently displayed interactive limit prompt.
type UsageLimitMenu struct {
	Title    string             `json:"title"`
	Reset    string             `json:"reset"`
	Options  []UsageLimitOption `json:"options"`
	Selected int                `json:"-"`
}

type UsageLimitOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var usageOption = regexp.MustCompile(`^(❯\s*)?([1-4])\.\s+(.+)$`)

// ParseUsageLimit recognizes a complete menu on the current screen, never a
// partial redraw or an old prompt followed by a fresh input prompt.
func ParseUsageLimit(screen string) *UsageLimitMenu {
	lines := strings.Split(ansi.ReplaceAllString(screen, ""), "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "You've hit your ") {
			start = i
		}
	}
	if start < 0 {
		return nil
	}
	question := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "What do you want to do?" {
			question = i
			break
		}
	}
	if question < 0 {
		return nil
	}
	// Heading and reset may wrap, but explanatory /upgrade text is separate.
	var heading []string
	for _, line := range lines[start:question] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "/") || strings.Contains(line, "──") || strings.Contains(line, "▔▔") {
			break
		}
		heading = append(heading, line)
	}
	h := strings.Join(heading, " ")
	title, reset, ok := strings.Cut(h, "resets ")
	if !ok {
		return nil
	}
	title = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(title), "·•—-"))
	if title != "You've hit your weekly limit" && title != "You've hit your session limit" && title != "You've hit your limit" {
		return nil
	}
	m := &UsageLimitMenu{Title: title, Reset: strings.TrimSpace(reset), Selected: -1}
	for _, line := range lines[question+1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		match := usageOption.FindStringSubmatch(line)
		if match != nil {
			if len(m.Options) >= 4 || match[2] != string(rune('1'+len(m.Options))) {
				return nil
			}
			if match[1] != "" {
				if m.Selected >= 0 {
					return nil
				}
				m.Selected = len(m.Options)
			}
			m.Options = append(m.Options, UsageLimitOption{ID: match[2], Label: match[3]})
		} else if strings.Contains(line, "❯") {
			return nil
		} else if len(m.Options) == 4 && m.Options[3].Label == "Upgrade your plan" {
			// Only the standard navigation hint may follow the completed menu.
			if !strings.Contains(line, "to select") && !strings.Contains(line, "to navigate") && !strings.Contains(line, "to confirm") {
				return nil
			}
		} else if len(m.Options) > 0 {
			m.Options[len(m.Options)-1].Label += " " + line
		} else {
			return nil
		}
	}
	if len(m.Options) != 4 || m.Selected < 0 || m.Reset == "" {
		return nil
	}
	if m.Options[0].Label != "Stop and wait for limit to reset" ||
		!(m.Options[1].Label == "Wait here, then continue automatically" || strings.HasPrefix(m.Options[1].Label, "Wait here, then continue automatically at ")) ||
		m.Options[2].Label != "Switch to usage credits" || m.Options[3].Label != "Upgrade your plan" {
		return nil
	}
	return m
}

func usageLimitKeys(expected, live *UsageLimitMenu, option string) ([]byte, error) {
	if expected == nil || live == nil || expected.Title != live.Title || expected.Reset != live.Reset || len(expected.Options) != 4 || len(live.Options) != 4 || live.Selected < 0 || live.Selected > 3 {
		return nil, errors.New("usage limit menu changed")
	}
	target := -1
	for i := range live.Options {
		if expected.Options[i] != live.Options[i] {
			return nil, errors.New("usage limit menu changed")
		}
		if live.Options[i].ID == option {
			target = i
		}
	}
	if target < 0 {
		return nil, errors.New("invalid usage limit option")
	}
	delta, arrow := target-live.Selected, "\x1b[B"
	if delta < 0 {
		delta, arrow = -delta, "\x1b[A"
	}
	return []byte(strings.Repeat(arrow, delta) + "\r"), nil
}

// ChooseUsageLimit validates the live screen and sends one selection gesture
// while holding the same lock used by screen updates.
func (r *Runner) ChooseUsageLimit(expected *UsageLimitMenu, option string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.exited || isClosed(r.done) {
		return errExited()
	}
	keys, err := usageLimitKeys(expected, ParseUsageLimit(r.screenLocked()), option)
	if err != nil {
		return err
	}
	r.lastKey = time.Now()
	return r.write(keys)
}

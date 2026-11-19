package terminal

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
)

type ResumeMenu struct {
	Title    string             `json:"title"`
	Message  string             `json:"message"`
	Options  []UsageLimitOption `json:"options"`
	Selected int                `json:"-"`
}

var resumeIndex = regexp.MustCompile(`^[1-3]\.\s*`)
var resumeChoices = []UsageLimitOption{{ID: "resume", Label: "Resume"}, {ID: "new_conversation", Label: "Start a new conversation"}, {ID: "compact", Label: "Resume from summary"}}

// ParseResumeMenu accepts the complete native cost confirmation, including the
// idle variant with hidden option numbers. Unrelated text is not actionable.
func ParseResumeMenu(screen string) *ResumeMenu {
	lines := strings.Split(ansi.ReplaceAllString(screen, ""), "\n")
	start := -1
	for i := range lines {
		for end := i; end < len(lines) && end < i+3; end++ {
			if flat(strings.Join(lines[i:end+1], "")) == "resumethisconversation?" {
				start = end
			}
		}
	}
	if start < 0 {
		return nil
	}
	m := &ResumeMenu{Title: "Resume this conversation?", Selected: -1}
	var message []string
	for i := start + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.Trim(line, "─╭╮╰╯│ ") == "" {
			continue
		}
		selected := strings.HasPrefix(line, "❯")
		candidate := resumeIndex.ReplaceAllString(strings.TrimSpace(strings.TrimPrefix(line, "❯")), "")
		matched, last := -1, i
		for end := i; end < len(lines) && end < i+3; end++ {
			if end > i {
				candidate += strings.TrimSpace(lines[end])
			}
			for k, choice := range resumeChoices {
				if flat(candidate) == flat(choice.Label) {
					matched, last = k, end
					break
				}
			}
			if matched >= 0 {
				break
			}
		}
		if matched >= 0 {
			if matched != len(m.Options) {
				return nil
			}
			if selected {
				if m.Selected >= 0 {
					return nil
				}
				m.Selected = matched
			}
			m.Options = append(m.Options, resumeChoices[matched])
			i = last
			continue
		}
		if selected || strings.HasPrefix(line, "●") || retrySpinner.MatchString(line) {
			return nil
		}
		if len(m.Options) == 0 {
			message = append(message, line)
			continue
		}
		// Native option descriptions and input hints are the only permitted footer.
		footerEnd := -1
		for end := i; end < len(lines) && end < i+5; end++ {
			text := flat(strings.Join(lines[i:end+1], ""))
			for _, description := range []string{"Claude can look this one up if you refer to it later", "Claude writes a summary now, and you continue from it", "Enter to confirm · Esc to resume", "Enter to confirm · Esc to dismiss"} {
				if text == flat(description) {
					footerEnd = end
				}
			}
		}
		if footerEnd >= 0 {
			i = footerEnd
			continue
		}
		footer := strings.ToLower(line)
		if strings.HasPrefix(line, "Claude can look this one up") || strings.HasPrefix(line, "Claude writes a summary") || strings.Contains(footer, "confirm") || strings.Contains(footer, "esc") || strings.Contains(footer, "pick a row first") {
			continue
		}
		return nil
	}
	m.Message = strings.Join(message, " ")
	compactMessage := flat(m.Message)
	if len(m.Options) < 2 || m.Selected < 0 || !strings.Contains(compactMessage, flat("This conversation has been inactive for ")) || !strings.Contains(compactMessage, flat("tokens long.")) || !strings.Contains(compactMessage, flat("5-hour usage limit.")) {
		return nil
	}
	return m
}

func resumeMenuKeys(expected, live *ResumeMenu, option string) ([]byte, error) {
	if expected == nil || live == nil || expected.Title != live.Title || expected.Message != live.Message || !slices.Equal(expected.Options, live.Options) || live.Selected < 0 || live.Selected >= len(live.Options) {
		return nil, errors.New("resume menu changed")
	}
	target := slices.IndexFunc(live.Options, func(o UsageLimitOption) bool { return o.ID == option })
	if target < 0 {
		return nil, errors.New("invalid resume option")
	}
	delta, arrow := target-live.Selected, "\x1b[B"
	if delta < 0 {
		delta, arrow = -delta, "\x1b[A"
	}
	return []byte(strings.Repeat(arrow, delta) + "\r"), nil
}

func (r *Runner) ChooseResumeMenu(expected *ResumeMenu, option string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.exited || isClosed(r.done) {
		return errExited()
	}
	keys, err := resumeMenuKeys(expected, ParseResumeMenu(r.screenLocked()), option)
	if err != nil {
		return err
	}
	r.lastKey = time.Now()
	return r.write(keys)
}

package terminal

import (
	"strings"
	"testing"
)

const resumeMenuScreen = "Resume this conversation?\nThis conversation has been inactive for 2 hours and is 120k tokens long.\nResuming it will use about 35% of your 5-hour usage limit.\n❯ 1. Resume\n  2. Start a new conversation\n  3. Resume from summary\nClaude writes a summary now, and you continue from it\nEnter to confirm · Esc to resume"

func TestParseResumeMenu(t *testing.T) {
	for _, input := range []string{resumeMenuScreen, strings.ReplaceAll(resumeMenuScreen, "1. ", ""), strings.ReplaceAll(resumeMenuScreen, "about 35%", "all"), strings.ReplaceAll(resumeMenuScreen, "Start a new conversation", "Start a new\nconversation"), strings.ReplaceAll(resumeMenuScreen, "Resume this conversation?", "Resume this\nconversation?")} {
		menu := ParseResumeMenu(input)
		if menu == nil || menu.Selected != 0 || len(menu.Options) != 3 || menu.Options[1].ID != "new_conversation" || !strings.Contains(menu.Message, "5-hour usage limit") {
			t.Fatalf("missing resume menu: %+v\n%s", menu, input)
		}
	}
	for _, input := range []string{"ordinary output", resumeMenuScreen + "\n❯ new prompt", strings.ReplaceAll(resumeMenuScreen, "❯ ", ""), strings.ReplaceAll(resumeMenuScreen, "Start a new conversation", "Run something else"), "● " + resumeMenuScreen, strings.ReplaceAll(resumeMenuScreen, "Resuming it will use about 35% of your 5-hour usage limit.", "")} {
		if menu := ParseResumeMenu(input); menu != nil {
			t.Fatalf("accepted incomplete or stale menu: %q", input)
		}
	}
}

func TestResumeMenuKeysValidateAndUseLiveSelection(t *testing.T) {
	expected := ParseResumeMenu(resumeMenuScreen)
	for selected := range 3 {
		live := ParseResumeMenu(resumeMenuScreen)
		live.Selected = selected
		for target := range 3 {
			keys, err := resumeMenuKeys(expected, live, live.Options[target].ID)
			delta, arrow := target-selected, "\x1b[B"
			if delta < 0 {
				delta, arrow = -delta, "\x1b[A"
			}
			if err != nil || string(keys) != strings.Repeat(arrow, delta)+"\r" {
				t.Fatalf("selection %d -> %d: %q %v", selected, target, keys, err)
			}
		}
	}
	live := ParseResumeMenu(resumeMenuScreen)
	live.Message = "different cost"
	if _, err := resumeMenuKeys(expected, live, "resume"); err == nil {
		t.Fatal("accepted changed menu")
	}
	if _, err := resumeMenuKeys(expected, nil, "resume"); err == nil {
		t.Fatal("accepted closed menu")
	}
	if _, err := resumeMenuKeys(expected, expected, "invalid"); err == nil {
		t.Fatal("accepted invalid option")
	}
}

func TestResumeMenuPhysicalWraps(t *testing.T) {
	for _, width := range []int{24, 40, 100} {
		screen := newScreen(width, 35)
		screen.write([]byte(strings.ReplaceAll(resumeMenuScreen, "\n", "\r\n")))
		if m := ParseResumeMenu(strings.Join(screen.lines(), "\n")); m == nil {
			t.Fatalf("missing wrapped menu at %d\n%s", width, strings.Join(screen.lines(), "\n"))
		}
	}
}

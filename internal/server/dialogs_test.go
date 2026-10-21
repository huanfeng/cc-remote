package server

import (
	"strings"
	"testing"
	"time"
)

func TestDialogShown(t *testing.T) {
	dialog := "● Bash(npm test)\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel · Tab to amend\n"
	quoted := "● Read(terminal.go)\n  shows \"Esc to cancel\" at the bottom\n" + strings.Repeat("output\n", 12) + "─────\n❯ \n─────\n  ⏸ manual mode on\n"
	for _, c := range []struct {
		name, screen string
		want         bool
	}{
		{"dialog", dialog, true},
		{"dialog over a status line", dialog + "  [OMC] | Model: Haiku\n  ctx: 6%\n\n\n", true},
		{"input box", "● Done\n─────\n❯ \n─────\n  ⏸ manual mode on\n", false},
		{"the words in output above the input box", quoted, false},
		{"empty", "", false},
	} {
		if got := dialogShown(c.screen); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

// The card is withdrawn only after its dialog was shown and stayed away dialogGrace.
func TestDialogStep(t *testing.T) {
	p := &pending{}
	at := time.Unix(1000, 0)
	tick := func(shown bool, after time.Duration) bool { return p.dialogStep(shown, at.Add(after)) }
	if tick(false, 0) || tick(false, 10*time.Second) {
		t.Fatal("withdrawn before the dialog was ever shown")
	}
	if tick(true, 11*time.Second) || tick(false, 12*time.Second) || tick(false, 13*time.Second) {
		t.Fatal("withdrawn within the grace")
	}
	if tick(true, 13500*time.Millisecond) || tick(false, 14*time.Second) || tick(false, 15*time.Second) {
		t.Fatal("a redraw that showed the dialog again did not restart the grace")
	}
	if !tick(false, 16*time.Second) {
		t.Fatal("not withdrawn after the dialog stayed away")
	}
}

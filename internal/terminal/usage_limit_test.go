package terminal

import (
	"encoding/json"
	"strings"
	"testing"
)

const limitScreen = "You've hit your weekly limit · resets Nov 19, 1am (Asia/Shanghai)\n/upgrade or /usage-credits to finish what you’re working on.\n────────\nWhat do you want to do?\n❯ 1. Stop and wait for limit to reset\n  2. Wait here, then continue automatically at Nov 19, 1am\n  3. Switch to usage credits\n  4. Upgrade your plan"

func TestParseUsageLimit(t *testing.T) {
	for _, screen := range []string{limitScreen, strings.ReplaceAll(limitScreen, "weekly", "session"), strings.ReplaceAll(limitScreen, "weekly ", ""), strings.ReplaceAll(limitScreen, "continue automatically", "continue\nautomatically"), strings.ReplaceAll(limitScreen, "Upgrade your plan", "Upgrade your\nplan"), strings.Replace(limitScreen, "/upgrade or /usage-credits to finish what you’re working on.\n────────", "▔▔▔▔▔▔▔", 1)} {
		m := ParseUsageLimit(screen)
		if m == nil || m.Selected != 0 || m.Reset != "Nov 19, 1am (Asia/Shanghai)" || len(m.Options) != 4 {
			t.Fatalf("menu: %+v", m)
		}
		b, err := json.Marshal(m)
		if err != nil || strings.Contains(strings.ToLower(string(b)), "selected") {
			t.Fatalf("json %s %v", b, err)
		}
	}
	for _, screen := range []string{limitScreen + "\n❯ ", limitScreen + "\n❯ ordinary input", strings.Replace(limitScreen, "❯ ", "", 1), strings.ReplaceAll(limitScreen, "4. Upgrade your plan", ""), strings.ReplaceAll(limitScreen, "Switch to usage credits", "Run arbitrary command"), "ordinary output", strings.ReplaceAll(limitScreen, "You've hit your", "Quoted You've hit your")} {
		if m := ParseUsageLimit(screen); m != nil {
			t.Fatalf("accepted stale/incomplete screen: %q", screen)
		}
	}
}

func TestUsageLimitKeys(t *testing.T) {
	expected := ParseUsageLimit(limitScreen)
	for selected := 0; selected < 4; selected++ {
		live := ParseUsageLimit(limitScreen)
		live.Selected = selected
		for target := 0; target < 4; target++ {
			got, err := usageLimitKeys(expected, live, live.Options[target].ID)
			delta, arrow := target-selected, "\x1b[B"
			if delta < 0 {
				delta, arrow = -delta, "\x1b[A"
			}
			want := strings.Repeat(arrow, delta) + "\r"
			if err != nil || string(got) != want {
				t.Fatalf("%d to %d: %q %v", selected, target, got, err)
			}
		}
	}
	live := ParseUsageLimit(limitScreen)
	if _, err := usageLimitKeys(expected, live, "5"); err == nil {
		t.Fatal("accepted invalid option")
	}
	live.Reset = "tomorrow"
	if _, err := usageLimitKeys(expected, live, "1"); err == nil {
		t.Fatal("accepted changed reset")
	}
	if _, err := usageLimitKeys(expected, nil, "1"); err == nil {
		t.Fatal("accepted disappeared menu")
	}
	r := &Runner{exited: true}
	if err := r.ChooseUsageLimit(expected, "1"); err == nil {
		t.Fatal("accepted exited runner")
	}
}

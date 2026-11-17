package server

import (
	"strings"
	"testing"
)

func TestTerminalUsageLimit(t *testing.T) {
	menu := "You've hit your weekly limit · resets Nov 19, 1am (Asia/Shanghai)\n/upgrade or /usage-credits to finish what you’re working on.\n────────\nWhat do you want to do?\n❯ 1. Stop and wait for limit to reset\n  2. Wait here, then continue automatically at Nov 19, 1am\n  3. Switch to usage credits\n  4. Upgrade your plan"
	for _, tc := range []struct {
		name, screen string
		want         bool
	}{
		{"weekly menu", menu, true},
		{"session menu", strings.ReplaceAll(menu, "weekly limit", "limit"), true},
		{"wrapped menu", strings.ReplaceAll(menu, "limit to reset", "limit to\n reset"), true},
		{"answered", menu + "\n──────\n❯ \n──────", false},
		{"old output", menu + strings.Repeat("\noutput", 40), false},
		{"notice without choices", "You've hit your weekly limit · resets Nov 19, 1am", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := terminalUsageLimit(tc.screen); (got != "") != tc.want {
				t.Fatalf("notice = %q, want detected %v", got, tc.want)
			}
		})
	}
}

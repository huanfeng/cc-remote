package terminal

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPickerEOFDoesNotChooseDefault(t *testing.T) {
	old := stdin
	defer func() { stdin = old }()
	stdin = bufio.NewReader(strings.NewReader(""))
	_, err := pickItems([]pickerItem{{id: "x", lines: [2]string{"x", "details"}}}, "x", nil, nil)
	if err == nil {
		t.Fatal("EOF selected default")
	}
}

func TestPickerFormattingIsSafeAndTwoLines(t *testing.T) {
	now := time.Now()
	reset := now.Add(2 * time.Hour)
	u := profileUsageSummary{Available: true, FetchedAt: &now, IdentityKnown: true, Windows: []quotaWindow{{Name: "5 小时", Percent: 24, WindowMinutes: 300, ResetsAt: &reset}, {Name: "7 天", Percent: 67, WindowMinutes: 10080}}}
	text := usageSummary(u)
	if !strings.Contains(text, "24%") || !strings.Contains(text, "↺2h") {
		t.Fatal(text)
	}
	item := pickerItem{id: "1", lines: [2]string{"危险\x1b[31m名字很长", text}}
	for _, line := range itemLines(item, 12) {
		if strings.ContainsAny(line, "\x1b\n\r") || displayWidth(line) > 12 {
			t.Fatal(line)
		}
	}
}

func TestSessionDefaultSurvivesGrouping(t *testing.T) {
	list := []remoteSession{{ID: "idle", Title: "recent", Attached: 0}, {ID: "run", Title: "older", Online: true, Status: "running"}}
	items, def := sessionItems(list, false)
	if def != "idle" || items[0].id != "run" {
		t.Fatalf("%s %+v", def, items)
	}
}

func TestPickerMultiDigitNumberMapsDisplayedItem(t *testing.T) {
	old := stdin
	defer func() { stdin = old }()
	stdin = bufio.NewReader(strings.NewReader("12\n2\n"))
	var items []pickerItem
	for i := 1; i <= 12; i++ {
		items = append(items, pickerItem{id: fmt.Sprintf("id-%d", i), lines: [2]string{fmt.Sprintf("%d  item", i), "details"}})
	}
	// Stable IDs and displayed numbers remain aligned even after reordering.
	items[0], items[11] = items[11], items[0]
	got, err := pickItems(items, "id-1", nil, nil)
	if err != nil || got != "id-12" {
		t.Fatalf("number 12 selected %q: %v", got, err)
	}
	// bufio may already have read the next prompt's answer; do not lose it.
	got, err = pickItems(items, "id-1", nil, nil)
	if err != nil || got != "id-2" {
		t.Fatalf("buffered second selection %q: %v", got, err)
	}
}

func TestChooseEOFNeverCreatesSession(t *testing.T) {
	old := stdin
	defer func() { stdin = old }()
	stdin = bufio.NewReader(strings.NewReader(""))
	created := false
	_, err := choose([]remoteSession{{ID: "attached", Attached: 1}}, func() (remoteSession, error) {
		created = true
		return remoteSession{}, nil
	})
	if created || !errors.Is(err, errPickerCancelled) {
		t.Fatalf("EOF created=%v err=%v", created, err)
	}
}

func TestUsageStaleCacheDoesNotClaimRefreshFailed(t *testing.T) {
	text := usageSummary(profileUsageSummary{Stale: true})
	if strings.Contains(text, "failed") || strings.Contains(text, "失败") {
		t.Fatal(text)
	}
}

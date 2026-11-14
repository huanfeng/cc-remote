package terminal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"
)

type pickerTestResult struct {
	ID           string
	Error        string
	ModeRestored bool
}

func pickerTestChild() {
	before, _ := term.GetState(int(os.Stdin.Fd()))
	id, err := pickItems([]pickerItem{
		{id: "alpha", lines: [2]string{"1  item Alpha", "details"}, search: "item Alpha"},
		{id: "beta", lines: [2]string{"2  item Beta", "details"}, search: "item Beta query"},
	}, "alpha", nil, nil)
	after, _ := term.GetState(int(os.Stdin.Fd()))
	result := pickerTestResult{ID: id, ModeRestored: before != nil && after != nil && reflect.DeepEqual(before, after)}
	if err != nil {
		result.Error = err.Error()
	}
	data, _ := json.Marshal(result)
	_ = os.WriteFile(os.Getenv("CC_REMOTE_PICKER_RESULT"), data, 0600)
}

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
	for _, line := range itemLines(item, 12, 1) {
		if strings.ContainsAny(line, "\x1b\n\r") || displayWidth(line) > 12 {
			t.Fatal(line)
		}
	}
}

func TestPickerNumberAndDetailsAlignment(t *testing.T) {
	items := []pickerItem{
		{lines: [2]string{"1  ● 执行中  修复登录页", "Claude · Sonnet · 工作账号"}},
		{lines: [2]string{"12  ○ 就绪  更新文档", "D:\\项目 · Claude"}},
		{lines: [2]string{"0  + 新建终端会话", ""}},
		{lines: [2]string{"2  工作账号 · OAuth", "已用 5h 24% ↺2h"}},
		{lines: [2]string{"10  团队 API · API", "无订阅额度"}},
	}
	want := [][2]string{
		{"1   ● 执行中  修复登录页", "    Claude · Sonnet · 工作账号"},
		{"12  ○ 就绪  更新文档", "    D:\\项目 · Claude"},
		{"0   + 新建终端会话", ""},
		{"2   工作账号 · OAuth", "    已用 5h 24% ↺2h"},
		{"10  团队 API · API", "    无订阅额度"},
	}
	numberWidth := pickerNumberWidth(items)
	for i, item := range items {
		if got := itemLines(item, 80, numberWidth); got != want[i] {
			t.Fatalf("item %d: got %q, want %q", i, got, want[i])
		}
		// The column width belongs to the full list, so filtering out two-digit
		// numbers does not move the remaining titles or details.
		for _, width := range []int{1, 4, 8, 12} {
			for _, line := range itemLines(item, width, numberWidth) {
				if displayWidth(line) > width || strings.ContainsAny(line, "\r\n\x1b") {
					t.Fatalf("width %d: %q", width, line)
				}
			}
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

func TestPickerQNeverCreatesSession(t *testing.T) {
	old := stdin
	defer func() { stdin = old }()
	for _, input := range []string{"q\n", "q"} {
		stdin = bufio.NewReader(strings.NewReader(input))
		created := false
		_, err := choose(nil, func() (remoteSession, error) {
			created = true
			return remoteSession{}, nil
		})
		if created || !errors.Is(err, errPickerCancelled) {
			t.Fatalf("q created=%v err=%v", created, err)
		}
	}
}

func TestUsageStaleCacheDoesNotClaimRefreshFailed(t *testing.T) {
	text := usageSummary(profileUsageSummary{Stale: true})
	if strings.Contains(text, "failed") || strings.Contains(text, "失败") {
		t.Fatal(text)
	}
}

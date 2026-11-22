package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"cc-remote/internal/i18n"
	"golang.org/x/term"
)

var errPickerCancelled = errors.New("selection cancelled")

const recentLimit = 20

type pickerItem struct {
	id            string
	lines         [2]string
	search, group string
	warning       bool
}
type pickerRefresh func() ([]pickerItem, error)
type quotaWindow struct {
	Name          string     `json:"name"`
	Percent       float64    `json:"percent"`
	WindowMinutes int        `json:"windowMinutes"`
	ResetsAt      *time.Time `json:"resetsAt"`
}
type profileUsageSummary struct {
	Available     bool          `json:"available"`
	Reason        string        `json:"reason"`
	IdentityKnown bool          `json:"identityKnown"`
	Windows       []quotaWindow `json:"windows"`
	FetchedAt     *time.Time    `json:"fetchedAt"`
	Stale         bool          `json:"stale"`
	Error         string        `json:"error"`
	Shared        bool          `json:"shared"`
}

func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}
func clipText(s string, width int) string {
	s = printable(s)
	if displayWidth(s) <= width {
		return s
	}
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		w := runeWidth(r)
		if n+w > width-1 {
			break
		}
		b.WriteRune(r)
		n += w
	}
	return b.String() + "…"
}
func pickerNumberWidth(items []pickerItem) int {
	width := 1
	for _, item := range items {
		number, _, _ := strings.Cut(item.lines[0], " ")
		if _, err := strconv.Atoi(number); err == nil {
			width = max(width, len(number))
		}
	}
	return width
}

func itemLines(item pickerItem, width, numberWidth int) [2]string {
	first, second := printable(item.lines[0]), printable(item.lines[1])
	number, title, ok := strings.Cut(first, " ")
	if _, err := strconv.Atoi(number); ok && err == nil {
		first = fmt.Sprintf("%-*s  %s", numberWidth, number, strings.TrimLeft(title, " "))
		if second != "" {
			second = strings.Repeat(" ", numberWidth+2) + second
		}
	}
	return [2]string{clipText(first, width), clipText(second, width)}
}
func shortDuration(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd", int(math.Ceil(d.Hours()/24)))
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(math.Ceil(d.Hours())))
	}
	return fmt.Sprintf("%dm", int(math.Ceil(d.Minutes())))
}
func usageSummary(u profileUsageSummary) string {
	var parts []string
	if u.Error != "" {
		parts = append(parts, i18n.T("old · update failed", "旧值·更新失败"))
	} else if u.Stale {
		parts = append(parts, i18n.T("outdated", "数据过期"))
	}
	for _, w := range u.Windows {
		if len(parts) >= 4 {
			break
		}
		name := w.Name
		if w.WindowMinutes == 300 {
			name = "5h"
		} else if w.WindowMinutes == 10080 {
			name = "7d"
		}
		value := "—"
		if !math.IsNaN(w.Percent) && !math.IsInf(w.Percent, 0) && w.Percent >= 0 && w.Percent <= 100 {
			value = strconv.FormatFloat(w.Percent, 'f', 0, 64) + "%"
		}
		label := name + " " + value
		if w.ResetsAt != nil {
			label += " ↺" + shortDuration(time.Until(*w.ResetsAt))
		}
		parts = append(parts, label)
	}
	if len(u.Windows) > 0 {
		parts = append([]string{i18n.T("Used", "已用")}, parts...)
	} else {
		switch u.Reason {
		case "api-key", "third-party":
			parts = append(parts, i18n.T("No subscription quota", "无订阅额度"))
		case "no-login":
			parts = append(parts, i18n.T("Not signed in", "未登录"))
		default:
			parts = append(parts, i18n.T("No reading", "尚无数据"))
		}
	}
	if u.FetchedAt != nil {
		parts = append(parts, i18n.T("read ", "采集")+shortDuration(time.Since(*u.FetchedAt))+i18n.T(" ago", "前"))
	}
	if !u.IdentityKnown && len(u.Windows) > 0 {
		parts = append(parts, i18n.T("identity unconfirmed", "身份未确认"))
	}
	if u.Shared {
		parts = append(parts, i18n.T("shared", "共用额度"))
	}
	return strings.Join(parts, " · ")
}

func sessionItems(list []remoteSession, create bool) ([]pickerItem, string) {
	if len(list) > recentLimit {
		list = list[:recentLimit]
	}
	def := ""
	for _, x := range list {
		if x.Attached == 0 {
			def = x.ID
			break
		}
	}
	if def == "" && !create && len(list) > 0 {
		def = list[0].ID
	}
	showCwd := false
	for _, x := range list {
		showCwd = showCwd || !samePath(x.Cwd, list[0].Cwd)
	}
	ordered := append([]remoteSession{}, list...)
	busy := func(x remoteSession) bool { return x.Online && (x.Status == "running" || x.Status == "waiting") }
	sort.SliceStable(ordered, func(i, j int) bool { return busy(ordered[i]) && !busy(ordered[j]) })
	items := []pickerItem{}
	for i, x := range ordered {
		group := i18n.T("Other sessions", "其他会话")
		if busy(x) {
			group = i18n.T("In progress", "进行中")
		}
		state := statusText(x.Status)
		if x.Mode == "" {
			state = i18n.T("Restorable", "可恢复")
		} else if !x.Online {
			state = i18n.T("Not running", "未运行")
		}
		marker := "○"
		if busy(x) {
			marker = "●"
		}
		if x.Status == "waiting" && x.Online {
			marker = "!"
		}
		model := x.Model
		if model == "" {
			model = i18n.T("model unknown", "模型未知")
		}
		profile := x.ProfileName
		if profile == "" {
			profile = i18n.T("supplier unknown", "供应商未知")
		}
		id := x.ID
		if len(id) > 8 {
			id = id[:8]
		}
		agent := "Claude"
		if x.agent() == "codex" {
			agent = "Codex"
		}
		details := fmt.Sprintf("%s · %s · %s · %d %s · %s", agent, model, profile, x.Attached, i18n.T("terminals", "终端"), id)
		if showCwd {
			details = x.Cwd + " · " + details
		}
		items = append(items, pickerItem{id: x.ID, group: group, lines: [2]string{fmt.Sprintf("%d  %s %s  %s · %s", i+1, marker, state, x.Title, ago(x.UpdatedAt)), details}, search: x.Title + " " + x.Cwd + " " + x.ID + " " + model + " " + profile, warning: x.Status == "waiting" || x.Status == "failed"})
	}
	if create {
		items = append(items, pickerItem{id: "", lines: [2]string{"0  + " + i18n.T("New terminal session", "新建终端会话"), ""}})
	}
	return items, def
}

// pickItems returns stable IDs; rendering order never determines the default.
func pickItems(items []pickerItem, def string, refresh pickerRefresh, updates <-chan []pickerItem) (string, error) {
	interactive := pickerInteractive()
	if !interactive {
		numberWidth := pickerNumberWidth(items)
		for _, item := range items {
			lines := itemLines(item, 118, numberWidth)
			mark := "  "
			if item.id == def {
				mark = "* "
			}
			fmt.Println(mark + lines[0])
			if lines[1] != "" {
				fmt.Println("  " + lines[1])
			}
		}
		fmt.Print(i18n.T("Number (Enter: default, q: quit): ", "选择编号（回车=默认，q退出）："))
		line, err := stdin.ReadString('\n')
		if err != nil && !(err == io.EOF && strings.TrimSpace(line) != "") {
			return "", errPickerCancelled
		}
		line = strings.TrimSpace(line)
		if line == "q" {
			return "", errPickerCancelled
		}
		if line == "" {
			return def, nil
		}
		for _, item := range items {
			n := strings.Fields(item.lines[0])
			if len(n) > 0 && n[0] == line {
				return item.id, nil
			}
		}
		return "", i18n.Errorf("invalid choice", "无效的选择")
	}
	state, err := makePickerRaw(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	defer term.Restore(int(os.Stdin.Fd()), state)
	restoreVT := enableVT(os.Stdout)
	defer restoreVT()
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[0m\x1b[?25h\r\n")
	selected := def
	filter, input, message := "", "", ""
	searching := false
	drawn := 0
	visible := func() []pickerItem {
		var out []pickerItem
		for _, item := range items {
			if filter == "" || strings.Contains(strings.ToLower(item.search+" "+item.lines[0]), strings.ToLower(filter)) {
				out = append(out, item)
			}
		}
		return out
	}
	for {
		rows := visible()
		width, height, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil {
			width, height = 80, 24
		}
		width = max(1, width-1)
		maxItems := max(1, (height-5)/3)
		pos := 0
		for i, item := range rows {
			if item.id == selected {
				pos = i
				break
			}
		}
		start := max(0, pos-maxItems+1)
		end := min(len(rows), start+maxItems)
		if drawn > 0 {
			fmt.Printf("\x1b[%dA", drawn)
		}
		var lines []string
		group := ""
		color := os.Getenv("NO_COLOR") == ""
		numberWidth := pickerNumberWidth(items)
		for _, item := range rows[start:end] {
			if item.group != "" && item.group != group {
				lines = append(lines, item.group)
				group = item.group
			}
			text := itemLines(item, width-2, numberWidth)
			for i, line := range text {
				if i == 1 && line == "" {
					continue
				}
				prefix, mark := "  ", ""
				if i == 0 {
					if item.id == selected {
						prefix = "› "
					}
					if item.id == def {
						mark = " *"
						line = clipText(line, width-4)
					}
				}
				a := clipText(prefix+line+mark, width)
				if color && item.id == selected {
					a = "\x1b[7m" + a + "\x1b[0m"
				} else if color && item.warning && i == 0 {
					a = "\x1b[33m" + a + "\x1b[0m"
				}
				lines = append(lines, a)
			}
		}
		if len(rows) == 0 {
			lines = append(lines, i18n.T("No matches", "无匹配项"))
		}
		if searching {
			lines = append(lines, "/ "+filter)
		} else {
			lines = append(lines, i18n.T("↑↓ Enter · / search · r refresh · q/Esc quit", "↑↓ Enter确认 · /筛选 · r刷新 · q/Esc退出")+" "+input)
		}
		if message != "" {
			lines = append(lines, message)
		}
		n := min(max(1, height-1), max(drawn, len(lines)))
		for i := 0; i < n; i++ {
			fmt.Print("\r\x1b[2K")
			if i < len(lines) {
				fmt.Print(clipANSI(lines[i], width))
			}
			fmt.Print("\r\n")
		}
		drawn = n
		var key string
		for key == "" {
			select {
			case next, ok := <-updates:
				if !ok {
					updates = nil
				} else {
					items = next
				}
				key = "update"
			default:
			}
			if key != "" {
				break
			}
			key, err = readPickerKey(80 * time.Millisecond)
			if err != nil {
				return "", err
			}
		}
		if key == "q" && !searching {
			return "", errPickerCancelled
		}
		switch key {
		case "update":
			continue
		case "ctrl-c":
			return "", errPickerCancelled
		case "escape":
			if searching {
				searching = false
				filter = ""
				continue
			}
			return "", errPickerCancelled
		case "up", "down":
			if len(rows) > 0 {
				if key == "up" {
					pos = (pos + len(rows) - 1) % len(rows)
				} else {
					pos = (pos + 1) % len(rows)
				}
				selected = rows[pos].id
			}
			input = ""
		case "enter":
			if len(rows) == 0 {
				continue
			}
			if input != "" {
				found := false
				for _, item := range rows {
					f := strings.Fields(item.lines[0])
					if len(f) > 0 && f[0] == input {
						selected = item.id
						found = true
						break
					}
				}
				if !found {
					message = i18n.T("Invalid number", "编号无效")
					continue
				}
			}
			for _, item := range rows {
				if item.id == selected {
					return selected, nil
				}
			}
			message = i18n.T("Select an item", "请选择条目")
		case "backspace":
			if searching {
				r := []rune(filter)
				if len(r) > 0 {
					filter = string(r[:len(r)-1])
				}
			} else if len(input) > 0 {
				input = input[:len(input)-1]
			}
		default:
			if searching {
				filter += key
				if filtered := visible(); len(filtered) > 0 {
					selected = filtered[0].id
				}
			} else if key == "/" {
				searching = true
				input = ""
			} else if key == "r" && refresh != nil {
				// A refresh can renumber sessions. Discard partial numeric input
				// so Enter keeps the selected ID instead of applying an old number.
				input = ""
				next, e := refresh()
				if e != nil {
					message = i18n.T("Refresh failed", "刷新失败")
				} else {
					items = next
					message = ""
				}
			} else if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
				input += key
			}
		}
	}
}

// ANSI is generated only by the renderer, after untrusted text was sanitized.
func clipANSI(s string, width int) string {
	if strings.HasPrefix(s, "\x1b[") {
		return s
	}
	return clipText(s, width)
}

func pickerInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("TERM") != "dumb" && stdin.Buffered() == 0
}

func (c *client) readProfileUsage(parent context.Context) map[string]profileUsageSummary {
	ctx, cancel := context.WithTimeout(parent, 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", c.host+"/api/profiles/usage?app=claude", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := c.http().Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil
	}
	var reply struct {
		Profiles map[string]profileUsageSummary `json:"profiles"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&reply) != nil {
		return nil
	}
	return reply.Profiles
}
func (c *client) chooseSupplier(app string, list []supplierProfile, defaultID string) (string, error) {
	makeItems := func(usage map[string]profileUsageSummary) []pickerItem {
		label := i18n.T("Host configuration", "宿主配置")
		if defaultID == "@keep" {
			label = i18n.T("Keep session configuration", "沿用原会话配置")
		} else if app == "codex" && defaultID != "" && defaultID != "@native" {
			label = i18n.F("Unavailable (%s)", "不可用（%s）", defaultID)
		}
		for _, p := range list {
			if p.ID == defaultID {
				label = p.Name
			}
		}
		items := []pickerItem{{id: "", lines: [2]string{"0  " + i18n.T("Default: ", "默认：") + label, ""}}}
		for i, p := range list {
			kind := "API"
			if p.Kind == "oauth" {
				kind = "OAuth"
			} else if p.ID == "@native" {
				kind = i18n.T("Native", "本机配置")
			}
			name := fmt.Sprintf("%d  %s · %s", i+1, p.Name, kind)
			if p.ID == defaultID {
				name += " · " + i18n.T("default", "默认")
			}
			note := p.ID
			if app == "claude" {
				note = i18n.T("Quota unavailable", "用量暂不可用")
				if u, ok := usage[p.ID]; ok {
					note = usageSummary(u)
				}
			}
			items = append(items, pickerItem{id: p.ID, lines: [2]string{name, note}, search: p.Name + " " + p.ID + " " + kind})
		}
		return items
	}
	if app != "claude" {
		return pickItems(makeItems(nil), "", nil, nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !pickerInteractive() {
		return pickItems(makeItems(c.readProfileUsage(ctx)), "", nil, nil)
	}
	updates := make(chan []pickerItem, 1)
	go func() { updates <- makeItems(c.readProfileUsage(ctx)); close(updates) }()
	return pickItems(makeItems(nil), "", func() ([]pickerItem, error) { return makeItems(c.readProfileUsage(ctx)), nil }, updates)
}

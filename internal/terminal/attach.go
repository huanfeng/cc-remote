package terminal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"

	"github.com/coder/websocket"
	"golang.org/x/term"
)

// resetModes undoes terminal modes Claude Code's TUI turns on (bracketed paste,
// focus and mouse reporting, keyboard protocols) so the local terminal works
// normally after detaching.
const resetModes = "\x1b[?2004l\x1b[?1004l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2031l\x1b[?9001l\x1b[<99u\x1b[>4m\x1b[?25h\x1b[0m"

type remoteSession struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Cwd    string `json:"cwd"`
	Mode   string `json:"mode"`
	Status string `json:"status"`
	Online bool   `json:"online"`
	// Attached counts the terminals attached to it now.
	Attached int `json:"attached"`
	// UpdatedAt is the last activity (RFC 3339).
	UpdatedAt string `json:"updatedAt"`
}

// Attach is `cc-remote attach`: it connects this terminal to a terminal
// session the host holds.
func Attach(args []string, defaultDataDir string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	host := fs.String("host", "http://127.0.0.1:8787", i18n.T("cc-remote host URL", "cc-remote 宿主 URL"))
	dataDir := fs.String("data-dir", defaultDataDir, i18n.T("cc-remote data directory (access token)", "cc-remote 数据目录（访问令牌所在）"))
	create := fs.Bool("new", false, i18n.T("start a new terminal session in the current directory", "在当前目录新建终端会话"))
	title := fs.String("title", "", newTitleHelp())
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), i18n.T("usage: cc-remote attach [--new [--title T]] [session id prefix or title]", "用法：cc-remote attach [--new [--title T]] [会话 ID 前缀或标题]"))
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	api, err := newClient(*host, *dataDir)
	if err != nil {
		return err
	}
	var target remoteSession
	if *create {
		target, err = api.create(*title, nil)
	} else {
		target, err = api.pick(strings.Join(fs.Args(), " "))
	}
	if err != nil {
		return err
	}
	if !confirmAttached(target) {
		return errors.New(i18n.T("not attached", "未接入"))
	}
	return api.attach(target)
}

func newTitleHelp() string {
	return i18n.T("title of a new session (default: directory name)", "新会话的标题（默认：目录名）")
}

// CCR is `ccr`: it attaches to this directory's terminal session, creating one
// when there is none; with Claude Code options it starts one with them, and
// -r/-c continue a Claude Code session. ensureHost starts a background host
// when none runs.
func CCR(args []string, defaultDataDir string, ensureHost func(dataDir, host string) error) error {
	for _, a := range args {
		if a == "--shared" {
			return errors.New(i18n.T("the shared mode was removed: ccr [claude options] starts Claude Code with them in a terminal session", "共享模式已移除：ccr [claude 参数] 会带着这些参数在终端会话中启动 Claude Code"))
		}
	}
	split := splitCCR(args)
	resume := split.resume
	fs := flag.NewFlagSet("ccr", flag.ContinueOnError)
	host := fs.String("host", "http://127.0.0.1:8787", i18n.T("cc-remote host URL", "cc-remote 宿主 URL"))
	dataDir := fs.String("data-dir", defaultDataDir, i18n.T("cc-remote data directory", "cc-remote 数据目录"))
	create := fs.Bool("new", false, i18n.T("start a new terminal session in this directory", "在当前目录新建终端会话"))
	title := fs.String("title", "", newTitleHelp())
	model := fs.String("model", "", i18n.T("model of a new session", "新会话的模型"))
	effort := fs.String("effort", "", i18n.T("reasoning effort of a new session", "新会话的推理强度"))
	mode := fs.String("permission-mode", "", i18n.T("permission mode of a new session", "新会话的权限模式"))
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), i18n.T(`usage: ccr [--new] [--title T] [--model M] [--effort E] [--permission-mode P] [session id prefix or title]
       ccr -r, --resume [id or search]   (continue a Claude Code session as a terminal session; no value: pick one of this directory's)
       ccr -c, --continue                (continue this directory's most recent Claude Code session)
       ccr ls                            (recent terminal sessions in every directory; pick one to attach)
       ccr [claude options] [prompt]     (a new terminal session: Claude Code starts with the options; the prompt is its first message)
       ccr -- [claude options] [prompt]  (the same, everything after -- is for Claude Code)

ccr attaches to this directory's terminal session, or starts one. Ctrl+] detaches; Claude Code keeps running on the host.
Claude Code options the host sets itself (--settings, --session-id, -p and the like) are refused.
`, `用法：ccr [--new] [--title T] [--model M] [--effort E] [--permission-mode P] [会话 ID 前缀或标题]
      ccr -r, --resume [ID 或关键词]    （以终端会话继续 Claude Code 会话；不带值则从当前目录的会话中选择）
      ccr -c, --continue                （继续当前目录最近的 Claude Code 会话）
      ccr ls                            （所有目录最近的终端会话，选一个接入）
      ccr [claude 参数] [提示词]        （新建终端会话：Claude Code 带这些参数启动，提示词作为第一条消息）
      ccr -- [claude 参数] [提示词]     （同上，-- 之后都交给 Claude Code）

ccr 接入当前目录的终端会话，没有则新建一个。Ctrl+] 断开；Claude Code 继续在宿主上运行。
宿主自己设置的 Claude Code 参数（--settings、--session-id、-p 等）不能使用。
`))
		fs.PrintDefaults()
	}
	if err := fs.Parse(split.own); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if err := ensureHost(*dataDir, *host); err != nil {
		return err
	}
	api, err := newClient(*host, *dataDir)
	if err != nil {
		return err
	}
	options := map[string]any{"model": *model, "effort": *effort, "permissionMode": *mode}
	if len(split.claude) > 0 {
		options["claudeArgs"] = split.claude
	}
	if *title == "" {
		*title = split.name()
	}
	// Claude Code options (or "--") start Claude Code with them, as claude
	// does: the words are then its first message, not a session to attach.
	launch := split.launch || len(split.claude) > 0
	var target remoteSession
	switch {
	case resume.set:
		target, err = api.resume(resume, *title, options)
	case *create || launch:
		target, err = api.create(*title, options)
	case len(split.words) == 1 && (split.words[0] == "ls" || split.words[0] == "list"):
		target, err = api.recent(*title, options)
	case len(split.words) > 0:
		target, err = api.pick(strings.Join(split.words, " "))
	default:
		target, err = api.here(*title, options)
	}
	if err != nil {
		return err
	}
	if !confirmAttached(target) {
		return errors.New(i18n.T("not attached; ccr --new starts another session in this directory", "未接入；ccr --new 可以在当前目录另开一个会话"))
	}
	if prompt := strings.Join(split.words, " "); prompt != "" && (launch || resume.set) {
		if err = api.do("POST", "/api/sessions/"+target.ID+"/messages", map[string]string{"text": prompt}, nil); err != nil {
			return err
		}
	}
	return api.attach(target)
}

// resumeArgs are Claude Code's resume flags given to ccr, so the command
// Claude Code prints on exit works with ccr in place of claude.
type resumeArgs struct {
	set   bool
	query string // -r/--resume value: a session ID (or prefix) or words of its title
	last  bool   // -c/--continue
}

// ccrArgs is a ccr command line split into ccr's own options, Claude Code's
// resume options, other Claude Code options and positional words.
type ccrArgs struct {
	own    []string // ccr's options, for its flag set
	resume resumeArgs
	claude []string // Claude Code options the session starts with
	words  []string // a session to attach, or Claude Code's first message
	launch bool     // "--" was given: everything after it is for Claude Code
}

// ccr's own options; --model, --effort and --permission-mode are also Claude
// Code's and become the session's settings.
var ccrValueFlags = map[string]bool{"host": true, "data-dir": true, "title": true, "model": true, "effort": true, "permission-mode": true}

func splitCCR(args []string) ccrArgs {
	var out ccrArgs
	afterDash := false
	for i := 0; i < len(args); {
		a := args[i]
		if a == "--" && !afterDash {
			out.launch, afterDash = true, true
			i++
			continue
		}
		name, _, inline := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch {
		case !strings.HasPrefix(a, "-") || a == "-":
			out.words = append(out.words, a)
			i++
		case a == "-r" || a == "--resume" || a == "-resume" || strings.HasPrefix(a, "--resume="):
			out.resume.set = true
			switch {
			case inline:
				out.resume.query = a[strings.Index(a, "=")+1:]
			case i+1 < len(args) && !strings.HasPrefix(args[i+1], "-"):
				i++
				out.resume.query = args[i]
			}
			i++
		case a == "-c" || a == "--continue" || a == "-continue":
			out.resume.set, out.resume.last = true, true
			i++
		case (strings.HasPrefix(a, "--") || !afterDash) && (ccrValueFlags[name] || name == "new" || name == "h" || name == "help"):
			if ccrValueFlags[name] && !inline && i+1 < len(args) {
				out.own = append(out.own, a, args[i+1])
				i += 2
			} else {
				out.own = append(out.own, a)
				i++
			}
		default: // a Claude Code option, with the values Claude Code would take
			arg := claude.ParseArgs(args[i:])[0]
			out.claude = append(out.claude, arg.Words()...)
			i += len(arg.Words())
		}
	}
	return out
}

// name is Claude Code's -n/--name option, the session's title by default.
func (a ccrArgs) name() string {
	for _, arg := range claude.ParseArgs(a.claude) {
		if (arg.Name == "-n" || arg.Name == "--name") && len(arg.Values) > 0 {
			return arg.Values[0]
		}
	}
	return ""
}

// resume finds the Claude Code session to continue and the terminal session
// continuing it (the host creates one in the session's own directory).
func (c *client) resume(r resumeArgs, title string, options map[string]any) (remoteSession, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return remoteSession{}, err
	}
	var all []remoteSession // the host's list of Claude Code sessions, newest first
	if err = c.do("GET", "/api/native/sessions", nil, &all); err != nil {
		return remoteSession{}, err
	}
	var here []remoteSession
	for _, x := range all {
		if samePath(x.Cwd, cwd) {
			here = append(here, x)
		}
	}
	var list []remoteSession
	switch {
	case r.query != "":
		q := strings.ToLower(r.query)
		for _, x := range all {
			if strings.HasPrefix(x.ID, q) {
				list = append(list, x)
			}
		}
		if len(list) == 0 {
			for _, x := range here {
				if strings.Contains(strings.ToLower(x.Title), q) {
					list = append(list, x)
				}
			}
		}
		if len(list) == 0 {
			return remoteSession{}, i18n.Errorf("no Claude Code session matches %q", "没有匹配 %q 的 Claude Code 会话", r.query)
		}
	case len(here) == 0:
		return remoteSession{}, errors.New(i18n.T("no Claude Code session in this directory to continue", "当前目录没有可继续的 Claude Code 会话"))
	case r.last:
		list = here[:1]
	default:
		list = here
	}
	target := list[0]
	if len(list) > 1 {
		fmt.Println(i18n.T("Claude Code sessions:", "Claude Code 会话："))
		if target, err = choose(list, nil); err != nil {
			return remoteSession{}, err
		}
	}
	// Continuing a session is trusting its directory, as running claude --resume there.
	body := map[string]any{"title": title, "mode": "terminal", "trustFolder": true, "resume": target.ID}
	for k, v := range options {
		if v != "" {
			body[k] = v
		}
	}
	var x remoteSession
	err = c.do("POST", "/api/sessions", body, &x)
	return x, err
}

func newClient(host, dataDir string) (*client, error) {
	token := strings.TrimSpace(os.Getenv("CC_REMOTE_TOKEN"))
	if token == "" {
		b, err := os.ReadFile(filepath.Join(dataDir, "access-token"))
		if err != nil {
			return nil, i18n.Errorf("cannot read the access token (%v); point --data-dir at the host's data directory", "读取访问令牌失败（%v）；用 --data-dir 指定宿主的数据目录", err)
		}
		token = strings.TrimSpace(string(b))
	}
	return &client{host: strings.TrimRight(host, "/"), token: token}, nil
}

// here returns this directory's terminal session: the only one, a choice
// among several, or a new one.
func (c *client) here(title string, options map[string]any) (remoteSession, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return remoteSession{}, err
	}
	all, err := c.terminals()
	if err != nil {
		return remoteSession{}, err
	}
	var list []remoteSession
	for _, x := range all {
		if samePath(x.Cwd, cwd) {
			list = append(list, x)
		}
	}
	if len(list) == 0 {
		fmt.Println(i18n.T("No terminal session in this directory yet; starting one…", "这个目录还没有终端会话，正在新建…"))
		return c.create(title, options)
	}
	// A directory usually has several sessions: always ask, Enter taking the
	// most recent one no terminal is attached to.
	fmt.Println(i18n.F("Terminal sessions in this directory (%d):", "这个目录的终端会话（%d 个）：", len(list)))
	return choose(list, func() (remoteSession, error) { return c.create(title, options) })
}

// recentLimit is how many sessions the pickers list.
const recentLimit = 20

// recent lists the most recently active terminal sessions to pick from.
func (c *client) recent(title string, options map[string]any) (remoteSession, error) {
	list, err := c.terminals()
	if err != nil {
		return remoteSession{}, err
	}
	if len(list) == 0 {
		fmt.Println(i18n.T("No terminal sessions yet; starting one in this directory…", "还没有终端会话，正在当前目录新建…"))
		return c.create(title, options)
	}
	fmt.Println(i18n.T("Recent terminal sessions:", "最近的终端会话："))
	return choose(list, func() (remoteSession, error) { return c.create(title, options) })
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

type client struct{ host, token string }

func (c *client) http() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}}
}

func (c *client) do(method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.host+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return i18n.Errorf("cannot reach the host at %s: %w", "无法连接宿主 %s：%w", c.host, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return i18n.Errorf("the host returned %d", "宿主返回 %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *client) create(title string, options map[string]any) (remoteSession, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return remoteSession{}, err
	}
	if title == "" {
		title = filepath.Base(cwd)
	}
	// Running attach --new (or ccr) in a directory is the user's decision to trust it.
	body := map[string]any{"title": title, "cwd": cwd, "mode": "terminal", "trustFolder": true}
	for k, v := range options {
		if v != "" {
			body[k] = v
		}
	}
	var x remoteSession
	err = c.do("POST", "/api/sessions", body, &x)
	return x, err
}

func (c *client) terminals() ([]remoteSession, error) {
	var all []remoteSession
	if err := c.do("GET", "/api/sessions", nil, &all); err != nil {
		return nil, err
	}
	var list []remoteSession
	for _, x := range all {
		if x.Mode == "terminal" {
			list = append(list, x)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].UpdatedAt > list[j].UpdatedAt }) // most recent first
	return list, nil
}

// ago is a short "how long ago" for an RFC 3339 time.
func ago(at string) string {
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return ""
	}
	switch d := time.Since(t); {
	case d < time.Minute:
		return i18n.T("just now", "刚刚")
	case d < time.Hour:
		return i18n.F("%dm ago", "%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return i18n.F("%dh ago", "%d 小时前", int(d.Hours()))
	default:
		return i18n.F("%dd ago", "%d 天前", int(d.Hours()/24))
	}
}

func statusText(status string) string {
	switch status {
	case "running":
		return i18n.T("running", "执行中")
	case "waiting":
		return i18n.T("waiting for an answer", "等待回答")
	case "idle":
		return i18n.T("ready", "就绪")
	case "completed":
		return i18n.T("completed", "已完成")
	case "interrupted":
		return i18n.T("stopped", "已停止")
	case "failed":
		return i18n.T("failed", "失败")
	}
	return status
}

// choose asks for one of list by number; with create, 0 starts a new session.
func choose(list []remoteSession, create func() (remoteSession, error)) (remoteSession, error) {
	if len(list) > recentLimit {
		list = list[:recentLimit]
	}
	// The default: the most recent session no terminal is attached to, else a
	// new one (or the first when none can be created).
	def := -1
	for i, x := range list {
		if x.Attached == 0 {
			def = i
			break
		}
	}
	if def < 0 && create == nil {
		def = 0
	}
	showCwd := false
	for _, x := range list {
		showCwd = showCwd || !samePath(x.Cwd, list[0].Cwd)
	}
	for i, x := range list {
		mark := " "
		if i == def {
			mark = "*"
		}
		where := ""
		if showCwd {
			where = "  " + x.Cwd
		}
		fmt.Printf("%s%2d. %s  [%s]%s  (%s)\n", mark, i+1, x.Title, sessionState(x), where, x.ID[:8])
	}
	if create != nil {
		mark := " "
		if def < 0 {
			mark = "*"
		}
		fmt.Println(mark + i18n.T(" 0. New terminal session", " 0. 新建终端会话"))
	}
	fmt.Print(i18n.F("Session number (Enter: %d): ", "选择会话编号（回车 = %d）：", def+1))
	line, _ := stdin.ReadString('\n')
	n, err := def+1, error(nil)
	if line = strings.TrimSpace(line); line != "" {
		n, err = strconv.Atoi(line)
	}
	if err == nil && n == 0 && create != nil {
		return create()
	}
	if err != nil || n < 1 || n > len(list) {
		return remoteSession{}, errors.New(i18n.T("invalid choice", "无效的选择"))
	}
	return list[n-1], nil
}

// stdin is shared by the prompts, so a line typed ahead is not lost between them.
var stdin = bufio.NewReader(os.Stdin)

// sessionState is a session's state for the picker: running or not, the
// terminals attached to it and its last activity.
func sessionState(x remoteSession) string {
	if x.Mode == "" { // a Claude Code session, not one the host holds
		return ago(x.UpdatedAt)
	}
	state := i18n.T("not running", "未运行")
	if x.Online {
		state = statusText(x.Status)
	}
	if x.Attached > 0 {
		state += " · " + i18n.F("%d terminal(s) attached", "已有 %d 个终端接入", x.Attached)
	}
	return state + " · " + ago(x.UpdatedAt)
}

// confirmAttached asks before attaching to a session another terminal is
// attached to: both show the same screen and type into the same Claude Code.
func confirmAttached(x remoteSession) bool {
	if x.Attached == 0 {
		return true
	}
	fmt.Print(i18n.F("“%s” already has %d terminal(s) attached: they share the screen and type into the same Claude Code. Attach anyway? [y/N] ",
		"「%s」已有 %d 个终端接入：画面同步，输入会进入同一个 Claude Code。仍然接入？[y/N] ", x.Title, x.Attached))
	line, _ := stdin.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes" || answer == "是"
}

func (c *client) pick(query string) (remoteSession, error) {
	list, err := c.terminals()
	if err != nil {
		return remoteSession{}, err
	}
	if query != "" {
		var matched []remoteSession
		for _, x := range list {
			if strings.HasPrefix(x.ID, strings.ToLower(query)) || strings.Contains(strings.ToLower(x.Title), strings.ToLower(query)) {
				matched = append(matched, x)
			}
		}
		list = matched
	}
	switch {
	case len(list) == 0 && query != "":
		return remoteSession{}, i18n.Errorf("no terminal session matches %q", "没有匹配 %q 的终端会话", query)
	case len(list) == 0:
		return remoteSession{}, errors.New(i18n.T("no terminal sessions yet; start one in this directory with cc-remote attach --new", "还没有终端会话；用 cc-remote attach --new 在当前目录新建一个"))
	case len(list) == 1:
		return list[0], nil
	}
	return choose(list, nil)
}

// errHostRestart: the stream ended because the host stopped (an update, a
// restart) or the connection dropped; the session comes back with the host.
var errHostRestart = errors.New("host restarting")

// hostWait is how long attach waits for a restarting host.
const hostWait = 3 * time.Minute

// attach connects this terminal to a session. When the host restarts (e.g.
// an update), it waits for the host and attaches again: the host resumes the
// session, so the terminal carries on instead of exiting.
func (c *client) attach(x remoteSession) error {
	for {
		err := c.attachOnce(x)
		if !errors.Is(err, errHostRestart) {
			return err
		}
		fmt.Print(i18n.T("\r\nThe host is restarting (an update?); attaching again when it is back… (Ctrl+C to quit)\r\n", "\r\n宿主正在重启（可能在更新），恢复后自动重新接入…（Ctrl+C 退出）\r\n"))
		deadline := time.Now().Add(hostWait)
		for c.do(http.MethodGet, "/api/info", nil, nil) != nil {
			if time.Now().After(deadline) {
				return i18n.Errorf("the host did not come back within %v; run ccr again once it runs", "宿主 %v 内没有恢复；宿主运行后再执行 ccr", hostWait)
			}
			time.Sleep(time.Second)
		}
	}
}

func (c *client) attachOnce(x remoteSession) error {
	u, err := url.Parse(c.host)
	if err != nil {
		return err
	}
	u.Scheme = map[string]string{"https": "wss"}[u.Scheme]
	if u.Scheme == "" {
		u.Scheme = "ws"
	}
	u.Path = "/api/terminals/" + x.ID + "/stream"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: &http.Transport{Proxy: nil}},
		HTTPHeader: http.Header{"Authorization": {"Bearer " + c.token}},
	})
	if err != nil {
		return i18n.Errorf("cannot attach to the session: %w", "无法接入会话：%w", err)
	}
	conn.SetReadLimit(4 << 20)
	defer conn.CloseNow()

	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) {
		return errors.New(i18n.T("cc-remote attach must run in a terminal", "cc-remote attach 需要在终端里运行"))
	}
	restoreOut := enableVT(os.Stdout)
	defer restoreOut()
	old, err := term.MakeRaw(in)
	if err != nil {
		return err
	}
	detached := false
	defer func() {
		os.Stdout.WriteString(resetModes)
		_ = term.Restore(in, old)
		if detached {
			fmt.Print(i18n.F("\r\nDetached. The session keeps running on the host; run ccr in that directory, or ccr %s, to attach again.\r\n", "\r\n已断开。会话仍在宿主上运行，在该目录运行 ccr，或 ccr %s 重新接入。\r\n", x.ID[:8]))
		}
	}()
	os.Stdout.WriteString("\x1b[2J\x1b[H")

	sendSize := func(cols, rows int) {
		b, _ := json.Marshal(map[string]any{"type": "resize", "cols": cols, "rows": rows})
		_ = conn.Write(ctx, websocket.MessageText, b)
	}
	cols, rows, _ := term.GetSize(out)
	sendSize(cols, rows)
	go func() { // window size changes (polled: works the same on every platform)
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if c2, r2, err := term.GetSize(out); err == nil && (c2 != cols || r2 != rows) {
					cols, rows = c2, r2
					sendSize(cols, rows)
				}
			}
		}
	}()
	// CC_REMOTE_ATTACH_LOG names a file that records keyboard reads (troubleshooting).
	var keyLog *os.File
	if path := os.Getenv("CC_REMOTE_ATTACH_LOG"); path != "" {
		keyLog, _ = os.Create(path)
		defer keyLog.Close()
	}
	keys := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if keyLog != nil {
				fmt.Fprintf(keyLog, "read %d %q %v\n", n, buf[:n], err)
			}
			if err != nil {
				keys <- err
				return
			}
			chunk := buf[:n]
			if i := DetachIndex(chunk); i >= 0 {
				if i > 0 {
					_ = conn.Write(ctx, websocket.MessageBinary, append([]byte(nil), chunk[:i]...))
				}
				keys <- nil
				return
			}
			if err := conn.Write(ctx, websocket.MessageBinary, append([]byte(nil), chunk...)); err != nil {
				keys <- err
				return
			}
			if HasRedraw(chunk) { // Ctrl+L: repaint every cell, clearing leftovers
				_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"redraw"}`))
			}
		}
	}()
	screen := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				screen <- err
				return
			}
			if _, err := os.Stdout.Write(data); err != nil {
				screen <- err
				return
			}
		}
	}()
	select {
	case err := <-keys:
		detached = err == nil
		// Leave at once: the close handshake may wait for the host up to
		// seconds; the host ends the stream on a dropped connection too.
		closed := make(chan struct{})
		go func() { _ = conn.Close(websocket.StatusNormalClosure, "detach"); close(closed) }()
		select {
		case <-closed:
		case <-time.After(500 * time.Millisecond):
		}
		return nil
	case err := <-screen:
		// The host stops (StatusGoingAway) or the connection drops: wait for it.
		if status := websocket.CloseStatus(err); status == websocket.StatusGoingAway || status == -1 && ctx.Err() == nil {
			return errHostRestart
		}
		if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
			os.Stdout.WriteString(resetModes)
			fmt.Print(i18n.T("\r\nThe session has ended.\r\n", "\r\n会话已结束。\r\n"))
			return nil
		}
		return i18n.Errorf("connection lost: %w", "连接断开：%w", err)
	}
}

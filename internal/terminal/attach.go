package terminal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"cc-remote/internal/hostauth"
	"cc-remote/internal/i18n"

	"github.com/coder/websocket"
	"golang.org/x/term"
)

// resetModes undoes terminal modes Claude Code's TUI turns on (bracketed paste,
// focus and mouse reporting, keyboard protocols) so the local terminal works
// normally after detaching.
const resetModes = "\x1b[?1049l\x1b[?2004l\x1b[?1004l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2031l\x1b[?9001l\x1b[<99u\x1b[>4m\x1b[?25h\x1b[0m"

type remoteSession struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Cwd         string `json:"cwd"`
	Mode        string `json:"mode"`
	Provider    string `json:"provider"`
	NativeID    string `json:"claudeSessionId"`
	Status      string `json:"status"`
	Online      bool   `json:"online"`
	ActiveTasks int    `json:"activeTasks,omitempty"`
	// Attached counts the terminals attached to it now.
	Attached int `json:"attached"`
	// UpdatedAt is the last activity (RFC 3339).
	UpdatedAt string `json:"updatedAt"`
	// ProfileID is the saved supplier configuration, offered on a resume.
	ProfileID   string `json:"profileId,omitempty"`
	Model       string `json:"model,omitempty"`
	ProfileName string `json:"profileName,omitempty"`
}

// checkHost refuses a host the token must not go to: the token runs code on
// the host, so it goes only to this machine or over HTTPS.
func checkHost(host string) error {
	if !hostauth.Allowed(strings.TrimRight(host, "/")) {
		return i18n.Errorf("--host must be http://127.0.0.1:PORT (or localhost) or an https:// URL: %s", "--host 必须是 http://127.0.0.1:端口（或 localhost），或 https:// 地址：%s", host)
	}
	return nil
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
	c := &client{host: strings.TrimRight(host, "/"), token: token}
	if err := checkHost(c.host); err != nil {
		return nil, err
	}
	if err := hostauth.Verify(c.http(), c.host, token, dataDir); err != nil {
		if errors.Is(err, hostauth.ErrWrongToken) || errors.Is(err, hostauth.ErrNotHost) {
			return nil, i18n.Errorf("the service at %s is not the host of this data directory (%v); point --data-dir at the host's data directory", "%s 上的服务不是此数据目录的宿主（%v）；用 --data-dir 指定宿主的数据目录", c.host, err)
		}
		return nil, i18n.Errorf("cannot reach the host at %s: %w", "无法连接宿主 %s：%w", c.host, err)
	}
	return c, nil
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (x remoteSession) agent() string {
	if x.Provider == "" {
		return "claude"
	}
	return x.Provider
}

type client struct {
	host, token string
}

// hostHTTP is shared: a client per request would keep every connection of a
// poll (the Herdr bridge asks the host each second) idle in a pool nobody reuses.
var hostHTTP = &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}}

func (c *client) http() *http.Client { return hostHTTP }

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
		var e struct{ Error, Code string }
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return hostError{resp.StatusCode, e.Code, e.Error}
		}
		return hostError{resp.StatusCode, "", i18n.F("the host returned %d", "宿主返回 %d", resp.StatusCode)}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

type supplierProfile struct {
	ID   string            `json:"id"`
	Name string            `json:"name"`
	App  string            `json:"app"`
	Kind string            `json:"kind,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
}

// Creation follows the catalog default. On Codex resume the host checks any
// explicit supplier against the thread's saved configuration.
func (c *client) selectProfile(app, explicit string, interactive, resuming bool) (string, error) {
	if explicit == "@native" || app == "codex" && resuming {
		return explicit, nil
	}
	if explicit == "" && !interactive {
		return "", nil
	}
	var catalog struct {
		Profiles        []supplierProfile `json:"profiles"`
		DefaultProfiles map[string]string `json:"defaultProfiles"`
	}
	if err := c.do("GET", "/api/profiles", nil, &catalog); err != nil {
		var refused hostError
		if errors.As(err, &refused) && refused.status == http.StatusNotFound && explicit == "" {
			return "", nil // host predates supplier profiles
		}
		return "", err
	}
	var list []supplierProfile
	for _, p := range catalog.Profiles {
		if p.App == app {
			list = append(list, p)
			if explicit != "" && (p.ID == explicit || strings.TrimPrefix(p.ID, "ccswitch:") == explicit) {
				return explicit, nil
			}
		}
	}
	if explicit != "" {
		agent := "Claude"
		if app == "codex" {
			agent = "Codex"
		}
		return "", i18n.Errorf("%s supplier configuration %q was not found on the host", "宿主上找不到 %s 供应商配置 %q", agent, explicit)
	}
	defaultID := catalog.DefaultProfiles[app]
	if len(list) == 0 && (app != "codex" || defaultID == "" || defaultID == "@native") {
		return "", nil
	}
	list = append(list, supplierProfile{ID: "@native", Name: i18n.T("Native configuration", "宿主原生配置"), App: app})
	if resuming {
		defaultID = "@keep"
	}
	return c.chooseSupplier(app, list, defaultID)
}

func chooseProfile(list []supplierProfile, defaultID string) (string, error) {
	items := []pickerItem{{id: "", lines: [2]string{"0  " + i18n.T("Default", "默认"), ""}}}
	for i, p := range list {
		items = append(items, pickerItem{id: p.ID, lines: [2]string{fmt.Sprintf("%d  %s", i+1, p.Name), p.ID}, search: p.Name})
	}
	return pickItems(items, "", nil, nil)
}

// start creates a terminal session. In a folder Claude Code does not trust
// yet, it asks first, as Claude Code itself would: the folder's settings can
// run commands (hooks, MCP servers) as soon as Claude Code starts.
func (c *client) start(body map[string]any, cwd string) (remoteSession, error) {
	var x remoteSession
	err := c.do("POST", "/api/sessions", body, &x)
	var refused hostError
	// A host before the code answers 400 with its message only.
	if errors.As(err, &refused) && (refused.code == "trust-required" || refused.status == http.StatusBadRequest && (strings.Contains(refused.message, "trust this folder") || strings.Contains(refused.message, "信任此目录"))) {
		if !confirmTrust(cwd) {
			return remoteSession{}, errors.New(i18n.T("the folder is not trusted; no session was started", "未信任此目录，没有启动会话"))
		}
		body["trustFolder"] = true
		err = c.do("POST", "/api/sessions", body, &x)
	}
	return x, err
}

func confirmTrust(cwd string) bool {
	fmt.Print(i18n.F(`Do you trust the files in this folder?
  %s
Claude Code may read, edit and run files here, and the folder's own settings can run commands (hooks, MCP servers).
Only trust a folder whose code you trust. Trust it? [y/N] `, `是否信任此文件夹中的文件？
  %s
Claude Code 可以读取、修改和执行这里的文件，文件夹自身的设置也可以运行命令（hooks、MCP 服务器）。
只信任你信得过其代码的文件夹。是否信任？[y/N] `, printable(cwd)))
	line, _ := stdin.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes" || answer == "是"
}

// hostError is a request the host refused; code names the reason when the
// host gives one.
type hostError struct {
	status        int
	code, message string
}

func (e hostError) Error() string { return printable(e.message) }

// printable drops control and format characters: titles and paths come from
// transcripts and must not send escape sequences to the local terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

func (c *client) sessions() ([]remoteSession, error) {
	var all []remoteSession
	if err := c.do("GET", "/api/sessions", nil, &all); err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].UpdatedAt > all[j].UpdatedAt })
	return all, nil
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
	return chooseWithReload(list, create, nil)
}

func chooseWithReload(list []remoteSession, create func() (remoteSession, error), reload func() ([]remoteSession, error)) (remoteSession, error) {
	items, def := sessionItems(list, create != nil)
	var refresh pickerRefresh
	if reload != nil {
		refresh = func() ([]pickerItem, error) {
			next, err := reload()
			if err != nil {
				return nil, err
			}
			list = next
			items, _ := sessionItems(list, create != nil)
			return items, nil
		}
	}
	id, err := pickItems(items, def, refresh, nil)
	if err != nil {
		return remoteSession{}, err
	}
	if id == "" && create != nil {
		return create()
	}
	for _, x := range list {
		if x.ID == id {
			return x, nil
		}
	}
	return remoteSession{}, errPickerCancelled
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
	if x.Online || x.Mode != "terminal" {
		state = statusText(x.Status)
	}
	if x.Attached > 0 {
		state += " · " + i18n.F("%d terminal(s) attached", "已有 %d 个终端接入", x.Attached)
	}
	return state + " · " + ago(x.UpdatedAt)
}

// confirmAttached asks before attaching to a session another terminal is
// attached to: both show the same screen and type into the same Agent.
func confirmAttached(x remoteSession) bool {
	if x.Attached == 0 {
		return true
	}
	fmt.Print(i18n.F("“%s” already has %d terminal(s) attached: they share the screen and type into the same Agent. Attach anyway? [y/N] ",
		"「%s」已有 %d 个终端接入：画面同步，输入会进入同一个 Agent。仍然接入？[y/N] ", printable(x.Title), x.Attached))
	line, _ := stdin.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes" || answer == "是"
}

// errHostRestart: the stream ended because the host stopped (an update, a
// restart) or the connection dropped; the session comes back with the host.
var errHostRestart = errors.New("host restarting")

// hostWait is how long attach waits for a restarting host.
const hostWait = 3 * time.Minute

// attach reconnects after a transport failure. Claude sessions restored by a
// restarting host may take longer to become available than its HTTP server.
func (c *client) attach(x remoteSession) error {
	var deadline time.Time // set while waiting for a restarting host
	for {
		err := c.attachOnce(x)
		// A host that just came back may still be resuming its sessions (and
		// starting Claude Code): its stream can fail for a moment. Try again.
		var refused *dialError
		waitingForClaude := x.agent() == "claude" && errors.As(err, &refused) && refused.status == http.StatusConflict
		if !deadline.IsZero() && (retryableDial(err) || waitingForClaude) && time.Now().Before(deadline) {
			time.Sleep(2 * time.Second)
			continue
		}
		if !errors.Is(err, errHostRestart) {
			return err
		}
		fmt.Print(i18n.T("\r\nThe host is restarting (an update?); attaching again when it is back… (Ctrl+C to quit)\r\n", "\r\n宿主正在重启（可能在更新），恢复后自动重新接入…（Ctrl+C 退出）\r\n"))
		deadline = time.Now().Add(hostWait)
		for c.do(http.MethodGet, "/api/info", nil, nil) != nil {
			if time.Now().After(deadline) {
				return i18n.Errorf("the host did not come back within %v; run ccr again once it runs", "宿主 %v 内没有恢复；宿主运行后再执行 ccr", hostWait)
			}
			time.Sleep(time.Second)
		}
	}
}

// dialError is a stream the host refused (status, with its reason) or that
// could not be opened at all (status 0).
type dialError struct {
	status int
	reason string
	err    error
}

func (e *dialError) Error() string {
	if e.reason != "" {
		return i18n.F("cannot attach to the session (%d): %s", "无法接入会话（%d）：%s", e.status, e.reason)
	}
	return i18n.F("cannot attach to the session: %v", "无法接入会话：%v", e.err)
}

func (e *dialError) Unwrap() error { return e.err }

// retryableDial reports whether a refused stream may work in a moment: the
// host failed (5xx) or did not answer, rather than refusing the session.
func retryableDial(err error) bool {
	var d *dialError
	return errors.As(err, &d) && (d.status == 0 || d.status >= 500)
}

// dialStream opens the session's terminal stream.
func (c *client) dialStream(ctx context.Context, id string) (*websocket.Conn, error) {
	u, err := url.Parse(c.host)
	if err != nil {
		return nil, err
	}
	u.Scheme = map[string]string{"https": "wss"}[u.Scheme]
	if u.Scheme == "" {
		u.Scheme = "ws"
	}
	u.Path = "/api/terminals/" + id + "/stream"
	u.RawQuery = "attach=1"
	conn, resp, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: &http.Transport{Proxy: nil}},
		HTTPHeader: http.Header{"Authorization": {"Bearer " + c.token}},
	})
	if err == nil {
		return conn, nil
	}
	d := &dialError{err: err}
	if resp != nil {
		d.status = resp.StatusCode
		if resp.Body != nil {
			var body struct {
				Error string `json:"error"`
			}
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			if json.Unmarshal(raw, &body) == nil {
				d.reason = body.Error
			}
		}
	}
	return nil, d
}

func (c *client) attachOnce(x remoteSession) error {
	// Registered first so it runs last: the stream is closed and the terminal
	// restored before the pane is released in Herdr (which may take a moment).
	stopHerdr := func() {}
	defer func() { stopHerdr() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, err := c.dialStream(ctx, x.ID)
	if err != nil {
		return err
	}
	conn.SetReadLimit(4 << 20)
	defer conn.CloseNow()

	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) {
		return errors.New(i18n.T("ccr session attach must run in a terminal", "ccr session attach 需要在终端里运行"))
	}
	restoreOut := enableVT(os.Stdout)
	defer restoreOut()
	old, err := term.MakeRaw(in)
	if err != nil {
		return err
	}
	var screenOut io.Writer = os.Stdout
	var links *linkWriter
	if hostauth.Local(c.host) && hyperlinks() { // file paths open with a click
		links = newLinkWriter(os.Stdout)
		screenOut = links
	}
	osc := newOSCStatus(os.Getenv, os.Stdout) // in an hc terminal: tell it the session's state
	if osc != nil {
		screenOut = osc.wrap(screenOut)
	}
	if x.agent() == "claude" {
		stopHerdr = c.startHerdr(x, osc)
	}
	detached := false
	defer func() {
		if links != nil {
			links.close()
		}
		os.Stdout.WriteString(resetModes)
		_ = term.Restore(in, old)
		if detached {
			fmt.Print(i18n.F("\r\nDetached. The session keeps running on the host; run ccr session attach %s to attach again.\r\n", "\r\n已断开。会话仍在宿主上运行，运行 ccr session attach %s 重新接入。\r\n", x.ID))
		}
	}()
	os.Stdout.WriteString("\x1b[2J\x1b[H")

	sendSize := func(cols, rows int) {
		b, _ := json.Marshal(map[string]any{"type": "resize", "cols": cols, "rows": rows})
		_ = conn.Write(ctx, websocket.MessageText, b)
	}
	cols, rows, _ := term.GetSize(out)
	sendSize(cols, rows)
	var clicks *clicker
	if hostauth.Local(c.host) && os.Getenv("CC_REMOTE_HYPERLINKS") != "0" { // a click on a file's path opens it
		clicks = newClicker(cols, rows, x.Cwd, openFile)
	}
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
					if clicks != nil {
						clicks.resize(cols, rows)
					}
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
			if clicks != nil {
				if chunk = clicks.filter(chunk); len(chunk) == 0 {
					continue
				}
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
			if clicks != nil {
				clicks.observe(data)
			}
			if _, err := screenOut.Write(data); err != nil {
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
			if links != nil {
				links.close() // the last of the screen before the notice
			}
			os.Stdout.WriteString(resetModes)
			fmt.Print(i18n.T("\r\nThe session has ended.\r\n", "\r\n会话已结束。\r\n"))
			return nil
		}
		return i18n.Errorf("connection lost: %w", "连接断开：%w", err)
	}
}

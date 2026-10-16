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
	"strconv"
	"strings"
	"time"

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
}

// Attach is `cc-remote attach`: it connects this terminal to a terminal
// session the host holds.
func Attach(args []string, defaultDataDir string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	host := fs.String("host", "http://127.0.0.1:8787", "cc-remote host URL")
	dataDir := fs.String("data-dir", defaultDataDir, "cc-remote data directory (access token)")
	create := fs.Bool("new", false, "start a new terminal session in the current directory")
	title := fs.String("title", "", "title of a new session (default: directory name)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: cc-remote attach [--new [--title T]] [session id prefix or title]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	token := strings.TrimSpace(os.Getenv("CC_REMOTE_TOKEN"))
	if token == "" {
		b, err := os.ReadFile(filepath.Join(*dataDir, "access-token"))
		if err != nil {
			return fmt.Errorf("读取访问令牌失败（%v）；用 --data-dir 指定宿主的数据目录", err)
		}
		token = strings.TrimSpace(string(b))
	}
	api := &client{host: strings.TrimRight(*host, "/"), token: token}
	var target remoteSession
	var err error
	if *create {
		target, err = api.create(*title)
	} else {
		target, err = api.pick(strings.Join(fs.Args(), " "))
	}
	if err != nil {
		return err
	}
	return api.attach(target)
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
		return fmt.Errorf("无法连接宿主 %s：%w", c.host, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("宿主返回 %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *client) create(title string) (remoteSession, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return remoteSession{}, err
	}
	if title == "" {
		title = filepath.Base(cwd)
	}
	var x remoteSession
	// Running attach --new in a directory is the user's decision to trust it.
	err = c.do("POST", "/api/sessions", map[string]any{"title": title, "cwd": cwd, "mode": "terminal", "trustFolder": true}, &x)
	return x, err
}

func (c *client) pick(query string) (remoteSession, error) {
	var all []remoteSession
	if err := c.do("GET", "/api/sessions", nil, &all); err != nil {
		return remoteSession{}, err
	}
	var list []remoteSession
	for _, x := range all {
		if x.Mode == "terminal" {
			list = append(list, x)
		}
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
		return remoteSession{}, fmt.Errorf("没有匹配 %q 的终端会话", query)
	case len(list) == 0:
		return remoteSession{}, errors.New("还没有终端会话；用 cc-remote attach --new 在当前目录新建一个")
	case len(list) == 1:
		return list[0], nil
	}
	for i, x := range list {
		state := "未运行"
		if x.Online {
			state = x.Status
		}
		fmt.Printf("%2d. %s  [%s]  %s  (%s)\n", i+1, x.Title, state, x.Cwd, x.ID[:8])
	}
	fmt.Print("选择会话编号：")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(list) {
		return remoteSession{}, errors.New("无效的选择")
	}
	return list[n-1], nil
}

func (c *client) attach(x remoteSession) error {
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
		return fmt.Errorf("无法接入会话：%w", err)
	}
	conn.SetReadLimit(4 << 20)
	defer conn.CloseNow()

	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) {
		return errors.New("cc-remote attach 需要在终端里运行")
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
			fmt.Printf("\r\n已断开。会话仍在宿主上运行，用 cc-remote attach %s 重新接入。\r\n", x.ID[:8])
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
		if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
			os.Stdout.WriteString(resetModes)
			fmt.Print("\r\n会话已结束。\r\n")
			return nil
		}
		return fmt.Errorf("连接断开：%w", err)
	}
}

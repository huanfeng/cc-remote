package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"cc-remote/internal/hostauth"
	"cc-remote/internal/i18n"

	"github.com/coder/websocket"
	"golang.org/x/term"
)

const (
	projectShellAPI                             = "/api/project-shells"
	projectShellExitStatus websocket.StatusCode = 4001
)

type cliProjectShell struct {
	ID      string   `json:"id"`
	Cwd     string   `json:"cwd"`
	Aliases []string `json:"aliases"`
	State   string   `json:"state"`
	RunID   string   `json:"runId"`
}

func (s cliProjectShell) runPath() string {
	return projectShellAPI + "/" + url.PathEscape(s.ID) + "/runs/" + url.PathEscape(s.RunID)
}

type projectShellCLIOptions struct {
	host, dataDir, cwd string
	command, query     string
}

func parseProjectShellCLI(args []string, defaultDataDir string) (projectShellCLIOptions, error) {
	o := projectShellCLIOptions{command: "open"}
	fs := flag.NewFlagSet("shell", flag.ContinueOnError)
	fs.StringVar(&o.host, "host", "http://127.0.0.1:8787", i18n.T("cc-remote host URL", "cc-remote 宿主 URL"))
	fs.StringVar(&o.dataDir, "data-dir", defaultDataDir, i18n.T("cc-remote data directory (access token)", "cc-remote 数据目录（访问令牌所在）"))
	fs.StringVar(&o.cwd, "cwd", "", i18n.T("directory on the host (default: current directory)", "宿主上的目录（默认当前目录）"))
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), i18n.T(`Usage: ccr shell [--cwd DIRECTORY]
       ccr shell list
       ccr shell attach [ID]
       ccr shell stop [ID]

Open or attach to the project's default shell. Ctrl+] detaches and leaves
it running. Type exit in the shell, or use ccr shell stop, to end it.
Omit ID to select the shell in the current directory.`, `用法：ccr shell [--cwd 目录]
      ccr shell list
      ccr shell attach [ID]
      ccr shell stop [ID]

打开或接入项目的默认 Shell。Ctrl+] 断开，Shell 在宿主上继续运行。
在 Shell 中输入 exit，或运行 ccr shell stop，可结束进程。
省略 ID 时选择当前目录的 Shell。`))
		fs.PrintDefaults()
	}
	var flags, words []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			words = append(words, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			words = append(words, arg)
			continue
		}
		flags = append(flags, arg)
		name, _, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if fs.Lookup(name) != nil && !inline && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	if err := fs.Parse(flags); err != nil {
		return o, err
	}
	if len(words) == 0 {
		return o, nil
	}
	switch words[0] {
	case "list", "ls":
		o.command = "list"
	case "attach":
		o.command = "attach"
	case "stop", "exit":
		o.command = "stop"
	default:
		return o, i18n.Errorf("unknown shell command %q; see ccr shell --help", "未知 Shell 子命令 %q，请查看 ccr shell --help", words[0])
	}
	if len(words) > 2 || (o.command == "list" && len(words) > 1) {
		return o, errors.New(i18n.T("too many shell command arguments", "Shell 命令参数过多"))
	}
	if len(words) == 2 {
		o.query = words[1]
	}
	return o, nil
}

// ProjectShellCLI operates on the same project shells shown in the Web app.
func ProjectShellCLI(args []string, defaultDataDir string, ensureHost ...func(dataDir, host string) error) error {
	o, err := parseProjectShellCLI(args, defaultDataDir)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if (o.command == "open" || o.command == "attach") && (!term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd()))) {
		return errors.New(i18n.T("opening a project shell requires an interactive terminal", "打开项目终端需要交互式终端"))
	}
	if err := checkHost(o.host); err != nil {
		return err
	}
	if len(ensureHost) > 0 && ensureHost[0] != nil {
		if err := ensureHost[0](o.dataDir, o.host); err != nil {
			return err
		}
	}
	api, err := newClient(o.host, o.dataDir)
	if err != nil {
		return err
	}
	if o.command != "list" && o.query == "" {
		if o.cwd == "" {
			o.cwd, err = os.Getwd()
		} else if hostauth.Local(api.host) && !windowsShellPath(o.cwd) {
			o.cwd, err = filepath.Abs(o.cwd)
		}
		if err != nil {
			return err
		}
	}
	var shell cliProjectShell
	if o.command == "open" {
		body := struct {
			Cwd     string `json:"cwd"`
			Restart bool   `json:"restart"`
		}{o.cwd, true}
		if err = api.do(http.MethodPost, projectShellAPI, body, &shell); err != nil {
			return err
		}
	} else {
		var shells []cliProjectShell
		endpoint := projectShellAPI
		if o.command != "list" && o.query == "" {
			endpoint += "?" + url.Values{"cwd": {o.cwd}}.Encode()
		}
		if err = api.do(http.MethodGet, endpoint, nil, &shells); err != nil {
			return err
		}
		if o.command == "list" {
			return printProjectShells(os.Stdout, shells)
		}
		if o.query == "" {
			// Hosts that predate cwd filtering can return an unfiltered list.
			// Restrict the selection before sending input or ending a process.
			shells = slices.DeleteFunc(shells, func(s cliProjectShell) bool {
				return !shellPathSame(s.Cwd, o.cwd) && !slices.ContainsFunc(s.Aliases, func(alias string) bool { return shellPathSame(alias, o.cwd) })
			})
		}
		shell, err = pickProjectShell(shells, o.query)
		if err != nil {
			return err
		}
	}
	if shell.State != "running" || shell.RunID == "" {
		return errors.New(i18n.T("this project shell is not running; use ccr shell --cwd to start it", "此项目终端未运行，请用 ccr shell --cwd 启动"))
	}
	if o.command == "stop" {
		if err := api.do(http.MethodDelete, shell.runPath(), nil, nil); err != nil {
			return err
		}
		_, err := fmt.Fprintln(os.Stdout, i18n.F("Stopped project shell %s", "已结束项目终端 %s", shell.ID))
		return err
	}
	return api.attachProjectShell(shell)
}

func printProjectShells(out io.Writer, shells []cliProjectShell) error {
	if len(shells) == 0 {
		_, err := fmt.Fprintln(out, i18n.T("No project shells", "没有项目终端"))
		return err
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, i18n.T("ID\tSTATE\tDIRECTORY", "ID\t状态\t目录"))
	for _, s := range shells {
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.ID, s.State, s.Cwd)
	}
	return w.Flush()
}

func windowsShellPath(p string) bool {
	return strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") || (len(p) >= 3 && p[1] == ':' && (p[2] == '/' || p[2] == '\\'))
}

func shellPathSame(a, b string) bool {
	if windowsShellPath(a) && windowsShellPath(b) {
		return strings.EqualFold(path.Clean(strings.ReplaceAll(a, `\`, "/")), path.Clean(strings.ReplaceAll(b, `\`, "/")))
	}
	return path.Clean(a) == path.Clean(b)
}

func pickProjectShell(shells []cliProjectShell, query string) (cliProjectShell, error) {
	var picks []cliProjectShell
	prefix := strings.ToUpper(query)
	for _, s := range shells {
		if query != "" && strings.EqualFold(s.ID, query) {
			return s, nil
		}
		if strings.HasPrefix(strings.ToUpper(s.ID), prefix) {
			picks = append(picks, s)
		}
	}
	if len(picks) > 1 {
		return cliProjectShell{}, errors.New(i18n.T("more than one project shell matches; use its full ID from ccr shell list", "匹配到多个项目终端，请使用 ccr shell list 中的完整 ID"))
	}
	if len(picks) == 1 {
		return picks[0], nil
	}
	return cliProjectShell{}, errors.New(i18n.T("project shell not found; see ccr shell list", "找不到项目终端，请查看 ccr shell list"))
}

func (c *client) dialProjectShell(ctx context.Context, shell cliProjectShell) (*websocket.Conn, error) {
	var reply struct {
		Ticket string `json:"ticket"`
	}
	if err := c.do(http.MethodPost, shell.runPath()+"/ticket", nil, &reply); err != nil {
		return nil, err
	}
	u, err := url.Parse(c.host + shell.runPath() + "/stream")
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.RawQuery = url.Values{"ticket": {reply.Ticket}, "after": {"0"}}.Encode()
	httpClient := *c.http()
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	conn, resp, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: &httpClient})
	if err != nil {
		if resp != nil {
			return nil, i18n.Errorf("could not attach to the project shell (HTTP %d)", "无法接入项目终端（HTTP %d）", resp.StatusCode)
		}
		// websocket.Dial errors may include the URL with its ticket.
		return nil, errors.New(i18n.T("the project shell connection failed", "项目终端连接失败"))
	}
	return conn, nil
}

type projectShellCLIFrame struct {
	Type string `json:"type"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
	Held bool   `json:"held,omitempty"`
}

const shellCLIResetModes = resetModes + "\x1b[?1049l"

type shellCLIInput struct {
	data   []byte
	detach bool
	err    error
}

func shellCLIReadInput(ctx context.Context, stdin io.Reader, input chan<- shellCLIInput) {
	buf := make([]byte, 4096)
	for {
		n, err := stdin.Read(buf)
		data := buf[:n]
		i := DetachIndex(data)
		item := shellCLIInput{detach: i >= 0, err: err}
		if item.detach {
			data = data[:i]
		}
		item.data = append([]byte(nil), data...)
		select {
		case input <- item:
		case <-ctx.Done():
			return
		}
		if item.detach || err != nil {
			return
		}
	}
}

func (c *client) attachProjectShell(shell cliProjectShell) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	dialCtx, endDial := context.WithTimeout(ctx, 15*time.Second)
	conn, err := c.dialProjectShell(dialCtx, shell)
	endDial()
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20) // PTY chunks can reach 32 KiB before their sequence prefix.
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	restoreOut := enableVT(os.Stdout)
	defer restoreOut()
	old, err := term.MakeRaw(in)
	if err != nil {
		return err
	}
	detached := false
	defer func() {
		fmt.Fprint(os.Stdout, shellCLIResetModes)
		_ = term.Restore(in, old)
		if detached {
			fmt.Fprint(os.Stdout, i18n.F("\r\nDetached. Reattach with ccr shell attach %s.\r\n", "\r\n已断开。使用 ccr shell attach %s 重新接入。\r\n", shell.ID))
		}
	}()
	if _, err = io.WriteString(os.Stdout, "\x1b[2J\x1b[H"); err != nil {
		return err
	}

	input := make(chan shellCLIInput)
	go shellCLIReadInput(ctx, os.Stdin, input)
	type socketMessage struct {
		kind websocket.MessageType
		data []byte
		err  error
	}
	messages := make(chan socketMessage)
	go func() {
		for {
			kind, data, err := conn.Read(ctx)
			select {
			case messages <- socketMessage{kind, data, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	send := func(kind websocket.MessageType, data []byte) error {
		writeCtx, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		return conn.Write(writeCtx, kind, data)
	}
	cols, rows := 0, 0
	resize := func() error {
		c, r, err := term.GetSize(out)
		if err != nil || c <= 0 || r <= 0 || (c == cols && r == rows) {
			return nil
		}
		cols, rows = c, r
		data, _ := json.Marshal(projectShellCLIFrame{Type: "resize", Cols: c, Rows: r})
		return send(websocket.MessageText, data)
	}
	resizeTick := time.NewTicker(250 * time.Millisecond)
	defer resizeTick.Stop()
	ready, controlling, requestedControl, redraw := false, false, false, false
	for {
		select {
		case <-ctx.Done():
			detached = true
			return nil
		case msg := <-messages:
			if msg.err != nil {
				if websocket.CloseStatus(msg.err) == projectShellExitStatus || ctx.Err() != nil {
					return nil
				}
				return i18n.Errorf("project shell disconnected: %w; reattach with ccr shell attach %s", "项目终端连接断开：%w；使用 ccr shell attach %s 重新接入", msg.err, shell.ID)
			}
			if msg.kind == websocket.MessageBinary {
				if len(msg.data) >= 8 {
					if _, err := os.Stdout.Write(msg.data[8:]); err != nil {
						return err
					}
				}
				continue
			}
			var frame projectShellCLIFrame
			if json.Unmarshal(msg.data, &frame) != nil {
				continue
			}
			switch frame.Type {
			case "exit":
				return nil
			case "reset":
				if _, err := io.WriteString(os.Stdout, shellCLIResetModes+"\x1b[2J\x1b[H"); err != nil {
					return err
				}
				redraw = true
			case "ready":
				ready = true
			case "control":
				controlling = frame.Held
				if !requestedControl && !controlling {
					if err := send(websocket.MessageText, []byte(`{"type":"take"}`)); err != nil {
						return err
					}
				}
				requestedControl = true
				cols, rows = 0, 0
				if ready && controlling {
					if err := resize(); err != nil {
						return err
					}
					if redraw {
						if err := send(websocket.MessageText, []byte(`{"type":"redraw"}`)); err != nil {
							return err
						}
						redraw = false
					}
				}
			}
		case <-resizeTick.C:
			if ready && controlling {
				if err := resize(); err != nil {
					return err
				}
			}
		case item := <-input:
			if ready && controlling && len(item.data) != 0 {
				if err := send(websocket.MessageBinary, item.data); err != nil {
					return err
				}
			}
			if item.detach || errors.Is(item.err, io.EOF) {
				detached = true
				return nil
			}
			if item.err != nil {
				return item.err
			}
		}
	}
}

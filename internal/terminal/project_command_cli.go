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
	"strings"

	"cc-remote/internal/i18n"

	"github.com/coder/websocket"
)

// ProjectCommandExitError preserves the command's exit status for the caller.
type ProjectCommandExitError struct{ Code int }

func (e ProjectCommandExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }
func (e ProjectCommandExitError) ExitCode() int { return e.Code }

type projectCommandRequest struct {
	Cwd  string   `json:"cwd"`
	Argv []string `json:"argv"`
}

type projectCommandReply struct {
	Type   string `json:"type"`
	Detail string `json:"detail"`
	Code   *int   `json:"code"`
}

// ProjectCommandCLI runs a command on the host, forwarding each of the three
// standard streams and returning the host command's exit status.
func ProjectCommandCLI(args []string, defaultDataDir string, ensureHost ...func(dataDir, host string) error) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	host := fs.String("host", "http://127.0.0.1:8787", i18n.T("cc-remote host URL", "cc-remote 宿主 URL"))
	dataDir := fs.String("data-dir", defaultDataDir, i18n.T("cc-remote data directory (access token)", "cc-remote 数据目录（访问令牌所在）"))
	cwd := fs.String("cwd", "", i18n.T("absolute working directory on the host (default: this directory)", "宿主上的绝对工作目录（默认取当前目录）"))
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), i18n.T("usage: ccr run [--host URL] [--data-dir DIR] [--cwd HOST_DIR] [--] COMMAND [ARGS...]", "用法：ccr run [--host URL] [--data-dir DIR] [--cwd 宿主目录] [--] 命令 [参数…]"))
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	argv := fs.Args()
	if len(argv) == 0 || argv[0] == "" {
		return errors.New(i18n.T("ccr run requires a command", "ccr run 需要指定命令"))
	}
	if *cwd == "" {
		var err error
		*cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	if err := checkHost(*host); err != nil {
		return err
	}
	if len(ensureHost) > 0 && ensureHost[0] != nil {
		if err := ensureHost[0](*dataDir, *host); err != nil {
			return err
		}
	}
	api, err := newClient(*host, *dataDir)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err = api.projectCommand(ctx, projectCommandRequest{Cwd: *cwd, Argv: argv}, os.Stdin, os.Stdout, os.Stderr)
	var exit ProjectCommandExitError
	if err != nil && ctx.Err() == context.Canceled && !errors.As(err, &exit) {
		return ProjectCommandExitError{Code: 130}
	}
	return err
}

func (c *client) projectCommand(ctx context.Context, req projectCommandRequest, stdin io.Reader, stdout, stderr io.Writer) error {
	endpoint, err := url.Parse(c.host + "/api/project-shells/run")
	if err != nil {
		return err
	}
	endpoint.Scheme = "ws" + strings.TrimPrefix(endpoint.Scheme, "http")
	transport := *c.http()
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	conn, resp, err := websocket.Dial(ctx, endpoint.String(), &websocket.DialOptions{
		HTTPClient: &transport,
		HTTPHeader: http.Header{"Authorization": {"Bearer " + c.token}},
	})
	if err != nil {
		if resp != nil && resp.Body != nil {
			defer resp.Body.Close()
			var body struct{ Error string }
			if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body) == nil && body.Error != "" {
				return hostError{status: resp.StatusCode, message: body.Error}
			}
		}
		return i18n.Errorf("cannot open a command stream: %w", "无法打开命令流：%w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(256 << 10)
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, websocket.MessageText, body); err != nil {
		return i18n.Errorf("could not send the command to the host: %w", "无法将命令发送给宿主：%w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go copyProjectCommandInput(ctx, conn, stdin)
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return i18n.Errorf("the connection ended before the host reported the command's exit status: %w", "宿主返回命令退出状态前连接已结束：%w", err)
		}
		if kind == websocket.MessageBinary {
			if len(data) == 0 || data[0] != 1 && data[0] != 2 {
				return errors.New(i18n.T("invalid command output from the host", "宿主返回了无效的命令输出"))
			}
			writer := stdout
			if data[0] == 2 {
				writer = stderr
			}
			if n, err := writer.Write(data[1:]); err != nil {
				return err
			} else if n != len(data)-1 {
				return io.ErrShortWrite
			}
			continue
		}
		var frame projectCommandReply
		if err := json.Unmarshal(data, &frame); err != nil {
			return i18n.Errorf("invalid command status from the host: %w", "宿主返回了无效的命令状态：%w", err)
		}
		switch {
		case frame.Type == "exit" && frame.Code != nil && *frame.Code >= 0:
			if *frame.Code == 0 {
				return nil
			}
			return ProjectCommandExitError{Code: *frame.Code}
		case frame.Type == "error" && frame.Detail != "":
			return errors.New(printable(frame.Detail))
		default:
			return errors.New(i18n.T("invalid command status from the host", "宿主返回了无效的命令状态"))
		}
	}
}

func copyProjectCommandInput(ctx context.Context, conn *websocket.Conn, stdin io.Reader) {
	buf := make([]byte, 32<<10)
	for ctx.Err() == nil {
		n, err := stdin.Read(buf)
		if n > 0 {
			if writeErr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"stdin-eof"}`))
			return
		}
	}
}

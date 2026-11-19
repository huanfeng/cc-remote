package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"cc-remote/internal/terminal"
	"github.com/coder/websocket"
)

const (
	maxProjectCommands        = 16
	projectCommandFrameLimit  = 256 << 10
	projectCommandWireTimeout = 10 * time.Second
)

type projectCommandRegistry struct {
	mu      sync.Mutex
	active  map[*projectCommandRun]struct{}
	closing bool
}

type projectCommandRun struct {
	context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *projectCommandRegistry) begin(ctx context.Context) *projectCommandRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing || len(r.active) >= maxProjectCommands {
		return nil
	}
	if r.active == nil {
		r.active = make(map[*projectCommandRun]struct{})
	}
	ctx, cancel := context.WithCancel(ctx)
	run := &projectCommandRun{Context: ctx, cancel: cancel, done: make(chan struct{})}
	r.active[run] = struct{}{}
	return run
}

func (r *projectCommandRegistry) end(run *projectCommandRun) {
	run.cancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, run)
	close(run.done)
}

func (r *projectCommandRegistry) shutdown(ctx context.Context) {
	r.mu.Lock()
	r.closing = true
	done := make([]<-chan struct{}, 0, len(r.active))
	for run := range r.active {
		run.cancel()
		done = append(done, run.done)
	}
	r.mu.Unlock()
	for _, ch := range done {
		select {
		case <-ch:
		case <-ctx.Done():
			return
		}
	}
}

type projectCommandRequest struct {
	Cwd  string   `json:"cwd"`
	Argv []string `json:"argv"`
}

type projectCommandFrame struct {
	Type   string `json:"type"`
	Code   *int   `json:"code,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func writeProjectCommandFrame(ctx context.Context, ws *websocket.Conn, frame projectCommandFrame) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, projectCommandWireTimeout)
	defer cancel()
	return ws.Write(ctx, websocket.MessageText, data)
}

const (
	projectCommandStdout byte = 1
	projectCommandStderr byte = 2
)

// projectCommandOutput relays exec's stdout and stderr separately. WebSocket
// serializes writes from the two os/exec pipe readers.
type projectCommandOutput struct {
	ws      *websocket.Conn
	run     *projectCommandRun
	channel byte
}

func (o projectCommandOutput) Write(data []byte) (int, error) {
	frame := make([]byte, len(data)+1)
	frame[0] = o.channel
	copy(frame[1:], data)
	ctx, cancel := context.WithTimeout(o.run, projectCommandWireTimeout)
	defer cancel()
	if err := o.ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
		o.run.cancel()
		return 0, err
	}
	return len(data), nil
}

// runProjectCommand runs one explicit argv on the host. This connection owns
// its command, unlike the persistent project Shell's detachable PTY stream.
func (s *Server) runProjectCommand(w http.ResponseWriter, req *http.Request) {
	if !s.projectShellAvailable(w, req) {
		return
	}
	l := s.lang(req)
	run := s.projectCommands.begin(req.Context())
	if run == nil {
		fail(w, http.StatusServiceUnavailable, l.T("The host is shutting down or has too many running commands", "宿主正在关闭或同时运行的命令过多"))
		return
	}
	defer s.projectCommands.end(run)
	// Handler checks Bearer and Origin before the upgrade.
	ws, err := websocket.Accept(w, req, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(projectCommandFrameLimit)
	failRun := func(detail string) {
		_ = writeProjectCommandFrame(run, ws, projectCommandFrame{Type: "error", Detail: detail})
	}
	startCtx, cancel := context.WithTimeout(run, projectCommandWireTimeout)
	kind, data, err := ws.Read(startCtx)
	cancel()
	if err != nil {
		return
	}
	var request projectCommandRequest
	if kind != websocket.MessageText || json.Unmarshal(data, &request) != nil || len(request.Argv) == 0 || len(request.Argv) > 1024 || request.Argv[0] == "" {
		failRun(l.T("Expected a command and its arguments", "请输入要执行的命令及参数"))
		return
	}
	dir, _, err := projectShellDir(request.Cwd)
	if err != nil {
		failRun(l.T("Working directory is unavailable on this host", "宿主上的工作目录不可用"))
		return
	}
	stdin, input, err := os.Pipe()
	if err != nil {
		failRun(l.T("Could not open command input", "无法打开命令输入"))
		return
	}
	defer stdin.Close()
	defer input.Close()
	stopClosingInput := context.AfterFunc(run, func() { _ = input.Close() })
	defer stopClosingInput()
	cmd := exec.CommandContext(run, request.Argv[0], request.Argv[1:]...)
	cmd.Dir = dir
	cmd.Env = terminal.CleanEnv(cmd.Environ())
	cmd.Stdin = stdin
	cmd.Stdout = projectCommandOutput{ws: ws, run: run, channel: projectCommandStdout}
	cmd.Stderr = projectCommandOutput{ws: ws, run: run, channel: projectCommandStderr}
	cmd.WaitDelay = 3 * time.Second
	configureProjectCommand(cmd)
	if err := cmd.Start(); err != nil {
		failRun(l.T("Could not start command: ", "无法启动命令：") + err.Error())
		return
	}
	_ = stdin.Close()
	go readProjectCommandInput(run, ws, input)
	go pingProjectShell(run, ws, run.cancel)
	err = cmd.Wait() // Wait also drains both output streams before reporting exit.
	if run.Err() != nil {
		return
	}
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = projectCommandExitCode(exitErr)
	} else if err != nil {
		failRun(l.T("Could not finish command: ", "命令无法完成：") + err.Error())
		return
	}
	_ = writeProjectCommandFrame(run, ws, projectCommandFrame{Type: "exit", Code: &code})
}

func readProjectCommandInput(run *projectCommandRun, ws *websocket.Conn, input *os.File) {
	defer run.cancel()
	acceptInput := true
	for {
		kind, data, err := ws.Read(run)
		if err != nil {
			return
		}
		if kind == websocket.MessageBinary {
			if acceptInput {
				if _, err := input.Write(data); err != nil {
					// A program may close stdin early and continue producing output.
					acceptInput = false
					_ = input.Close()
				}
			}
			continue
		}
		var frame projectCommandFrame
		if json.Unmarshal(data, &frame) != nil || frame.Type != "stdin-eof" {
			return
		}
		acceptInput = false
		_ = input.Close()
	}
}

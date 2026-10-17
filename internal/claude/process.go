package claude

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"cc-remote/internal/i18n"
)

// ProcessHooks receive what a long-lived Claude process reports. Callbacks run
// on the process reader goroutine, one at a time and in stream order.
type ProcessHooks struct {
	// Event receives every parsed event; an error stops the process.
	Event func(Event) error
	// Replay receives the user message a turn starts with, as Claude echoes it
	// (--replay-user-messages). Messages that queued up while a turn ran are
	// merged into one turn and echoed together, joined by newlines. Slash
	// commands are echoed after their output.
	Replay func(content string)
	// Result reports the end of a turn: err is nil on success, origin is set
	// for turns Claude started itself (e.g. reporting a background agent).
	Result func(err error, origin bool)
	// Exit reports that the process ended.
	Exit func(error)
}

// Process is a Claude Code session kept running between turns
// ("claude -p --input-format stream-json"). Messages written while a turn runs
// queue inside Claude and start the next turn; Claude merges them.
type Process struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writeMu sync.Mutex
	done    chan struct{}

	mu     sync.Mutex
	tasks  map[string]bool
	closed bool
}

func processArgs(o Options, settings, mcp string) []string {
	return append(commandArgs(o, settings, mcp), "--input-format", "stream-json", "--replay-user-messages")
}

// StartProcess launches Claude for o.SessionID; o.Message is ignored.
func StartProcess(o Options, hooks ProcessHooks) (*Process, error) {
	if !uuidPattern.MatchString(o.SessionID) {
		return nil, errors.New(i18n.T("invalid Claude session UUID", "无效的 Claude 会话 UUID"))
	}
	if o.Executable == "" {
		o.Executable = "claude"
	}
	if o.HostExecutable == "" {
		var err error
		if o.HostExecutable, err = os.Executable(); err != nil {
			return nil, err
		}
	}
	if o.BridgeURL == "" || o.BridgeToken == "" {
		return nil, errors.New("permission bridge is required")
	}
	dir, err := os.MkdirTemp("", "cc-remote-claude-")
	if err != nil {
		return nil, err
	}
	settings, mcp, err := writeConfigs(dir, o)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	cmd := exec.Command(o.Executable, processArgs(o, settings, mcp)...)
	cmd.Dir = o.Cwd
	cmd.Env = runEnvironment(os.Environ(), o)
	configureProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, i18n.Errorf("start Claude Code: %w", "启动 Claude Code 失败：%w", err)
	}
	p := &Process{cmd: cmd, stdin: stdin, done: make(chan struct{}), tasks: map[string]bool{}}
	go func() {
		defer os.RemoveAll(dir)
		readErr := p.read(stdout, hooks)
		if readErr != nil {
			killProcessTree(cmd)
		}
		waitErr := cmd.Wait()
		p.mu.Lock()
		closed := p.closed
		p.mu.Unlock()
		err := readErr
		if err == nil && waitErr != nil && !closed {
			err = i18n.Errorf("Claude Code exited: %w: %s", "Claude Code 已退出：%w：%s", waitErr, strings.TrimSpace(stderr.String()))
		}
		if err == nil && !closed {
			err = errors.New(i18n.T("Claude Code exited unexpectedly", "Claude Code 意外退出"))
		}
		close(p.done)
		if hooks.Exit != nil {
			hooks.Exit(err)
		}
	}()
	return p, nil
}

// streamMeta holds the fields Process needs beyond parseEvent.
type streamMeta struct {
	Type     string          `json:"type"`
	Subtype  string          `json:"subtype"`
	IsReplay bool            `json:"isReplay"`
	Origin   json.RawMessage `json:"origin"`
	TaskID   string          `json:"task_id"`
	Patch    struct {
		Status string `json:"status"`
	} `json:"patch"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func (p *Process) read(stdout io.Reader, hooks ProcessHooks) error {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var meta streamMeta
		if json.Unmarshal(line, &meta) != nil {
			continue // stray non-JSON output does not end a long-lived session
		}
		events, terminal, turnErr := parseEvent(line)
		p.trackTasks(meta)
		if meta.Type == "user" && meta.IsReplay {
			var content string
			if json.Unmarshal(meta.Message.Content, &content) == nil && hooks.Replay != nil {
				hooks.Replay(content)
			}
			continue // already shown as the user's message
		}
		for _, e := range events {
			if hooks.Event != nil {
				if err := hooks.Event(e); err != nil {
					return err
				}
			}
		}
		if terminal && hooks.Result != nil {
			hooks.Result(turnErr, len(meta.Origin) > 0 && string(meta.Origin) != "null")
		}
	}
	return scanner.Err()
}

// trackTasks follows background tasks (agents running after their turn).
func (p *Process) trackTasks(meta streamMeta) {
	if meta.Type != "system" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case meta.Subtype == "task_started" && meta.TaskID != "":
		p.tasks[meta.TaskID] = true
	case meta.Subtype == "task_notification":
		delete(p.tasks, meta.TaskID)
	case meta.Subtype == "task_updated" && meta.Patch.Status != "" && meta.Patch.Status != "running" && meta.Patch.Status != "pending":
		delete(p.tasks, meta.TaskID) // ended (completed, failed, killed) even if no notification follows
	}
}

func (p *Process) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	select {
	case <-p.done:
		return errors.New(i18n.T("Claude Code is not running", "Claude Code 未在运行"))
	default:
	}
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

// Send writes one user message; Claude runs it now or queues it.
func (p *Process) Send(text string) error {
	return p.write(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
}

// Interrupt stops the running turn; the process and its context stay.
func (p *Process) Interrupt() error {
	return p.write(map[string]any{"type": "control_request", "request_id": "interrupt-" + newRequestID(), "request": map[string]any{"subtype": "interrupt"}})
}

// BackgroundTasks counts started background tasks without a final notification.
func (p *Process) BackgroundTasks() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.tasks)
}

// HasTask reports whether this process started task id and it has not ended.
func (p *Process) HasTask(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tasks[id]
}

// Done is closed after the process exited.
func (p *Process) Done() <-chan struct{} { return p.done }

// Close ends the session: stdin is closed so Claude exits after flushing,
// then the process tree is killed if it does not exit promptly.
func (p *Process) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	_ = p.stdin.Close() // not under writeMu: this also fails a write stuck on a full pipe
	select {
	case <-p.done:
	case <-afterCloseGrace():
		killProcessTree(p.cmd)
		<-p.done
	}
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// closeGrace is how long Close waits for Claude to exit after stdin closes.
var closeGrace = 5 * time.Second

func afterCloseGrace() <-chan time.Time { return time.After(closeGrace) }

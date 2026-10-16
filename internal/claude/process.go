package claude

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ProcessHooks receive what a long-lived Claude process reports. Callbacks run
// on the process reader goroutine, one at a time and in stream order.
type ProcessHooks struct {
	// Event receives every parsed event; an error stops the process.
	Event func(Event) error
	// TurnDone reports that the turn of one Send finished (nil on success).
	TurnDone func(error)
	// SelfTurn reports turns Claude starts on its own, e.g. to report a
	// finished background agent: true when one starts, false when it ends.
	SelfTurn func(bool)
	// Exit reports that the process ended; turns still pending will not finish.
	Exit func(error)
}

// Process is a Claude Code session kept running between turns
// ("claude -p --input-format stream-json"). Messages written while a turn runs
// are queued by Claude, which merges consecutive queued messages into one
// turn; callers wanting one result per message send the next after TurnDone.
type Process struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writeMu sync.Mutex
	done    chan struct{}

	mu      sync.Mutex
	pending int  // Sends without their result yet
	self    bool // a turn Claude started on its own is running
	tasks   map[string]bool
	closed  bool
}

func processArgs(o Options, settings, mcp string) []string {
	return append(commandArgs(o, settings, mcp), "--input-format", "stream-json")
}

// StartProcess launches Claude for o.SessionID; o.Message is ignored.
func StartProcess(o Options, hooks ProcessHooks) (*Process, error) {
	if !uuidPattern.MatchString(o.SessionID) {
		return nil, errors.New("invalid Claude session UUID")
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
		return nil, fmt.Errorf("start Claude Code: %w", err)
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
			err = fmt.Errorf("Claude Code exited: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
		}
		if err == nil && !closed {
			err = errors.New("Claude Code exited unexpectedly")
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
		p.track(meta, terminal, hooks)
		for _, e := range events {
			if hooks.Event != nil {
				if err := hooks.Event(e); err != nil {
					return err
				}
			}
		}
		if terminal {
			p.finish(meta, turnErr, hooks)
		}
	}
	return scanner.Err()
}

// track updates background tasks and detects turns Claude starts by itself:
// conversation output while none of our messages is pending.
func (p *Process) track(meta streamMeta, terminal bool, hooks ProcessHooks) {
	p.mu.Lock()
	switch {
	case meta.Type == "system" && meta.Subtype == "task_started" && meta.TaskID != "":
		p.tasks[meta.TaskID] = true
	case meta.Type == "system" && meta.Subtype == "task_notification":
		delete(p.tasks, meta.TaskID)
	}
	start := !terminal && !p.self && p.pending == 0 && (meta.Type == "assistant" || meta.Type == "stream_event" || (meta.Type == "user" && !meta.IsReplay))
	if start {
		p.self = true
	}
	p.mu.Unlock()
	if start && hooks.SelfTurn != nil {
		hooks.SelfTurn(true)
	}
}

// finish attributes a result: one with an origin (or arriving while none of
// our messages is pending) ends a self-started turn, any other ends a Send.
func (p *Process) finish(meta streamMeta, turnErr error, hooks ProcessHooks) {
	p.mu.Lock()
	own := p.pending > 0 && len(meta.Origin) == 0
	ended := false
	if own {
		p.pending--
	} else if p.self {
		p.self = false
		ended = true
	}
	p.mu.Unlock()
	if own && hooks.TurnDone != nil {
		hooks.TurnDone(turnErr)
	}
	if ended && hooks.SelfTurn != nil {
		hooks.SelfTurn(false)
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
		return errors.New("Claude Code is not running")
	default:
	}
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

// Send queues one user message; its TurnDone follows its result.
func (p *Process) Send(text string) error {
	p.mu.Lock()
	p.pending++
	p.mu.Unlock()
	err := p.write(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
	if err != nil {
		p.mu.Lock()
		p.pending--
		p.mu.Unlock()
	}
	return err
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

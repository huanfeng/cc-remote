package codex

import (
	"bufio"
	"cc-remote/internal/claude"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const startupTimeout = 30 * time.Second

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("Codex RPC %d: %s", e.Code, e.Message) }

type rpcClient struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	readOnce  sync.Once
	killOnce  sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	writeGate chan struct{}
	mu        sync.Mutex
	next      uint64
	pending   map[string]chan rpcMessage
	err       error
	closing   bool
	stderr    tailBuffer
	onMessage func(rpcMessage) error
	onExit    func(error)
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > 64*1024 {
		b.data = append([]byte(nil), b.data[len(b.data)-64*1024:]...)
	}
	return len(p), nil
}
func (b *tailBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.data) }

// ResolveExecutable resolves PATH names without invoking a shell. npm's Windows
// wrappers are handled by executableCommand, using node and the installed entry point.
func ResolveExecutable(executable string) (string, error) {
	if executable == "" {
		executable = "codex"
	}
	p, err := exec.LookPath(executable)
	if err != nil {
		return "", err
	}
	return filepath.Abs(p)
}

func executableCommand(executable string) (*exec.Cmd, error) {
	p, err := ResolveExecutable(executable)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(p))
	if ext == ".cmd" || ext == ".bat" || ext == ".ps1" {
		entry := filepath.Join(filepath.Dir(p), "node_modules", "@openai", "codex", "bin", "codex.js")
		if _, err := os.Stat(entry); err != nil {
			return nil, fmt.Errorf("Codex wrapper has no npm entry point: %w", err)
		}
		node := filepath.Join(filepath.Dir(p), "node.exe")
		if _, err := os.Stat(node); err != nil {
			node, err = exec.LookPath("node")
			if err != nil {
				return nil, err
			}
		}
		return exec.Command(node, entry, "app-server", "--listen", "stdio://"), nil
	}
	return exec.Command(p, "app-server", "--listen", "stdio://"), nil
}

func childEnvironment(configDir string) []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "CC_REMOTE_") || key == "CLAUDECODE" || (key == "CODEX_HOME" && configDir != "") {
			continue
		}
		env = append(env, entry)
	}
	if configDir != "" {
		env = append(env, "CODEX_HOME="+configDir)
	}
	return env
}

func startRPC(executable, configDir, cwd string, onMessage func(rpcMessage) error, onExit func(error), profileOptions ...claude.Options) (*rpcClient, error) {
	cmd, err := executableCommand(executable)
	if err != nil {
		return nil, err
	}
	cmd.Dir = cwd
	cmd.Env = childEnvironment(configDir)
	if len(profileOptions) > 0 {
		o := profileOptions[0]
		cmd.Env = claude.ProfileEnvironment(cmd.Env, o.ProfileEnv)
		keys := make([]string, 0, len(o.ConfigOverrides))
		for k := range o.ConfigOverrides {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			value, e := tomlValue(o.ConfigOverrides[k])
			if e != nil {
				return nil, e
			}
			cmd.Args = append(cmd.Args, "-c", k+"="+value)
		}
	}
	configureProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &rpcClient{cmd: cmd, stdin: stdin, ctx: ctx, cancel: cancel, done: make(chan struct{}), writeGate: make(chan struct{}, 1), pending: map[string]chan rpcMessage{}, onMessage: onMessage, onExit: onExit}
	cmd.Stderr = &r.stderr
	if err = cmd.Start(); err != nil {
		cancel()
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("start Codex: %w", err)
	}
	r.stdout = stdout
	return r, nil
}

func (r *rpcClient) read(stdout io.Reader) {
	s := bufio.NewScanner(stdout)
	s.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var readErr error
	for s.Scan() {
		var m rpcMessage
		if err := json.Unmarshal(s.Bytes(), &m); err != nil {
			readErr = fmt.Errorf("invalid Codex protocol message: %w", err)
			break
		}
		if m.Method == "" && len(m.ID) > 0 {
			r.mu.Lock()
			ch := r.pending[string(m.ID)]
			delete(r.pending, string(m.ID))
			r.mu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		if r.onMessage != nil {
			if err := r.onMessage(m); err != nil {
				readErr = err
				break
			}
		} else if len(m.ID) > 0 {
			if err := r.reject(m.ID); err != nil {
				readErr = err
				break
			}
		}
	}
	if readErr == nil {
		readErr = s.Err()
	}
	// EOF can arrive while descendants retain stderr; terminate the process tree
	// before Wait so neither shutdown nor a failed handshake can hang forever.
	r.cancel()
	r.kill()
	waitErr := r.cmd.Wait()
	r.mu.Lock()
	if !r.closing {
		if readErr != nil {
			r.err = readErr
		} else if waitErr != nil {
			r.err = fmt.Errorf("Codex exited: %w", waitErr)
		} else {
			r.err = errors.New("Codex exited unexpectedly")
		}
		if detail := strings.TrimSpace(r.stderr.String()); detail != "" {
			r.err = fmt.Errorf("%w: %s", r.err, detail)
		}
	}
	err := r.err
	r.mu.Unlock()
	close(r.done)
	if r.onExit != nil {
		r.onExit(err)
	}
}

func (r *rpcClient) write(v any) error {
	return r.writeContext(r.ctx, v)
}

func (r *rpcClient) writeContext(ctx context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case r.writeGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-r.ctx.Done():
		return errors.New("Codex is not running")
	}
	defer func() { <-r.writeGate }()
	select {
	case <-r.ctx.Done():
		return errors.New("Codex is not running")
	default:
	}
	// A pipe write can block before call reaches its response select. Closing
	// stdin on cancellation releases that write; the watchdog also reaps the
	// ambiguous child, and never waits for the writer lock itself.
	writeDone := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-writeDone:
			return
		case <-ctx.Done():
		case <-r.ctx.Done():
		}
		_ = r.stdin.Close()
		r.kill()
	}()
	_, err = r.stdin.Write(append(b, '\n'))
	close(writeDone)
	<-watchDone
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
func (r *rpcClient) reject(id json.RawMessage) error {
	return r.write(map[string]any{"id": id, "error": map[string]any{"code": -32601, "message": "Unsupported client request"}})
}
func (r *rpcClient) call(ctx context.Context, method string, params any, result any) error {
	r.mu.Lock()
	r.next++
	id := fmt.Sprintf("cc-%d", r.next)
	key := fmt.Sprintf("%q", id)
	ch := make(chan rpcMessage, 1)
	r.pending[key] = ch
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, key); r.mu.Unlock() }()
	if err := r.writeContext(ctx, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if result != nil {
			return json.Unmarshal(m.Result, result)
		}
		return nil
	case <-ctx.Done():
		if r.ctx.Err() != nil {
			<-r.done
			r.mu.Lock()
			err := r.err
			r.mu.Unlock()
			if err != nil {
				return err
			}
		}
		return ctx.Err()
	case <-r.done:
		r.mu.Lock()
		err := r.err
		r.mu.Unlock()
		if err == nil {
			err = errors.New("Codex closed")
		}
		return err
	}
}
func (r *rpcClient) initialize(ctx context.Context) error {
	r.readOnce.Do(func() { go r.read(r.stdout) })
	if err := r.call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "cc_remote", "title": "CC Remote", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}}, nil); err != nil {
		return err
	}
	return r.write(map[string]any{"method": "initialized"})
}
func (r *rpcClient) close() {
	r.readOnce.Do(func() { go r.read(r.stdout) })
	r.mu.Lock()
	r.closing = true
	r.mu.Unlock()
	r.cancel()
	// Kill while the root is still alive: closing stdin first lets npm's
	// launcher exit before Windows taskkill can enumerate its descendants.
	r.kill()
	_ = r.stdin.Close()
	select {
	case <-r.done:
		return
	case <-time.After(time.Second):
		r.kill()
		<-r.done
	}
}

func (r *rpcClient) kill() { r.killOnce.Do(func() { killProcessTree(r.cmd) }) }

// JSON profiles are data, never shell syntax. Each override is one argv value.
func tomlValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		b, _ := json.Marshal(x)
		return string(b), nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case float64:
		return fmt.Sprintf("%v", x), nil
	case []any:
		parts := []string{}
		for _, e := range x {
			t, err := tomlValue(e)
			if err != nil {
				return "", err
			}
			parts = append(parts, t)
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := []string{}
		for _, k := range keys {
			t, e := tomlValue(x[k])
			if e != nil {
				return "", e
			}
			key, _ := json.Marshal(k)
			parts = append(parts, string(key)+"="+t)
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	default:
		return "", errors.New("unsupported Codex configuration value")
	}
}

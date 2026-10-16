// Package terminal runs an interactive Claude Code in a pseudo-terminal that
// the host owns. Terminal front ends (cc-remote attach, the web terminal)
// subscribe to its output and send keystrokes; the web sends whole messages
// as pasted text. Front ends come and go without touching the process, so
// long-running work such as background agents survives switching between them.
package terminal

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

// Options start a runner.
type Options struct {
	Executable string   // Claude Code executable (looked up in PATH when not absolute)
	Args       []string // arguments after the executable
	Dir        string
	Env        []string // full environment; CleanEnv it first
	Cols, Rows int
	// TrustFolder confirms Claude's folder trust dialog (the user confirmed
	// trusting the directory when creating the session).
	TrustFolder bool
	// AwaitStart holds input until MarkStarted (the SessionStart hook): Claude
	// Code can pause output for seconds while its startup hooks run, before
	// it accepts input.
	AwaitStart bool
}

// Runner is one interactive Claude Code in a pseudo-terminal.
type Runner struct {
	pty  pty.Pty
	cmd  *pty.Cmd
	done chan struct{}
	err  error

	mu        sync.Mutex
	subs      map[*Sub]bool
	cols      int
	rows      int
	lastKey   time.Time // last keystroke of a terminal front end
	lastOut   time.Time
	recent    []byte // tail of the output, ANSI stripped, for dialog detection
	trust     bool
	trustDone bool
	// trustSettled: the trust dialog was answered (or left to the user).
	trustSettled bool
	outSeen      bool
	awaitStart   bool
	started      bool
	launched     time.Time
	exited       bool

	pasteMu sync.Mutex // one web message at a time
	ready   chan struct{}
	modes   *modeTracker
	scr     *screen // screen text, for the activity line
}

// Sub is a front end's view of the output.
type Sub struct {
	C       chan []byte
	dropped bool
}

const (
	subBuffer   = 256       // chunks queued per front end before it is dropped
	recentBytes = 16 * 1024 // output tail kept for dialog detection
	typingPause = 2 * time.Second
	readyQuiet  = 1500 * time.Millisecond // output pause after start that means the TUI is up
	readyWait   = 45 * time.Second        // longest a web message waits for it
	startWait   = 20 * time.Second        // AwaitStart gives up on the hook after this
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;?<>=]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[PX^_][^\x1b]*\x1b\\|\x1b[@-Z\\-_]`)

// sessionMarkers are variables a parent Claude Code session sets for its
// subprocesses. Inherited, they make the child a "child session" that keeps
// no transcript, so a host started from inside Claude Code must drop them.
var sessionMarkers = []string{"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ID", "CLAUDE_PID", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_SESSION_ATTENDED", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN"}

// CleanEnv returns env without the markers of a parent Claude Code session.
func CleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, m := range sessionMarkers {
			if strings.EqualFold(name, m) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// Start launches Claude Code in a new pseudo-terminal.
func Start(o Options) (*Runner, error) {
	exe, err := exec.LookPath(o.Executable) // go-pty resolves bare names against Dir on Windows
	if err != nil {
		return nil, err
	}
	args := o.Args
	if runtime.GOOS == "windows" && (strings.HasSuffix(strings.ToLower(exe), ".cmd") || strings.HasSuffix(strings.ToLower(exe), ".bat")) {
		args = append([]string{"/c", exe}, args...)
		exe = os.Getenv("ComSpec")
	}
	if o.Cols <= 0 || o.Rows <= 0 {
		o.Cols, o.Rows = 120, 40
	}
	p, err := pty.New()
	if err != nil {
		return nil, err
	}
	if err := p.Resize(o.Cols, o.Rows); err != nil {
		p.Close()
		return nil, err
	}
	c := p.Command(exe, args...)
	c.Dir = o.Dir
	c.Env = append(o.Env, "TERM=xterm-256color")
	if err := c.Start(); err != nil {
		p.Close()
		return nil, err
	}
	r := &Runner{pty: p, cmd: c, done: make(chan struct{}), subs: map[*Sub]bool{}, cols: o.Cols, rows: o.Rows, trust: o.TrustFolder, lastOut: time.Now(), ready: make(chan struct{}), awaitStart: o.AwaitStart, launched: time.Now(), modes: newModeTracker(), scr: newScreen(o.Cols, o.Rows)}
	go r.read()
	go r.watchReady()
	go func() {
		r.err = c.Wait()
		r.mu.Lock()
		r.exited = true // no new front ends from here on
		for sub := range r.subs {
			close(sub.C)
			delete(r.subs, sub)
		}
		r.mu.Unlock()
		_ = p.Close()
		close(r.done)
	}()
	return r, nil
}

// Done is closed after Claude Code exited.
func (r *Runner) Done() <-chan struct{} { return r.done }

// Err is the exit error, valid after Done.
func (r *Runner) Err() error { return r.err }

// Pid is the process id of Claude Code (or of the shell running it).
func (r *Runner) Pid() int {
	if r.cmd.Process == nil {
		return 0
	}
	return r.cmd.Process.Pid
}

// watchReady closes ready once the TUI drew its first screen and went quiet
// (input typed before that is lost while Claude Code starts).
func (r *Runner) watchReady() {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-t.C:
			r.mu.Lock()
			started := !r.awaitStart || r.started || time.Since(r.launched) > startWait
			up := started && r.outSeen && time.Since(r.lastOut) >= readyQuiet && (!r.trustDone || r.trustSettled)
			r.mu.Unlock()
			if up {
				close(r.ready)
				return
			}
		}
	}
}

// Ready is closed once Claude Code accepts input.
func (r *Runner) Ready() <-chan struct{} { return r.ready }

// MarkStarted reports Claude Code's SessionStart hook (see AwaitStart).
func (r *Runner) MarkStarted() {
	r.mu.Lock()
	r.started = true
	r.mu.Unlock()
}

// Shows reports whether recent output contains text (whitespace ignored),
// e.g. a message still sitting in the input box; long pastes show as
// "[Pasted text ...]".
func (r *Runner) Shows(text string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	screen := flat(string(r.recent))
	want := []rune(flat(text))
	if len(want) > 24 {
		want = want[:24]
	}
	return (len(want) > 0 && strings.Contains(screen, string(want))) || strings.Contains(screen, "pastedtext")
}

// Submit presses Enter.
func (r *Runner) Submit() error { return r.write([]byte("\r")) }

// Activity is Claude Code's spinner line while it works, such as
// "Composing… (8m 33s · ↓ 26.2k tokens)", or "".
func (r *Runner) Activity() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scr.activity()
}

func (r *Runner) read() {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.pty.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			r.mu.Lock()
			r.lastOut = time.Now()
			r.outSeen = true
			r.modes.observe(chunk)
			r.scr.write(chunk)
			r.recent = append(r.recent, ansi.ReplaceAll(chunk, nil)...)
			if len(r.recent) > recentBytes {
				r.recent = append([]byte(nil), r.recent[len(r.recent)-recentBytes:]...)
			}
			for sub := range r.subs {
				select {
				case sub.C <- chunk:
				default: // a front end that cannot keep up is dropped; it can attach again
					sub.dropped = true
					close(sub.C)
					delete(r.subs, sub)
				}
			}
			// The dialog only comes at start: text about it later (a reply
			// quoting it) must not press keys.
			trust := r.trust && !r.trustDone && !isClosed(r.ready) && strings.Contains(flat(string(r.recent)), "trustthisfolder")
			if trust {
				r.trustDone = true
			}
			r.mu.Unlock()
			if trust {
				go r.confirmTrust()
			}
		}
		if err != nil {
			return
		}
	}
}

func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// flat removes whitespace: the TUI positions words with cursor moves, so
// stripped output has no spaces between them.
func flat(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), "")) }

// confirmTrust answers the folder trust dialog, whose selection starts on
// "No, exit": it moves to "Yes" and confirms only when the selection is there.
func (r *Runner) confirmTrust() {
	defer func() {
		r.mu.Lock()
		r.trustSettled = true
		r.mu.Unlock()
	}()
	time.Sleep(800 * time.Millisecond)
	r.mu.Lock()
	mark := len(r.recent)
	r.mu.Unlock()
	r.write([]byte("\x1b[B"))
	time.Sleep(800 * time.Millisecond)
	r.mu.Lock()
	after := ""
	if mark <= len(r.recent) {
		after = flat(string(r.recent[mark:]))
	}
	r.mu.Unlock()
	if i := strings.LastIndex(after, "❯"); i >= 0 && strings.HasPrefix(after[i+len("❯"):], "yes") {
		r.write([]byte("\r"))
	}
}

func (r *Runner) write(b []byte) error {
	select {
	case <-r.done:
		return errors.New("Claude Code 已退出")
	default:
	}
	_, err := r.pty.Write(b)
	return err
}

// Subscribe attaches a front end. The caller reads sub.C until it is closed
// and calls Unsubscribe when it leaves.
func (r *Runner) Subscribe() *Sub {
	sub := &Sub{C: make(chan []byte, subBuffer)}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.exited {
		close(sub.C)
		return sub
	}
	// The front end's terminal first enters the modes Claude Code turned on.
	if replay := r.modes.replay(); len(replay) > 0 {
		sub.C <- replay
	}
	r.subs[sub] = true
	return sub
}

// Unsubscribe detaches a front end; Claude Code keeps running.
func (r *Runner) Unsubscribe(sub *Sub) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.subs[sub] {
		delete(r.subs, sub)
		close(sub.C)
	}
}

// Attached is the number of terminal front ends.
func (r *Runner) Attached() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subs)
}

// Keys forwards a front end's keystrokes.
func (r *Runner) Keys(b []byte) error {
	r.mu.Lock()
	r.lastKey = time.Now()
	r.mu.Unlock()
	return r.write(b)
}

// Resize sets the terminal size: the most recent front end to resize wins.
func (r *Runner) Resize(cols, rows int) error {
	if cols < 20 || rows < 5 || cols > 1000 || rows > 500 {
		return errors.New("invalid terminal size")
	}
	r.mu.Lock()
	same := cols == r.cols && rows == r.rows
	r.cols, r.rows = cols, rows
	if !same {
		r.scr.resize(cols, rows)
	}
	r.mu.Unlock()
	if same {
		return nil
	}
	return r.pty.Resize(cols, rows)
}

// Redraw makes the TUI paint the whole screen again, for a front end that
// just attached: a size change makes it re-render.
func (r *Runner) Redraw() {
	r.mu.Lock()
	cols, rows := r.cols, r.rows
	r.mu.Unlock()
	_ = r.pty.Resize(cols-1, rows)
	time.Sleep(60 * time.Millisecond)
	r.mu.Lock()
	cols, rows = r.cols, r.rows // a front end may have resized meanwhile
	r.mu.Unlock()
	_ = r.pty.Resize(cols, rows)
}

// Paste sends a web message: bracketed paste keeps newlines inside one
// message, then Enter submits it. It waits for a pause in a front end's
// typing (up to 10 s) so the two do not interleave.
func (r *Runner) Paste(text string) error {
	r.pasteMu.Lock()
	defer r.pasteMu.Unlock()
	select {
	case <-r.ready:
	case <-r.done:
		return errors.New("Claude Code 已退出")
	case <-time.After(readyWait):
	}
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); {
		r.mu.Lock()
		quiet := time.Since(r.lastKey) >= typingPause
		r.mu.Unlock()
		if quiet {
			break
		}
		select {
		case <-r.done:
			return errors.New("Claude Code 已退出")
		case <-time.After(200 * time.Millisecond):
		}
	}
	if err := r.write([]byte("\x1b[200~" + CleanText(text) + "\x1b[201~")); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)
	return r.write([]byte("\r"))
}

// CleanText is a web message as it can be pasted: control characters other
// than newline and tab are dropped, so the text cannot end the bracketed
// paste early or press keys (ESC sequences, Enter, 8-bit CSI).
func CleanText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.Map(func(c rune) rune {
		if c == '\n' || c == '\t' {
			return c
		}
		if c < 0x20 || c == 0x7f || (c >= 0x80 && c <= 0x9f) {
			return -1
		}
		return c
	}, text)
}

// Interrupt presses Esc, which stops the running turn.
func (r *Runner) Interrupt() error { return r.write([]byte("\x1b")) }

// Cancel presses Ctrl+C.
func (r *Runner) Cancel() error { return r.write([]byte("\x03")) }

// Close ends Claude Code: Ctrl+C twice (its exit gesture), then the process
// is killed if it is still running after grace.
func (r *Runner) Close(grace time.Duration) {
	_ = r.write([]byte("\x1b"))
	time.Sleep(100 * time.Millisecond)
	_ = r.write([]byte("\x03"))
	time.Sleep(300 * time.Millisecond)
	_ = r.write([]byte("\x03"))
	select {
	case <-r.done:
		return
	case <-time.After(grace):
	}
	if r.cmd.Process != nil {
		killTree(r.cmd.Process.Pid)
	}
	_ = r.pty.Close()
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
	}
}

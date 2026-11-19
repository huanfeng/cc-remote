package terminal

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

const (
	projectShellJournalBytes  = 2 << 20
	projectShellJournalEvents = 4096
	projectShellSubscriber    = 128
	projectShellWriteTimeout  = 2 * time.Second
)

var errProjectShellExited = errors.New("shell has exited")

// ProjectShellEvent is immutable. A dimension change has no Data.
type ProjectShellEvent struct {
	Sequence   uint64
	Data       []byte
	Cols, Rows int
}

// ProjectShellReplay covers all events up through Through. Cols and Rows are
// the dimensions at the start of the replay. A reset replaces missing history
// with a snapshot at Through, with no earlier events.
type ProjectShellReplay struct {
	Events     []ProjectShellEvent
	Cols, Rows int
	Through    uint64
	Reset      bool
	Screen     []byte
}

// ProjectShell owns one process and its PTY for the lifetime of a run.
// Clients can leave and rejoin without affecting the process.
type ProjectShell struct {
	pty         pty.Pty
	cmd         *pty.Cmd
	done        chan struct{}
	processDone chan struct{}
	readDone    chan struct{}
	closePTY    sync.Once
	writeMu     sync.Mutex
	resizeMu    sync.Mutex

	mu          sync.Mutex
	closing     bool
	cols, rows  int
	baseCols    int
	baseRows    int
	sequence    uint64
	bytes       int
	journal     []ProjectShellEvent
	subscribers map[chan ProjectShellEvent]struct{}
	screen      *screen
	modes       *modeTracker
}

// StartProjectShell starts the platform's interactive shell in dir.
func StartProjectShell(dir string) (*ProjectShell, error) {
	exe, args, err := projectShellCommand()
	if err != nil {
		return nil, err
	}
	return startProjectShell(dir, exe, args)
}

func startProjectShell(dir, exe string, args []string) (*ProjectShell, error) {
	p, err := pty.New()
	if err != nil {
		return nil, err
	}
	const cols, rows = 120, 40
	if err := p.Resize(cols, rows); err != nil {
		_ = p.Close()
		return nil, err
	}
	c := p.Command(exe, args...)
	c.Dir = dir
	for _, entry := range CleanEnv(os.Environ()) {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(name, "TERM") {
			c.Env = append(c.Env, entry)
		}
	}
	c.Env = append(c.Env, "TERM=xterm-256color")
	if err := c.Start(); err != nil {
		_ = p.Close()
		return nil, err
	}
	if unix, ok := p.(pty.UnixPty); ok {
		// The child holds its own slave descriptor. Closing ours lets the
		// reader observe EOF when the last process attached to the tty exits.
		_ = unix.Slave().Close()
	}
	s := &ProjectShell{
		pty: p, cmd: c, done: make(chan struct{}), processDone: make(chan struct{}), readDone: make(chan struct{}),
		cols: cols, rows: rows, baseCols: cols, baseRows: rows,
		subscribers: make(map[chan ProjectShellEvent]struct{}), screen: newScreen(cols, rows), modes: newModeTracker(),
	}
	go s.read()
	go s.wait()
	return s, nil
}

func (s *ProjectShell) read() {
	defer close(s.readDone)
	buf := make([]byte, 32<<10)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.screen.write(chunk)
			s.modes.observe(chunk)
			s.publishLocked(ProjectShellEvent{Data: chunk})
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *ProjectShell) wait() {
	_ = s.cmd.Wait()
	close(s.processDone)
	if _, ok := s.pty.(pty.UnixPty); ok {
		// Drain the Unix master after process exit, with a bound in case a
		// descendant still holds an open slave descriptor.
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-s.readDone:
		case <-timer.C:
		}
		timer.Stop()
	}
	// ConPTY may emit final output while closing; the reader is still active.
	s.closePTY.Do(func() { _ = s.pty.Close() })
	<-s.readDone
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.done)
	for ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, ch)
	}
}

func (s *ProjectShell) publishLocked(event ProjectShellEvent) {
	s.sequence++
	event.Sequence = s.sequence
	s.journal = append(s.journal, event)
	s.bytes += len(event.Data)
	for s.bytes > projectShellJournalBytes || len(s.journal) > projectShellJournalEvents {
		old := s.journal[0]
		s.bytes -= len(old.Data)
		if old.Cols != 0 {
			s.baseCols, s.baseRows = old.Cols, old.Rows
		}
		s.journal[0] = ProjectShellEvent{}
		s.journal = s.journal[1:]
	}
	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
			// Reattaching from the acknowledged cursor will recover the gap.
			close(ch)
			delete(s.subscribers, ch)
		}
	}
}

// Subscribe atomically captures catch-up output and attaches a live subscriber.
// A slow subscriber's channel is closed so it can reattach with a fresh cursor.
func (s *ProjectShell) Subscribe(after uint64) (ProjectShellReplay, <-chan ProjectShellEvent, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	replay := ProjectShellReplay{Through: s.sequence, Cols: s.baseCols, Rows: s.baseRows}
	oldestCursor := s.sequence - uint64(len(s.journal))
	if after < oldestCursor || after > s.sequence {
		replay.Reset = true
		replay.Cols, replay.Rows = s.cols, s.rows
		replay.Screen = shellScreenSnapshot(s.screen, s.modes)
	} else {
		index := int(after - oldestCursor)
		for i := index - 1; i >= 0; i-- {
			if event := s.journal[i]; event.Cols != 0 {
				replay.Cols, replay.Rows = event.Cols, event.Rows
				break
			}
		}
		replay.Events = append([]ProjectShellEvent(nil), s.journal[index:]...)
	}
	ch := make(chan ProjectShellEvent, projectShellSubscriber)
	if isClosed(s.done) {
		close(ch)
	} else {
		s.subscribers[ch] = struct{}{}
	}
	return replay, ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, subscribed := s.subscribers[ch]; subscribed {
			delete(s.subscribers, ch)
			close(ch)
		}
	}
}

// shellScreenSnapshot renders the screen reader's best-effort text and cursor
// state. The VT journal handles full fidelity while the cursor is in range.
func shellScreenSnapshot(screen *screen, modes *modeTracker) []byte {
	var frame strings.Builder
	frame.WriteString("\x1b[2J\x1b[?7l")
	for row, line := range screen.lines() {
		if line != "" {
			fmt.Fprintf(&frame, "\x1b[%d;1H%s", row+1, line)
		}
	}
	fmt.Fprintf(&frame, "\x1b[?7h\x1b[%d;%dH", screen.y+1, screen.x+1)
	frame.Write(modes.replay())
	return []byte(frame.String())
}

// Done closes after the underlying process and PTY reader have finished.
func (s *ProjectShell) Done() <-chan struct{} { return s.done }

func (s *ProjectShell) stopping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing || isClosed(s.processDone)
}

// Write forwards keys without interpreting them as shell commands.
func (s *ProjectShell) Write(data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.stopping() {
		return errProjectShellExited
	}
	var pipe *os.File
	switch p := s.pty.(type) {
	case pty.UnixPty:
		pipe = p.Master()
	case pty.ConPty:
		pipe = p.InputPipe()
	}
	if pipe != nil && pipe.SetWriteDeadline(time.Now().Add(projectShellWriteTimeout)) == nil {
		defer pipe.SetWriteDeadline(time.Time{})
	}
	_, err := s.pty.Write(data)
	return err
}

// Resize records the dimension change before any resulting screen updates.
func (s *ProjectShell) Resize(cols, rows int) error {
	if err := validSize(cols, rows); err != nil {
		return err
	}
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	return s.resize(cols, rows)
}

func (s *ProjectShell) resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || isClosed(s.processDone) {
		return errProjectShellExited
	}
	if (cols == s.cols) && (rows == s.rows) {
		return nil
	}
	if err := s.pty.Resize(cols, rows); err != nil {
		return err
	}
	s.cols, s.rows = cols, rows
	s.screen.resize(cols, rows)
	s.publishLocked(ProjectShellEvent{Cols: cols, Rows: rows})
	return nil
}

// Redraw requests application repaint through a brief size change. It avoids
// sending an input key, which could alter a foreground command's state.
func (s *ProjectShell) Redraw() error {
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	s.mu.Lock()
	cols, rows := s.cols, s.rows
	s.mu.Unlock()
	adjacent := cols - 1
	if validSize(adjacent, rows) != nil {
		adjacent = cols + 1
	}
	if err := s.resize(adjacent, rows); err != nil {
		return err
	}
	time.Sleep(60 * time.Millisecond)
	return s.resize(cols, rows)
}

// Close starts stopping the process, independently of the client requesting it.
// Callers that need to wait for cleanup can wait on Done.
func (s *ProjectShell) Close() {
	s.mu.Lock()
	if s.closing || isClosed(s.processDone) {
		s.mu.Unlock()
		return
	}
	s.closing = true
	s.mu.Unlock()
	go func() {
		stopProjectShellProcess(s.cmd.Process, s.processDone)
		s.closePTY.Do(func() { _ = s.pty.Close() })
	}()
}

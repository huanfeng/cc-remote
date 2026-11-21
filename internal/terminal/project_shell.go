package terminal

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

const (
	projectShellJournalBytes  = 16 << 20
	projectShellJournalEvents = 64 << 10
	projectShellSubscriber    = 128
	projectShellWriteTimeout  = 2 * time.Second
)

var errProjectShellExited = errors.New("shell has exited")

// ProjectShellEvent is immutable. A dimension change has no Data.
type ProjectShellEvent struct {
	Sequence   uint64
	Data       []byte
	Cols, Rows int
	// Size after the event, for seeking without scanning the journal.
	cols, rows uint16
}

// ProjectShellReplay covers output through Through. Cols and Rows are the
// dimensions before Events. A reset restores a snapshot at ScreenSequence,
// immediately before the first retained event. Latest is the newest sequence
// when this segment was captured.
type ProjectShellReplay struct {
	Events         []ProjectShellEvent
	Cols, Rows     int
	Through        uint64
	Reset          bool
	Screen         []byte
	ScreenSequence uint64
	Latest         uint64
	More           bool
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
	closing     atomic.Bool
	cols, rows  int
	sequence    uint64
	bytes       int
	journal     []ProjectShellEvent
	journalHead int
	journalSize int
	subscribers map[chan ProjectShellEvent]struct{}
	watchers    map[chan struct{}]struct{}
	// Interpret only the evicted prefix. Recent output is replayed verbatim, and
	// the live PTY never has to maintain a second screen before history fills.
	screen *screen
	modes  *modeTracker
}

// StartProjectShell starts the platform's interactive shell in dir.
func StartProjectShell(dir, runID string) (*ProjectShell, error) {
	exe, args, err := projectShellCommand()
	if err != nil {
		return nil, err
	}
	return startProjectShell(dir, exe, args, runID)
}

func startProjectShell(dir, exe string, args []string, runID string) (*ProjectShell, error) {
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
		if !strings.EqualFold(name, "TERM") && !strings.EqualFold(name, projectShellRunEnv) {
			c.Env = append(c.Env, entry)
		}
	}
	c.Env = append(c.Env, "TERM=xterm-256color", projectShellRunEnv+"="+runID)
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
		cols: cols, rows: rows,
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
	for ch := range s.watchers {
		close(ch)
		delete(s.watchers, ch)
	}
}

func (s *ProjectShell) publishLocked(event ProjectShellEvent) {
	s.sequence++
	event.Sequence = s.sequence
	event.cols, event.rows = uint16(s.cols), uint16(s.rows)
	for s.journalSize > 0 && (s.bytes+len(event.Data) > projectShellJournalBytes || s.journalSize == projectShellJournalEvents) {
		old := s.journal[s.journalHead]
		s.bytes -= len(old.Data)
		if old.Cols != 0 {
			s.screen.resize(old.Cols, old.Rows)
		} else {
			modeBytes := old.Data
			if s.screen.state == stCSI || s.screen.state == stEsc {
				// A mode-setting CSI can straddle two PTY reads.
				modeBytes = []byte{0x1b}
				if s.screen.state == stCSI {
					modeBytes = append(append(modeBytes, '['), s.screen.seq...)
				}
				modeBytes = append(modeBytes, old.Data...)
			}
			s.screen.write(old.Data)
			s.modes.observe(modeBytes)
		}
		s.journal[s.journalHead] = ProjectShellEvent{}
		s.journalHead = (s.journalHead + 1) % len(s.journal)
		s.journalSize--
	}
	if s.journalSize == len(s.journal) {
		grown := make([]ProjectShellEvent, max(128, 2*len(s.journal)))
		n := copy(grown, s.journal[s.journalHead:])
		copy(grown[n:], s.journal[:s.journalHead])
		s.journal, s.journalHead = grown, 0
	}
	s.journal[(s.journalHead+s.journalSize)%len(s.journal)] = event
	s.journalSize++
	s.bytes += len(event.Data)
	for ch := range s.watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
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

// WatchOutput notifies a reader that output is available. Signals coalesce;
// bytes stay in the shared journal until the reader requests them. A closed
// channel means the run ended, but there can still be unread output.
func (s *ProjectShell) WatchOutput() (<-chan struct{}, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan struct{}, 1)
	if isClosed(s.done) {
		close(ch)
	} else {
		if s.watchers == nil {
			s.watchers = make(map[chan struct{}]struct{})
		}
		s.watchers[ch] = struct{}{}
	}
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, watching := s.watchers[ch]; watching {
			delete(s.watchers, ch)
			close(ch)
		}
	}
}

// ReadOutput returns the next output segment. maxBytes is a soft limit on
// event data and framing, excluding the snapshot. A positive budget still
// permits the first event so a cursor can always advance.
func (s *ProjectShell) ReadOutput(after uint64, maxBytes int) ProjectShellReplay {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readOutputLocked(after, maxBytes)
}

func (s *ProjectShell) readOutputLocked(after uint64, maxBytes int) ProjectShellReplay {
	oldestCursor := s.sequence - uint64(s.journalSize)
	replay := ProjectShellReplay{Cols: s.screen.cols, Rows: s.screen.rows, Latest: s.sequence}
	if after < oldestCursor || after > s.sequence {
		replay.Reset = true
		replay.ScreenSequence = oldestCursor
		replay.Screen = shellScreenSnapshot(s.screen, s.modes)
		after = oldestCursor
	}
	replay.Through = after
	idx := int(after - oldestCursor)
	if idx > 0 {
		last := s.journal[(s.journalHead+idx-1)%len(s.journal)]
		replay.Cols, replay.Rows = int(last.cols), int(last.rows)
	}
	if maxBytes <= 0 && idx < s.journalSize {
		replay.Events = make([]ProjectShellEvent, 0, s.journalSize-idx)
	}
	used := 0
	for ; idx < s.journalSize; idx++ {
		event := s.journal[(s.journalHead+idx)%len(s.journal)]
		cost := len(event.Data) + 24
		if maxBytes > 0 && used > 0 && used+cost > maxBytes {
			replay.More = true
			break
		}
		replay.Events = append(replay.Events, event)
		replay.Through = event.Sequence
		used += cost
	}
	return replay
}

// Subscribe atomically captures catch-up output and attaches a live subscriber.
// A slow subscriber's channel is closed so it can reattach with a fresh cursor.
func (s *ProjectShell) Subscribe(after uint64) (ProjectShellReplay, <-chan ProjectShellEvent, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	replay := s.readOutputLocked(after, 0)
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
	x := screen.x
	if screen.pendingWrap && x > 0 && screen.cells[screen.y][x].r == 0 {
		x-- // Position at the left half when a wide character filled the row.
	}
	fmt.Fprintf(&frame, "\x1b[?7h\x1b[%d;%dH", screen.y+1, x+1)
	if screen.pendingWrap {
		// Repainting the rightmost cell restores xterm's pending auto-wrap.
		frame.WriteRune(screen.cells[screen.y][x].r)
	}
	frame.Write(modes.replay())
	// The checkpoint can bisect a Unicode character or an escape sequence.
	// Leave xterm's parser waiting for its continuation in the first raw event.
	switch screen.state {
	case stGround:
		frame.Write(screen.seq)
	case stEsc:
		frame.WriteByte(0x1b)
	case stCSI:
		frame.WriteString("\x1b[")
		frame.Write(screen.seq)
	case stCharset:
		frame.WriteString("\x1b(")
	case stString:
		frame.WriteString("\x1b]")
	case stStringEsc:
		frame.WriteString("\x1b]\x1b")
	}
	return []byte(frame.String())
}

// Done closes after the underlying process and PTY reader have finished.
func (s *ProjectShell) Done() <-chan struct{} { return s.done }

func (s *ProjectShell) stopping() bool {
	return s.closing.Load() || isClosed(s.processDone)
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
	if s.stopping() {
		return errProjectShellExited
	}
	if (cols == s.cols) && (rows == s.rows) {
		return nil
	}
	if err := s.pty.Resize(cols, rows); err != nil {
		return err
	}
	s.cols, s.rows = cols, rows
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
	if s.stopping() {
		s.mu.Unlock()
		return
	}
	s.closing.Store(true)
	s.mu.Unlock()
	go func() {
		stopProjectShellProcess(s.cmd.Process, s.processDone)
		s.closePTY.Do(func() { _ = s.pty.Close() })
	}()
}

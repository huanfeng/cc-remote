package terminal

import (
	"context"
	"encoding/base64"
	"io"
	"sync"

	"cc-remote/internal/i18n"
)

// OSC 7501 is the Program Status Protocol (https://mitchellh.com/writing/program-status-osc7501):
// a program tells the terminal around it what it is doing, with
//
//	ESC ] 7501 ; state=working:app=claude-code ESC \
//
// herdr-core (hc, the Herdr desktop app) takes a pane's agent status from it.
// Claude Code runs on the host, behind its own PTY (ConPTY on Windows), so
// whatever it reports itself never reaches this terminal. ccr reports the
// host's exact state instead, and knows when a web answer ends a wait.

const oscApp = "claude-code"

// vt tracks just enough of the byte stream written to a terminal to know when
// an escape sequence or a UTF-8 character is cut in two.
type vtState int

const (
	vtGround vtState = iota
	vtEsc            // after ESC (and its intermediates)
	vtCSI            // inside ESC [ …
	vtStr            // inside an OSC / DCS / APC / PM / SOS string
	vtStrEsc         // ESC inside such a string (ESC \ ends it)
)

// oscStatus sits between the session's screen output and the terminal. It
// passes the output through unchanged, and puts the status sequence between
// two whole sequences, never inside one.
type oscStatus struct {
	out   io.Writer // the terminal
	inner io.Writer // where the screen output goes on (out, or a writer in front of it)

	mu      sync.Mutex
	st      vtState
	u8      int    // continuation bytes of a UTF-8 character still to come
	pending []byte // a status that waits for a boundary
	last    string // the state the terminal was told last
}

// newOSCStatus is nil unless this process runs in an hc terminal (it sets
// HC_TERMINAL_ID) or CC_REMOTE_OSC7501=1 asks for it. CC_REMOTE_HERDR=0 turns
// the Herdr bridge off, this one included.
func newOSCStatus(getenv func(string) string, out io.Writer) *oscStatus {
	if getenv("CC_REMOTE_HERDR") == "0" || getenv("CC_REMOTE_OSC7501") == "0" {
		return nil
	}
	if getenv("HC_TERMINAL_ID") == "" && getenv("CC_REMOTE_OSC7501") != "1" {
		return nil
	}
	return &oscStatus{out: out, inner: out}
}

// wrap makes next the place the screen output goes on to, and returns the
// writer the session's screen output must be written through.
func (o *oscStatus) wrap(next io.Writer) io.Writer {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.inner = next
	return o
}

func (o *oscStatus) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n, err := o.inner.Write(p)
	o.track(p[:n])
	if o.pending != nil && o.safe() {
		_, _ = o.out.Write(o.pending)
		o.pending = nil
	}
	return n, err
}

func (o *oscStatus) safe() bool { return o.st == vtGround && o.u8 == 0 }

func (o *oscStatus) track(p []byte) {
	for _, b := range p {
		switch o.st {
		case vtGround:
			if o.u8 > 0 {
				if b&0xC0 == 0x80 {
					o.u8--
					continue
				}
				o.u8 = 0 // not a continuation: the character was cut short
			}
			switch {
			case b == 0x1b:
				o.st = vtEsc
			case b >= 0xF0:
				o.u8 = 3
			case b >= 0xE0:
				o.u8 = 2
			case b >= 0xC0:
				o.u8 = 1
			}
		case vtEsc:
			switch {
			case b == '[':
				o.st = vtCSI
			case b == ']' || b == 'P' || b == '_' || b == '^' || b == 'X':
				o.st = vtStr
			case b >= 0x20 && b <= 0x2f: // an intermediate: the final byte is still to come
			case b == 0x1b:
			default:
				o.st = vtGround
			}
		case vtCSI:
			switch {
			case b >= 0x40 && b <= 0x7e:
				o.st = vtGround
			case b == 0x1b:
				o.st = vtEsc
			}
		case vtStr:
			switch b {
			case 0x07:
				o.st = vtGround
			case 0x1b:
				o.st = vtStrEsc
			}
		case vtStrEsc:
			if b == '\\' {
				o.st = vtGround
			} else { // ESC began a new sequence
				o.st = vtEsc
				if b == '[' {
					o.st = vtCSI
				} else if b == ']' {
					o.st = vtStr
				} else if b != 0x1b && !(b >= 0x20 && b <= 0x2f) {
					o.st = vtGround
				}
			}
		}
	}
}

// oscSequence is the OSC 7501 record for a session state. A wait carries a short
// message (base64, as the protocol asks) for terminals that show it.
func oscSequence(state string) []byte {
	s := "\x1b]7501;state=" + state
	if state == "clear" {
		return []byte(s + "\x1b\\")
	}
	s += ":app=" + oscApp
	if state == "blocked" {
		s += ":msg=" + base64.StdEncoding.EncodeToString([]byte(i18n.T("Waiting for an answer", "等待回答")))
	}
	return []byte(s + "\x1b\\")
}

// oscState maps the host's session status to a 7501 state.
func oscState(status string) string {
	switch status {
	case "running":
		return "working"
	case "waiting":
		return "blocked"
	}
	return "idle"
}

// report tells the terminal now, or at the next boundary of the output.
func (o *oscStatus) report(state string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if state == o.last {
		return
	}
	o.last = state
	seq := oscSequence(state)
	if o.safe() {
		o.pending = nil
		_, _ = o.out.Write(seq)
		return
	}
	o.pending = seq // only the latest matters
}

// sync follows the session: a session the host cannot run right now clears
// the record, anything else reports its state.
func (o *oscStatus) sync(_ context.Context, x *remoteSession) {
	if x == nil || !x.Online {
		o.report("clear")
		return
	}
	o.report(oscState(x.Status))
}

// close clears the record: the terminal is no longer attached to anything.
// A shell that ccr returns to must not be left looking like Claude Code.
func (o *oscStatus) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.last == "" || o.last == "clear" {
		return
	}
	o.last, o.pending = "clear", nil
	if !o.safe() { // end whatever string the stream was cut in
		_, _ = o.out.Write([]byte("\x1b\\"))
		o.st, o.u8 = vtGround, 0
	}
	_, _ = o.out.Write(oscSequence("clear"))
}

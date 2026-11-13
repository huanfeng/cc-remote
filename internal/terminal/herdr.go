package terminal

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"cc-remote/internal/i18n"
)

// Herdr (https://herdr.dev) is a terminal multiplexer that tracks AI agents
// per pane. ccr runs inside a pane but Claude Code runs on the host, so Herdr
// can neither see the process nor reach the host's hooks. While a terminal is
// attached, the bridge below reports the host's own, exact state of the
// session to Herdr (design: docs/design/herdr-bridge.md).

// defaultHost is the host ccr and attach use without --host.
const defaultHost = "http://127.0.0.1:8787"

const (
	herdrSource      = "custom:cc-remote"
	herdrMetaSource  = "user:cc-remote"
	herdrAgent       = "claude"
	herdrCallTimeout = 2 * time.Second
	// herdrBackoff is how long to leave a failing herdr alone, so a broken
	// install is not spawned on every poll.
	herdrBackoff = 10 * time.Second
	herdrPoll    = time.Second
)

type herdrBridge struct {
	bin, pane string
	// resume is the command Herdr re-runs in this pane after it restarts; nil
	// when ccr is not on PATH (Herdr wants a bare command name).
	resume []string
	exec   func(ctx context.Context, args []string) error
	now    func() time.Time

	mu       sync.Mutex
	seq      int64
	sent     string // the state Herdr holds for this pane; "" when none
	metaSent bool   // the display name and title were accepted
	title    string // the title Herdr shows
	closed   bool
	retryAt  time.Time
}

// newHerdrBridge is nil unless this process runs in a Herdr pane (and the
// bridge is not switched off with CC_REMOTE_HERDR=0).
func newHerdrBridge(getenv func(string) string, lookPath func(string) (string, error), host, sessionID string) *herdrBridge {
	if getenv("HERDR_ENV") != "1" || getenv("CC_REMOTE_HERDR") == "0" {
		return nil
	}
	pane := getenv("HERDR_PANE_ID")
	if pane == "" {
		return nil
	}
	bin := getenv("HERDR_BIN_PATH")
	if bin == "" {
		p, err := lookPath("herdr")
		if err != nil {
			return nil
		}
		bin = p
	}
	b := &herdrBridge{bin: bin, pane: pane, now: time.Now}
	if _, err := lookPath("ccr"); err == nil {
		b.resume = []string{"ccr", sessionID}
		if host != defaultHost { // the restarted pane must reach the same host
			b.resume = []string{"ccr", "--host", host, sessionID}
		}
	}
	b.exec = func(ctx context.Context, args []string) error {
		// The output must never reach the terminal Claude Code is drawn on.
		return exec.CommandContext(ctx, b.bin, args...).Run()
	}
	return b
}

// herdrState maps the host's session status to Herdr's agent states.
func herdrState(status string) string {
	switch status {
	case "running":
		return "working"
	case "waiting":
		return "blocked"
	}
	return "idle"
}

// sync brings Herdr in line with the session: nil or offline releases the
// pane, anything else reports the state (only when it changed).
func (b *herdrBridge) sync(ctx context.Context, x *remoteSession) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	if x == nil || !x.Online {
		b.releaseLocked(ctx)
		return
	}
	state, title := herdrState(x.Status), herdrText(x.Title)
	if state == b.sent && b.metaSent && title == b.title {
		return
	}
	if b.now().Before(b.retryAt) {
		return
	}
	if state != b.sent {
		args := []string{"pane", "report-agent", b.pane, "--source", herdrSource, "--agent", herdrAgent, "--state", state}
		if state == "blocked" {
			args = append(args, "--message", i18n.T("Waiting for an answer", "等待回答"))
		}
		args = append(args, "--seq", b.nextSeq())
		if b.resume != nil {
			args = append(args, "--")
			args = append(args, b.resume...)
		}
		if err := b.call(ctx, args); err != nil {
			b.retryAt = b.now().Add(herdrBackoff)
			return
		}
		b.sent = state
	}
	if !b.metaSent || title != b.title {
		meta := []string{"pane", "report-metadata", b.pane, "--source", herdrMetaSource, "--agent", herdrAgent, "--display-agent", "Claude"}
		if title != "" {
			meta = append(meta, "--title", title)
		}
		if err := b.call(ctx, meta); err != nil {
			b.retryAt = b.now().Add(herdrBackoff)
			return
		}
		b.metaSent, b.title = true, title
	}
}

// close releases the pane for good; later syncs do nothing.
func (b *herdrBridge) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.releaseLocked(ctx)
}

func (b *herdrBridge) releaseLocked(ctx context.Context) {
	if b.sent == "" {
		return
	}
	b.sent, b.metaSent = "", false
	_ = b.call(ctx, []string{"pane", "release-agent", b.pane, "--source", herdrSource, "--agent", herdrAgent, "--seq", b.nextSeq()})
}

func (b *herdrBridge) call(ctx context.Context, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, herdrCallTimeout)
	defer cancel()
	return b.exec(ctx, args)
}

// nextSeq is a millisecond timestamp that still rises when the clock does
// not: Herdr ignores reports whose number is not above the last one.
func (b *herdrBridge) nextSeq() string {
	n := b.now().UnixMilli()
	if n <= b.seq {
		n = b.seq + 1
	}
	b.seq = n
	return strconv.FormatInt(n, 10)
}

// statusSink is where the session's state goes: Herdr's CLI (herdrBridge) or
// the terminal itself (oscStatus).
type statusSink interface {
	sync(ctx context.Context, x *remoteSession)
	close()
}

func (b *herdrBridge) run(ctx context.Context, poll func() (*remoteSession, error), every time.Duration) {
	followSession(ctx, b, poll, every)
}

// followSession feeds the sink the session's state until ctx ends. A failed
// poll says nothing about the session (the host may be restarting), so it
// changes nothing.
func followSession(ctx context.Context, sink statusSink, poll func() (*remoteSession, error), every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if x, err := poll(); err == nil {
			sink.sync(ctx, x)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// herdrText is a title Herdr can show: one line, no control characters, no
// leading dashes (they would read as a flag), at most 80 characters.
func herdrText(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	s = strings.TrimLeft(s, "- ")
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80])
	}
	return s
}

// startHerdr starts reporting this attached session to Herdr when the process
// runs in a Herdr pane (through its CLI) or in an hc terminal (through OSC
// 7501, osc being the writer the screen output goes through). The returned
// function stops it and releases the pane.
func (c *client) startHerdr(x remoteSession, osc *oscStatus) (stop func()) {
	var sink statusSink
	if b := newHerdrBridge(os.Getenv, exec.LookPath, c.host, x.ID); b != nil {
		sink = b
	} else if osc != nil {
		sink = osc
	} else {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	poll := func() (*remoteSession, error) {
		list, err := c.terminals()
		if err != nil {
			return nil, err
		}
		for i := range list {
			if list[i].ID == x.ID {
				return &list[i], nil
			}
		}
		return nil, nil // the session is gone
	}
	go followSession(ctx, sink, poll, herdrPoll)
	return func() {
		cancel() // ends a running herdr call at once; close then takes the lock quickly
		sink.close()
	}
}

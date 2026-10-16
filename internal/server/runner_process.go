package server

import (
	"context"
	"errors"
	"sync"

	"cc-remote/internal/claude"
)

// runnerProcess runs each message as one Runner call ("claude -p" per turn).
// It backs tests with fake runners and the CC_REMOTE_ONESHOT fallback.
type runnerProcess struct {
	run   Runner
	opts  claude.Options
	hooks claude.ProcessHooks
	queue chan string
	stop  chan struct{}
	done  chan struct{}

	mu      sync.Mutex
	current context.CancelFunc
	closed  bool
}

func runnerStarter(run Runner) ProcessStarter {
	return func(o claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		p := &runnerProcess{run: run, opts: o, hooks: h, queue: make(chan string, maxQueuedMessages+1), stop: make(chan struct{}), done: make(chan struct{})}
		go p.loop()
		return p, nil
	}
}

func (p *runnerProcess) loop() {
	defer func() {
		close(p.done)
		if p.hooks.Exit != nil {
			p.hooks.Exit(nil)
		}
	}()
	for {
		var text string
		select {
		case <-p.stop:
			return
		case text = <-p.queue:
		}
		ctx, cancel := context.WithCancel(context.Background())
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			cancel()
			return
		}
		p.current = cancel
		o := p.opts
		p.mu.Unlock()
		o.Message = text
		if p.hooks.Replay != nil {
			p.hooks.Replay(text)
		}
		err := p.run(ctx, o, func(e claude.Event) error {
			if e.Kind == "session" || e.Kind == "result" {
				p.mu.Lock()
				p.opts.Resume = true // later turns resume the conversation
				p.mu.Unlock()
			}
			if p.hooks.Event == nil {
				return nil
			}
			return p.hooks.Event(e)
		})
		if ctx.Err() != nil && err == nil {
			err = ctx.Err()
		}
		cancel()
		p.mu.Lock()
		p.current = nil
		closed := p.closed
		p.mu.Unlock()
		if closed {
			return // the turn was cut short by Close; Exit settles it
		}
		if p.hooks.Result != nil {
			p.hooks.Result(err, false)
		}
	}
}

func (p *runnerProcess) Send(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("Claude Code is not running")
	}
	select {
	case p.queue <- text:
		return nil
	default:
		return errors.New("too many queued messages")
	}
}

func (p *runnerProcess) Interrupt() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current != nil {
		p.current()
	}
	return nil
}

func (p *runnerProcess) BackgroundTasks() int  { return 0 }
func (p *runnerProcess) Done() <-chan struct{} { return p.done }

func (p *runnerProcess) Close() {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.stop)
		if p.current != nil {
			p.current()
		}
	}
	p.mu.Unlock()
	<-p.done
}

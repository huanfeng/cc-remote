package server

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

// Managed sessions keep one Claude process running between turns.
//
// cc-remote queues web messages itself and writes the next one only after the
// previous turn's result: Claude merges messages that queue up on its stdin
// into one turn (one result for several messages), which would make results
// impossible to attribute. A session is busy (s.runs holds an execution,
// status "running") while a message is queued or in flight, while Claude runs
// a turn it started itself (e.g. to report a finished background agent), or
// while an approval request is open; the execution ends when all are over.

// claudeProcess is the part of claude.Process the server uses.
type claudeProcess interface {
	Send(text string) error
	Interrupt() error
	BackgroundTasks() int
	Close()
	Done() <-chan struct{}
}

// ProcessStarter launches a long-lived Claude process.
type ProcessStarter func(claude.Options, claude.ProcessHooks) (claudeProcess, error)

const (
	maxQueuedMessages = 5
	// An idle process without background tasks exits after Server.idleTimeout
	// (default processIdleTimeout); the next message resumes it in a new one.
	processIdleTimeout = 30 * time.Minute
	// A turn that has not ended this long after "stop" is ended by closing
	// the process; pressing stop again does so at once.
	interruptGrace = 15 * time.Second
)

// launchKey is what a running process was started with; a difference makes
// the next message restart it with --resume.
type launchKey struct {
	claudeID, permissionMode, model, effort string
}

// outgoing is a web message waiting to be written to Claude.
type outgoing struct {
	store.QueuedMessage
	prompt string
}

type sessionRuntime struct {
	id    string
	token string // bridge credential of this process
	key   launchKey
	proc  claudeProcess // nil while starting; set before ready is closed
	ready chan struct{}

	queue     []outgoing
	inflight  bool // a message was written and its result is outstanding
	self      bool // Claude runs a turn it started itself
	approvals int  // open bridge requests

	run     *execution
	release context.CancelFunc // cancels run.ctx without ending the process
	// outcome of the latest turn: "completed", "interrupted" or "failed".
	outcome      string
	interrupting bool
	stopping     bool // detached and being closed; its exit is expected
	idleSince    time.Time
}

func defaultProcessStarter(cfg Config) ProcessStarter {
	if cfg.Runner != nil {
		return runnerStarter(cfg.Runner)
	}
	if os.Getenv("CC_REMOTE_ONESHOT") == "1" {
		return runnerStarter(claude.Run) // one "claude -p" per turn, the pre-0.3 behaviour
	}
	return func(o claude.Options, h claude.ProcessHooks) (claudeProcess, error) {
		return claude.StartProcess(o, h)
	}
}

// runtimeLocked returns the session's runtime, starting a process when none
// runs. An idle process started with other settings is replaced; one with
// background tasks is kept so they are not killed (the settings apply once
// it has been idle long enough to be reaped).
func (s *Server) runtimeLocked(x store.Session) *sessionRuntime {
	opts := claude.Options{Executable: s.cfg.ClaudeExecutable, HostExecutable: s.cfg.HostExecutable, SessionID: x.ClaudeSessionID, Cwd: x.Cwd, Resume: x.Resume, BridgeURL: s.cfg.BridgeURL, ConfigDir: s.cfg.ClaudeConfigDir}
	p := s.effectivePreferences(x)
	opts.PermissionMode, opts.Model, opts.Effort = p.PermissionMode, p.Model, p.Effort
	key := launchKey{x.ClaudeSessionID, opts.PermissionMode, opts.Model, opts.Effort}
	if rt := s.procs[x.ID]; rt != nil {
		if rt.key == key || rt.run != nil || rt.proc == nil || rt.proc.BackgroundTasks() > 0 {
			return rt
		}
		s.closeAsync(s.detachLocked(x.ID))
	}
	rt := &sessionRuntime{id: x.ID, token: store.ID() + store.ID(), key: key, ready: make(chan struct{}), outcome: "completed", idleSince: time.Now()}
	opts.BridgeToken = rt.token
	s.procs[x.ID] = rt
	hooks := claude.ProcessHooks{
		Event:    func(e claude.Event) error { return s.processEvent(rt, e) },
		TurnDone: func(err error) { s.turnDone(rt, err) },
		SelfTurn: func(active bool) { s.selfTurn(rt, active) },
		Exit:     func(err error) { s.processExit(rt, err) },
	}
	// Starting can take seconds (Windows, antivirus); it runs outside s.mu and
	// messages queue meanwhile.
	go func() {
		proc, err := s.startProcess(opts, hooks)
		s.mu.Lock()
		rt.proc = proc
		close(rt.ready)
		if err != nil {
			s.mu.Unlock()
			s.processExit(rt, errors.New("无法启动 Claude Code："+err.Error()))
			return
		}
		detached := rt.stopping
		if !detached {
			s.dispatchLocked(rt)
		}
		s.mu.Unlock()
		if detached {
			proc.Close()
		}
	}()
	return rt
}

func (s *Server) processEvent(rt *sessionRuntime, e claude.Event) error {
	if e.Kind == "session" || e.Kind == "result" {
		if err := s.store.SetResume(rt.id); err != nil {
			return err
		}
	}
	_, err := s.store.Append(rt.id, e.Kind, e.Text, e.Payload)
	return err
}

// queuedMessages lists messages not yet written to Claude, for the web.
func (rt *sessionRuntime) queuedMessages() []store.QueuedMessage {
	out := make([]store.QueuedMessage, len(rt.queue))
	for i, m := range rt.queue {
		out[i] = m.QueuedMessage
	}
	return out
}

// enqueueLocked adds a web message; it reports whether it waits behind other work.
func (s *Server) enqueueLocked(rt *sessionRuntime, m outgoing) bool {
	waiting := rt.inflight || rt.self || len(rt.queue) > 0 || rt.proc == nil
	rt.queue = append(rt.queue, m)
	s.busyLocked(rt)
	s.dispatchLocked(rt)
	return waiting
}

// dispatchLocked writes the next queued message once Claude is free. The
// message enters the history now, before any of its output.
func (s *Server) dispatchLocked(rt *sessionRuntime) {
	if rt.proc == nil || rt.stopping || rt.inflight || rt.self || len(rt.queue) == 0 {
		return
	}
	m := rt.queue[0]
	rt.queue = rt.queue[1:]
	rt.inflight = true
	if _, e := s.store.Append(rt.id, "user", m.Text, m.Payload); e != nil {
		log.Printf("save message: %v", e)
	}
	proc := rt.proc
	go func() { // outside s.mu: the pipe can block while Claude's output waits for the reader
		if err := proc.Send(m.prompt); err != nil {
			s.mu.Lock()
			defer s.mu.Unlock()
			if rt.inflight && !rt.stopping {
				rt.inflight = false
				s.recordOutcomeLocked(rt, err)
				s.dispatchLocked(rt)
				s.settleLocked(rt)
			}
		}
	}()
}

// busyLocked starts an execution for the session if none is running.
func (s *Server) busyLocked(rt *sessionRuntime) {
	if rt.run != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &execution{ctx: ctx, sessionID: rt.id, token: rt.token, done: make(chan struct{})}
	run.cancel = func() { // ends the session's process; used by delete and shutdown
		cancel()
		go s.terminate(rt)
	}
	run.interrupt = func() { s.interruptLocked(rt) }
	rt.run, rt.release, rt.outcome = run, cancel, "completed"
	s.runs[rt.id] = run
	if err := s.store.SetStatus(rt.id, "running"); err != nil {
		log.Printf("save status: %v", err)
	}
}

// settleLocked ends the execution once nothing is left to do.
func (s *Server) settleLocked(rt *sessionRuntime) {
	if rt.run == nil || rt.inflight || rt.self || rt.approvals > 0 || (len(rt.queue) > 0 && !rt.stopping) {
		return
	}
	status := rt.outcome
	if rt.interrupting {
		status = "interrupted"
	}
	run := rt.run
	rt.run, rt.idleSince, rt.interrupting = nil, time.Now(), false
	for rid, p := range s.requests {
		if p.sessionID == rt.id && p.connection == nil {
			if e := s.store.Resolve(rid, "expired"); e != nil {
				log.Printf("expire: %v", e)
			}
			delete(s.requests, rid)
		}
	}
	if e := s.store.SetStatus(rt.id, status); e != nil {
		log.Printf("save status: %v", e)
	}
	if _, e := s.store.Append(rt.id, "status", status, nil); e != nil {
		log.Printf("save status event: %v", e)
	}
	if s.runs[rt.id] == run {
		delete(s.runs, rt.id)
	}
	rt.release() // open bridge calls of this execution see it ended
	close(run.done)
}

// recordOutcomeLocked sets the outcome of a finished turn (or of the process
// ending); errors that interrupt or close caused are expected, others are saved.
func (s *Server) recordOutcomeLocked(rt *sessionRuntime, err error) {
	switch {
	case rt.interrupting || rt.stopping: // an interrupted turn may still report success
		rt.outcome, rt.interrupting = "interrupted", false
	case err == nil:
		rt.outcome = "completed"
	default:
		rt.outcome = "failed"
		if _, e := s.store.Append(rt.id, "error", err.Error(), nil); e != nil {
			log.Printf("save execution error: %v", e)
		}
	}
}

func (s *Server) turnDone(rt *sessionRuntime, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt.inflight = false
	s.recordOutcomeLocked(rt, err)
	s.dispatchLocked(rt)
	s.settleLocked(rt)
}

func (s *Server) selfTurn(rt *sessionRuntime, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt.self = active
	if active && !rt.stopping {
		s.busyLocked(rt)
	}
	s.dispatchLocked(rt)
	s.settleLocked(rt)
}

func (s *Server) processExit(rt *sessionRuntime, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.procs[rt.id] == rt {
		delete(s.procs, rt.id)
	}
	if rt.run == nil {
		return
	}
	if err == nil && !rt.stopping {
		err = errors.New("Claude Code exited")
	}
	unsent := rt.queue
	rt.queue = nil
	s.recordOutcomeLocked(rt, err)
	if rt.stopping {
		rt.outcome = "interrupted"
	}
	for _, m := range unsent { // keep them in the history, marked as not run
		if _, e := s.store.Append(rt.id, "user", m.Text, m.Payload); e != nil {
			log.Printf("save message: %v", e)
		}
	}
	if len(unsent) > 0 && !rt.stopping {
		if _, e := s.store.Append(rt.id, "error", "Claude Code 已退出，以上排队消息未执行，请重新发送。", nil); e != nil {
			log.Printf("save execution error: %v", e)
		}
	}
	rt.inflight, rt.self, rt.approvals, rt.stopping = false, false, 0, true
	s.settleLocked(rt)
}

// interruptLocked stops the running turn without ending the process; queued
// messages run afterwards. Open approvals are denied: Claude abandons the tool
// call anyway. A second stop, or a turn still running after interruptGrace,
// ends the process.
func (s *Server) interruptLocked(rt *sessionRuntime) {
	if !rt.inflight && !rt.self {
		return // nothing runs; queued messages wait for a starting process
	}
	if rt.interrupting {
		go s.terminate(rt)
		return
	}
	rt.interrupting = true
	for rid, p := range s.requests {
		if p.sessionID == rt.id && p.connection == nil {
			select {
			case p.channel <- answer{Decision: "deny"}:
			default:
			}
			if e := s.store.Resolve(rid, "expired"); e != nil {
				log.Printf("expire: %v", e)
			}
			delete(s.requests, rid)
		}
	}
	run := rt.run
	time.AfterFunc(interruptGrace, func() {
		s.mu.Lock()
		stuck := rt.run == run && run != nil && rt.interrupting
		s.mu.Unlock()
		if stuck {
			s.terminate(rt)
		}
	})
	if proc := rt.proc; proc != nil {
		go func() {
			if err := proc.Interrupt(); err != nil {
				log.Printf("interrupt %s: %v", rt.id, err)
			}
		}()
	}
}

// detachLocked removes the session's runtime so nothing new is sent to it;
// the caller closes it outside s.mu.
func (s *Server) detachLocked(id string) *sessionRuntime {
	rt := s.procs[id]
	if rt == nil {
		return nil
	}
	delete(s.procs, id)
	rt.stopping = true
	return rt
}

// closeRuntime ends a detached runtime's process and waits for it.
func closeRuntime(rt *sessionRuntime) {
	if rt == nil {
		return
	}
	<-rt.ready
	if rt.proc != nil {
		rt.proc.Close()
	}
}

// closeAsync closes a detached runtime in the background; shutdown waits for it.
func (s *Server) closeAsync(rt *sessionRuntime) {
	if rt == nil {
		return
	}
	s.closers.Add(1)
	go func() {
		defer s.closers.Done()
		closeRuntime(rt)
	}()
}

// terminate ends rt's process if it is still the session's (stop, delete, shutdown).
func (s *Server) terminate(rt *sessionRuntime) {
	s.mu.Lock()
	if s.procs[rt.id] == rt {
		s.detachLocked(rt.id)
	}
	rt.stopping = true
	s.mu.Unlock()
	closeRuntime(rt)
}

// reapIdle checks every interval for processes idle longer than s.idleTimeout
// with no background task left and closes them, until stop is closed.
func (s *Server) reapIdle(stop <-chan struct{}, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		var idle []*sessionRuntime
		s.mu.Lock()
		for id, rt := range s.procs {
			if rt.run == nil && rt.proc != nil && time.Since(rt.idleSince) > s.idleTimeout && rt.proc.BackgroundTasks() == 0 {
				idle = append(idle, s.detachLocked(id)) // detached under s.mu: no message can slip in
			}
		}
		s.mu.Unlock()
		for _, rt := range idle {
			closeRuntime(rt)
		}
	}
}

// closeProcesses ends every session process (host shutdown): running turns
// are interrupted first so tools stop before the grace period.
func (s *Server) closeProcesses(ctx context.Context) error {
	s.mu.Lock()
	for id, rt := range s.procs {
		if rt.run != nil && rt.proc != nil {
			proc := rt.proc
			go func() { _ = proc.Interrupt() }()
		}
		s.closeAsync(s.detachLocked(id))
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.closers.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.New("waiting for Claude processes to exit")
	}
}

// bridgeRuntimeLocked finds the process a bridge token belongs to and keeps
// the session busy while the request is open, also between turns (background
// agents ask for approval after the turn that started them ended).
func (s *Server) bridgeRuntimeLocked(token string) *sessionRuntime {
	for _, rt := range s.procs {
		if equalToken(token, rt.token) {
			rt.approvals++
			s.busyLocked(rt)
			return rt
		}
	}
	return nil
}

func (s *Server) bridgeDoneLocked(rt *sessionRuntime) {
	if rt.approvals > 0 {
		rt.approvals--
	}
	s.settleLocked(rt)
}

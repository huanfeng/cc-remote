package main

import (
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"cc-remote/internal/i18n"
	"cc-remote/internal/listen"
)

// errListenRestart reports a listen change that only a restart can apply:
// it drops the address the attach command and local tools use.
var errListenRestart = errors.New("listen change needs a restart")

// listenerSet serves the public listeners of one http.Server and lets a
// reload ("service config --listen") change them while the host runs.
type listenerSet struct {
	srv  *http.Server
	errs chan<- error
	// keep is the address the attach command points at; a reload must keep it.
	keep string

	mu     sync.Mutex
	order  []string                  // resolved addresses, in configured order
	active map[string]*trackListener // by resolved address
}

func newListenerSet(srv *http.Server, errs chan<- error) *listenerSet {
	return &listenerSet{srv: srv, errs: errs, active: map[string]*trackListener{}}
}

// listen binds addr without serving it yet.
func listenOn(addr string) (*trackListener, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, i18n.Errorf("listen on %s: %w", "监听 %s 失败：%w", addr, err)
	}
	return &trackListener{Listener: l, conns: &connSet{conns: map[*trackConn]struct{}{}}}, nil
}

// add records a bound listener; serve starts them.
func (s *listenerSet) add(addr string, l *trackListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, addr)
	s.active[addr] = l
}

func (s *listenerSet) serve() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, addr := range s.order {
		s.serveLocked(addr, s.active[addr])
	}
}

// serveLocked serves l; an error is reported unless a reload removed l.
func (s *listenerSet) serveLocked(addr string, l *trackListener) {
	go func() {
		err := s.srv.Serve(l)
		s.mu.Lock()
		current := s.active[addr] == l
		s.mu.Unlock()
		if current {
			select {
			case s.errs <- err:
			default:
			}
		}
	}()
}

// listeners returns the bound listeners in configured order.
func (s *listenerSet) listeners() []net.Listener {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]net.Listener, 0, len(s.order))
	for _, addr := range s.order {
		out = append(out, s.active[addr])
	}
	return out
}

func (s *listenerSet) bound() []string {
	var out []string
	for _, l := range s.listeners() {
		out = append(out, l.Addr().String())
	}
	return out
}

// closeAll closes every listener (the server's Shutdown closes them too).
func (s *listenerSet) closeAll() {
	for _, l := range s.listeners() {
		l.Close()
	}
}

// connGrace is how long connections through a removed address stay open, so
// the reply to the reload that removed it still goes out.
var connGrace = time.Second

// conflicts reports whether a and b cannot be bound together: the same port
// with a wildcard on either side.
func conflicts(a, b string) bool {
	ha, pa, errA := net.SplitHostPort(a)
	hb, pb, errB := net.SplitHostPort(b)
	return errA == nil && errB == nil && pa == pb && (listen.IsWildcard(ha) || listen.IsWildcard(hb))
}

// apply makes the resolved addresses the bound set. A removed listener that
// conflicts with a new address closes first; the others close, with their
// connections, once every new address is bound. When a bind fails nothing
// else changes and the conflicting listeners are bound again.
func (s *listenerSet) apply(addrs []string) ([]string, error) {
	s.mu.Lock()
	if s.keep != "" && !slices.Contains(addrs, s.keep) {
		s.mu.Unlock()
		return nil, errListenRestart
	}
	var removed, added []string
	for _, addr := range s.order {
		if !slices.Contains(addrs, addr) {
			removed = append(removed, addr)
		}
	}
	for _, addr := range addrs {
		if s.active[addr] == nil && !slices.Contains(added, addr) {
			added = append(added, addr)
		}
	}
	old := map[string]*trackListener{}
	var cleared []string
	for _, addr := range removed {
		old[addr] = s.active[addr]
		if slices.ContainsFunc(added, func(a string) bool { return conflicts(a, addr) }) {
			s.active[addr].stop()
			cleared = append(cleared, addr)
		}
	}
	opened := map[string]*trackListener{}
	var bindErr error
	for _, addr := range added {
		l, err := listenOn(addr)
		if err != nil {
			bindErr = err
			break
		}
		opened[addr] = l
	}
	if bindErr != nil {
		for _, l := range opened {
			l.Close()
		}
		var lost []string
		for _, addr := range cleared {
			l, err := listenOn(addr)
			if err != nil {
				delete(s.active, addr)
				old[addr].dropConns(connGrace)
				lost = append(lost, addr)
				continue
			}
			l.conns = old[addr].conns // a later removal still ends them
			s.active[addr] = l
			s.serveLocked(addr, l)
		}
		s.order = slices.DeleteFunc(s.order, func(a string) bool { return slices.Contains(lost, a) })
		s.mu.Unlock()
		if len(lost) > 0 {
			bindErr = i18n.Errorf("%w; could not listen on %s again", "%w；无法重新监听 %s", bindErr, strings.Join(lost, ", "))
		}
		return s.bound(), bindErr
	}
	for _, addr := range removed {
		delete(s.active, addr)
		old[addr].stop()
		old[addr].dropConns(connGrace)
	}
	for addr, l := range opened {
		s.active[addr] = l
		s.serveLocked(addr, l)
	}
	s.order = slices.Compact(slices.Clone(addrs))
	s.order = slices.DeleteFunc(s.order, func(a string) bool { return s.active[a] == nil })
	s.mu.Unlock()
	return s.bound(), nil
}

// trackListener remembers its open connections, so removing a listen address
// also ends the sessions (event streams, keep-alive) that came in through it.
type trackListener struct {
	net.Listener
	conns  *connSet // shared with the listener a failed reload binds again
	mu     sync.Mutex
	closed bool
}

// connSet is the open connections accepted on one address.
type connSet struct {
	mu    sync.Mutex
	conns map[*trackConn]struct{}
}

func (l *trackListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	tc := &trackConn{Conn: c, set: l.conns}
	l.mu.Lock()
	closed := l.closed
	if !closed {
		l.conns.mu.Lock()
		l.conns.conns[tc] = struct{}{}
		l.conns.mu.Unlock()
	}
	l.mu.Unlock()
	if closed {
		c.Close()
		return nil, net.ErrClosed
	}
	return tc, nil
}

// stop closes the listening socket; accepted connections stay open.
func (l *trackListener) stop() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.Listener.Close()
}

// dropConns closes the accepted connections after a delay.
func (l *trackListener) dropConns(after time.Duration) {
	time.AfterFunc(after, func() {
		s := l.conns
		s.mu.Lock()
		conns := s.conns
		s.conns = map[*trackConn]struct{}{}
		s.mu.Unlock()
		for c := range conns {
			c.Conn.Close()
		}
	})
}

type trackConn struct {
	net.Conn
	set  *connSet
	once sync.Once
}

func (c *trackConn) Close() error {
	c.once.Do(func() {
		c.set.mu.Lock()
		delete(c.set.conns, c)
		c.set.mu.Unlock()
	})
	return c.Conn.Close()
}

var _ interface{ CloseWrite() error } = (*trackConn)(nil)

// CloseWrite lets net/http half-close before closing, so a client still
// sending a rejected request body reads the reply instead of a reset.
func (c *trackConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// ReadFrom keeps the sendfile path of the underlying TCP connection.
func (c *trackConn) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := c.Conn.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(c.Conn, r)
}

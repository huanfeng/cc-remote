package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cc-remote/internal/terminal"
	"github.com/coder/websocket"
)

type projectShellRun struct {
	id          string
	pty         *terminal.ProjectShell
	released    chan struct{}
	stopped     atomic.Bool
	interrupted atomic.Bool

	mu    sync.Mutex // serializes ownership changes with PTY input and resize
	peers map[chan bool]bool
	owner chan bool
}

func (r *projectShellRun) finished() bool {
	select {
	case <-r.pty.Done():
		return true
	default:
		return false
	}
}

func (r *projectShellRun) state() string {
	if r.stopped.Load() {
		return "stopped"
	}
	if r.finished() {
		if r.interrupted.Load() {
			return "interrupted"
		}
		return "exited"
	}
	return "running"
}

func (r *projectShellRun) addPeer(peer chan bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	const maxShellPeers = 8
	if len(r.peers) >= maxShellPeers || r.state() != "running" {
		return false
	}
	if r.peers == nil {
		r.peers = make(map[chan bool]bool)
	}
	r.peers[peer] = true
	if r.owner == nil {
		r.owner = peer
	}
	peer <- r.owner == peer
	return true
}

func (r *projectShellRun) removePeer(peer chan bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers, peer)
	if r.owner == peer {
		r.owner = nil
	}
	// A quick hide and reopen can briefly overlap two connections. Give a
	// lone remaining viewer control once the old connection has detached.
	if r.owner == nil && len(r.peers) == 1 {
		for remaining := range r.peers {
			r.owner = remaining
			publishProjectShellControl(remaining, true)
		}
	}
}

func (r *projectShellRun) takeControl(peer chan bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.peers[peer] || r.state() != "running" {
		return
	}
	r.owner = peer
	for p := range r.peers {
		publishProjectShellControl(p, p == peer)
	}
}

func publishProjectShellControl(peer chan bool, held bool) {
	select {
	case <-peer:
	default:
	}
	peer <- held
}

func (r *projectShellRun) withControl(peer chan bool, action func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.owner == peer && r.state() == "running" {
		action()
	}
}

const projectShellStreamPrefix = "/api/project-shells/"

func projectShellStreamTicket(r *http.Request) bool {
	path, ok := strings.CutPrefix(r.URL.Path, projectShellStreamPrefix)
	if !ok || r.Method != http.MethodGet || r.URL.Query().Get("ticket") == "" {
		return false
	}
	parts := strings.Split(path, "/")
	return len(parts) == 4 && parts[0] != "" && parts[1] == "runs" && parts[2] != "" && parts[3] == "stream"
}

func (s *Server) projectShellTicket(w http.ResponseWriter, r *http.Request) {
	if !s.projectShellAvailable(w, r) {
		return
	}
	if run := s.findProjectShellRun(w, r); run != nil {
		ticket := s.projectShells.tickets.add(downloadTicket{session: r.PathValue("id"), path: run.id, expires: time.Now().Add(streamTicketTTL)})
		respond(w, http.StatusOK, map[string]string{"ticket": ticket})
	}
}

type projectShellFrame struct {
	Type   string  `json:"type"`
	Seq    *uint64 `json:"seq,omitempty"`
	Cols   int     `json:"cols,omitempty"`
	Rows   int     `json:"rows,omitempty"`
	Held   *bool   `json:"held,omitempty"`
	Detail string  `json:"detail,omitempty"`
}

func writeProjectShellFrame(ctx context.Context, ws *websocket.Conn, frame projectShellFrame) error {
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return ws.Write(ctx, websocket.MessageText, b)
}

func writeProjectShellEvent(ctx context.Context, ws *websocket.Conn, e terminal.ProjectShellEvent) error {
	if e.Cols > 0 {
		return writeProjectShellFrame(ctx, ws, projectShellFrame{Type: "size", Seq: &e.Sequence, Cols: e.Cols, Rows: e.Rows})
	}
	data := make([]byte, 8+len(e.Data))
	binary.BigEndian.PutUint64(data, e.Sequence)
	copy(data[8:], e.Data)
	return ws.Write(ctx, websocket.MessageBinary, data)
}

func writeProjectShellReplay(ctx context.Context, ws *websocket.Conn, replay terminal.ProjectShellReplay) error {
	if err := writeProjectShellReplayStart(ctx, ws, replay); err != nil {
		return err
	}
	for _, event := range replay.Events {
		if err := writeProjectShellEvent(ctx, ws, event); err != nil {
			return err
		}
	}
	return writeProjectShellFrame(ctx, ws, projectShellFrame{Type: "ready", Seq: &replay.Through})
}

func writeProjectShellReplayStart(ctx context.Context, ws *websocket.Conn, replay terminal.ProjectShellReplay) error {
	if replay.Reset {
		if err := writeProjectShellFrame(ctx, ws, projectShellFrame{Type: "reset", Seq: &replay.ScreenSequence}); err != nil {
			return err
		}
	}
	if err := writeProjectShellFrame(ctx, ws, projectShellFrame{Type: "size", Cols: replay.Cols, Rows: replay.Rows}); err != nil {
		return err
	}
	if len(replay.Screen) != 0 {
		return writeProjectShellEvent(ctx, ws, terminal.ProjectShellEvent{Sequence: replay.ScreenSequence, Data: replay.Screen})
	}
	return nil
}

func (s *Server) projectShellStream(w http.ResponseWriter, req *http.Request) {
	if !s.projectShellAvailable(w, req) {
		return
	}
	t, ok := s.projectShells.tickets.take(req.URL.Query().Get("ticket"))
	if !ok || t.session != req.PathValue("id") || t.path != req.PathValue("run") {
		fail(w, http.StatusUnauthorized, s.lang(req).T("The terminal link has expired", "终端连接票据已过期"))
		return
	}
	after, err := strconv.ParseUint(req.URL.Query().Get("after"), 10, 53)
	if err != nil {
		fail(w, http.StatusBadRequest, s.lang(req).T("Invalid terminal output cursor", "终端输出位置无效"))
		return
	}
	run := s.findProjectShellRun(w, req)
	if run == nil {
		return
	}
	peer := make(chan bool, 1)
	if !run.addPeer(peer) {
		fail(w, http.StatusServiceUnavailable, s.lang(req).T("The terminal is unavailable or has too many viewers", "终端不可用或连接数已达上限"))
		return
	}
	defer run.removePeer(peer)
	// Handler already checks Origin, including configured standalone frontends.
	ws, err := websocket.Accept(w, req, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(256 << 10)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	ready := make(chan struct{})
	var flow *projectShellFlow
	if req.URL.Query().Get("flow") == "1" {
		flow = &projectShellFlow{notify: make(chan struct{}, 1)}
	}
	go run.readSocket(ctx, ws, peer, ready, cancel, flow)
	go pingProjectShell(ctx, ws, cancel)
	if flow != nil {
		run.streamWithFlow(ctx, ws, peer, ready, after, flow)
		return
	}
	replay, events, unsubscribe := run.pty.Subscribe(after)
	defer unsubscribe()
	if err := writeProjectShellReplay(ctx, ws, replay); err != nil {
		return
	}
	close(ready)
	for {
		select {
		case <-ctx.Done():
			return
		case held := <-peer:
			if err := writeProjectShellFrame(ctx, ws, projectShellFrame{Type: "control", Held: &held}); err != nil {
				return
			}
		case event, open := <-events:
			if !open {
				if state := run.state(); state != "running" {
					_ = writeProjectShellFrame(ctx, ws, projectShellFrame{Type: "exit", Detail: state})
					_ = ws.Close(statusExited, state)
				}
				return
			}
			if err := writeProjectShellEvent(ctx, ws, event); err != nil {
				return
			}
		}
	}
}

func (r *projectShellRun) readSocket(ctx context.Context, ws *websocket.Conn, peer chan bool, ready <-chan struct{}, cancel context.CancelFunc, flow *projectShellFlow) {
	defer cancel()
	for {
		kind, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var message projectShellFrame
		if kind == websocket.MessageText {
			if json.Unmarshal(data, &message) != nil {
				continue
			}
			if message.Type == "ack" && flow != nil && message.Seq != nil {
				flow.ack(*message.Seq)
				continue
			}
		}
		select {
		case <-ready:
		default:
			continue
		}
		if kind == websocket.MessageBinary {
			r.withControl(peer, func() { _ = r.pty.Write(data) })
			continue
		}
		switch message.Type {
		case "take":
			r.takeControl(peer)
		case "resize":
			r.withControl(peer, func() { _ = r.pty.Resize(message.Cols, message.Rows) })
		case "redraw":
			r.withControl(peer, func() { _ = r.pty.Redraw() })
		}
	}
}

func pingProjectShell(ctx context.Context, ws *websocket.Conn, cancel context.CancelFunc) {
	ticker := time.NewTicker(streamPing)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, done := context.WithTimeout(ctx, streamPing)
			err := ws.Ping(pingCtx)
			done()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

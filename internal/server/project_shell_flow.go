package server

import (
	"context"
	"encoding/binary"
	"sync/atomic"
	"time"

	"cc-remote/internal/terminal"
	"github.com/coder/websocket"
)

const (
	projectShellOutputWindow = 256 << 10
	projectShellOutputFrame  = 64 << 10
	projectShellAckTimeout   = 45 * time.Second
)

// A client opts in to limiting WS output by acknowledging bytes parsed by
// xterm. The socket's reader continues handling input while the writer waits.
type projectShellFlow struct {
	sent, applied atomic.Uint64
	notify        chan struct{}
}

func (f *projectShellFlow) ack(seq uint64) {
	if seq <= f.applied.Load() || seq > f.sent.Load() {
		return
	}
	f.applied.Store(seq)
	select {
	case f.notify <- struct{}{}:
	default:
	}
}

func writeShellOutputBatch(ctx context.Context, ws *websocket.Conn, events []terminal.ProjectShellEvent, output []byte) error {
	var sequence uint64
	flush := func() error {
		if len(output) == 8 {
			return nil
		}
		binary.BigEndian.PutUint64(output, sequence)
		err := ws.Write(ctx, websocket.MessageBinary, output)
		output = output[:8]
		return err
	}
	for _, e := range events {
		if e.Cols != 0 || len(output)+len(e.Data) > 8+projectShellOutputFrame {
			if err := flush(); err != nil {
				return err
			}
		}
		if e.Cols != 0 {
			if err := writeProjectShellEvent(ctx, ws, e); err != nil {
				return err
			}
			continue
		}
		output = append(output, e.Data...)
		sequence = e.Sequence
	}
	return flush()
}

func writeShellControl(ctx context.Context, ws *websocket.Conn, held bool) bool {
	return writeProjectShellFrame(ctx, ws, projectShellFrame{Type: "control", Held: &held}) == nil
}

func (f *projectShellFlow) wait(ctx context.Context, ws *websocket.Conn, control <-chan bool, cursor uint64) bool {
	if f.applied.Load() >= cursor {
		return true
	}
	timer := time.NewTimer(projectShellAckTimeout)
	defer timer.Stop()
	for f.applied.Load() < cursor {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		case <-f.notify:
		case held := <-control:
			if !writeShellControl(ctx, ws, held) {
				return false
			}
		}
	}
	return true
}

func (r *projectShellRun) streamWithFlow(ctx context.Context, ws *websocket.Conn, peer <-chan bool, ready chan<- struct{}, cursor uint64, flow *projectShellFlow) {
	wake, unsubscribe := r.pty.WatchOutput()
	defer unsubscribe()
	if err := writeProjectShellFrame(ctx, ws, projectShellFrame{Type: "flow"}); err != nil {
		return
	}
	first := true
	var initialHead uint64
	var control <-chan bool
	output := make([]byte, 8, 8+projectShellOutputFrame)
	for ctx.Err() == nil {
		select {
		case held := <-control:
			if !writeShellControl(ctx, ws, held) {
				return
			}
		default:
		}

		batch := r.pty.ReadOutput(cursor, projectShellOutputWindow)
		if first {
			initialHead = batch.Latest
		}
		flow.sent.Store(batch.Through)
		if first || batch.Reset {
			if err := writeProjectShellReplayStart(ctx, ws, batch); err != nil {
				return
			}
		}
		if err := writeShellOutputBatch(ctx, ws, batch.Events, output); err != nil {
			return
		}
		if control == nil && batch.Through >= initialHead {
			if err := writeProjectShellFrame(ctx, ws, projectShellFrame{Type: "ready", Seq: &batch.Through}); err != nil {
				return
			}
			close(ready)
			control = peer
		}
		if batch.Reset || len(batch.Events) != 0 {
			if !flow.wait(ctx, ws, control, batch.Through) {
				return
			}
		}
		first = false
		cursor = batch.Through
		if batch.More {
			continue
		}
		if r.finished() {
			// The process may have exited between reading the batch and checking Done.
			if r.pty.ReadOutput(cursor, 1).Latest > cursor {
				continue
			}
			state := r.state()
			_ = writeProjectShellFrame(ctx, ws, projectShellFrame{Type: "exit", Detail: state})
			_ = ws.Close(statusExited, state)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-r.pty.Done():
		case held := <-control:
			if !writeShellControl(ctx, ws, held) {
				return
			}
		}
	}
}

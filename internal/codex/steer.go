package codex

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrSteerRejected means no input was accepted. Other errors have ambiguous
// delivery and must not be retried automatically as a new turn.
var ErrSteerRejected = errors.New("Codex did not accept the additional instruction")

// Steer appends input to this exact in-flight turn. It never falls back to
// turn/start: a completion race must not unexpectedly start more work.
func (p *Process) Steer(text string) error {
	if strings.TrimSpace(text) == "" || strings.HasPrefix(strings.TrimSpace(text), "/") {
		return fmt.Errorf("%w: only ordinary text is supported", ErrSteerRejected)
	}
	p.mu.Lock()
	thread, turn := p.threadID, p.turnID
	active := p.turnDone != nil && turn != "" && !p.compacting && !p.interruptPending
	p.mu.Unlock()
	if !active {
		return fmt.Errorf("%w: no active turn", ErrSteerRejected)
	}
	ctx, cancel := context.WithTimeout(p.rpc.ctx, startupTimeout)
	defer cancel()
	var response struct {
		TurnID string `json:"turnId"`
	}
	err := p.rpc.call(ctx, "turn/steer", map[string]any{"threadId": thread, "expectedTurnId": turn, "input": turnInput(text)}, &response)
	if err == nil && response.TurnID == turn {
		return nil
	}
	var rejected *rpcError
	if errors.As(err, &rejected) {
		return fmt.Errorf("%w: %v", ErrSteerRejected, err)
	}
	// A lost acknowledgement may have delivered the instruction. End this
	// connection rather than allow another submit against uncertain live state.
	p.rpc.kill()
	if err == nil {
		err = errors.New("unexpected turn ID in acknowledgement")
	}
	return fmt.Errorf("Codex additional instruction delivery is unknown; do not resend automatically: %w", err)
}

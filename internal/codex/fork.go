package codex

import (
	"context"
	"errors"
	"fmt"

	"cc-remote/internal/claude"
)

// ForkNativeSession copies the thread through lastTurnID, or its full
// conversation when no boundary is supplied. It does not send a model turn.
func ForkNativeSession(ctx context.Context, o claude.Options, lastTurnID string) (string, error) {
	if !nativeUUID.MatchString(o.SessionID) {
		return "", errors.New("invalid source Codex thread ID")
	}
	if lastTurnID != "" && !nativeUUID.MatchString(lastTurnID) {
		return "", errors.New("invalid Codex fork turn ID")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r, err := startRPC(o.Executable, o.ConfigDir, o.Cwd, nil, nil, o)
	if err != nil {
		return "", err
	}
	defer r.close()
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	if err := r.initialize(ctx); err != nil {
		return "", fmt.Errorf("initialize Codex: %w", err)
	}

	params := threadOptions(o)
	params["threadId"], params["excludeTurns"] = o.SessionID, true
	params["deferGoalContinuation"] = true
	if lastTurnID != "" {
		params["lastTurnId"] = lastTurnID
	}
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := r.call(ctx, "thread/fork", params, &response); err != nil {
		return "", fmt.Errorf("fork Codex thread: %w", err)
	}
	childID := response.Thread.ID
	if !nativeUUID.MatchString(childID) || childID == o.SessionID {
		return "", errors.New("Codex did not return a new thread ID")
	}
	// Older app servers can ignore an unfamiliar lastTurnId field. Check the
	// resulting history so an edited prompt cannot be sent with later context.
	if lastTurnID != "" {
		var page struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := r.call(ctx, "thread/turns/list", map[string]any{"threadId": childID, "sortDirection": "desc", "limit": 1}, &page); err != nil {
			return "", fmt.Errorf("verify Codex fork turn: %w", err)
		}
		if len(page.Data) != 1 || page.Data[0].ID != lastTurnID {
			return "", errors.New("Codex fork did not stop at the requested turn")
		}
	}
	return childID, nil
}

type nativeForkTurn struct {
	active     string
	prior      string
	start      int
	priorKnown bool
}

func (f *nativeForkTurn) observe(kind, id string, events []claude.Event) {
	if id == "" {
		return
	}
	switch kind {
	case "task_started":
		if f.active != "" {
			f.priorKnown = false
		}
		f.active, f.start = id, len(events)
	case "task_complete", "turn_aborted":
		if f.active != id {
			return
		}
		userSeen := false
		for i := f.start; i < len(events); i++ {
			ev := &events[i]
			switch ev.Kind {
			case "user":
				if userSeen {
					continue // An input steered into this turn has no fork boundary.
				}
				userSeen = true
			case "assistant":
			default:
				continue
			}
			ev.NativeAnchor, ev.NativeParentAnchor, ev.NativeParentKnown = id, f.prior, f.priorKnown
		}
		f.active, f.prior, f.priorKnown = "", id, true
	}
}

package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"cc-remote/internal/claude"
)

type rolloutReadBounds struct {
	before *uint64
	read   int64
}

// Paginated Codex forks keep their own records and a reference to a prefix of
// the parent's rollout. Read that prefix from the same Codex home, so later
// turns in the parent do not appear in the fork.
type linkedRolloutReader struct {
	root      *os.Root
	remaining int64
	turn      nativeForkTurn
	seen      map[string]bool
}

func (r *linkedRolloutReader) read(path string, metadata claude.NativeSession, before *uint64) (claude.NativeSession, []claude.Event, error) {
	id := strings.ToLower(metadata.ID)
	if r.seen[id] || len(r.seen) >= 64 {
		return claude.NativeSession{}, nil, errors.New("invalid Codex fork history chain")
	}
	r.seen[id] = true
	var parent claude.NativeSession
	var inherited []claude.Event
	if metadata.ForkedFromID != "" && metadata.ForkedFromOrdinal != nil {
		parentID := metadata.ForkedFromID
		if !nativeUUID.MatchString(parentID) {
			return claude.NativeSession{}, nil, errors.New("invalid Codex fork parent")
		}
		parentPath, parentMeta, err := nativeHistoryPath(context.Background(), r.root, parentID)
		if err != nil {
			return claude.NativeSession{}, nil, fmt.Errorf("read Codex fork parent: %w", err)
		}
		cutoff := metadata.ForkedFromOrdinal
		if before != nil && *before < *cutoff {
			cutoff = before
		}
		parent, inherited, err = r.read(parentPath, parentMeta, cutoff)
		if err != nil {
			return claude.NativeSession{}, nil, err
		}
	}
	if r.remaining <= 0 {
		metadata.Truncated = true
		return metadata, inherited, nil
	}
	f, err := openNative(r.root, path)
	if err != nil {
		return claude.NativeSession{}, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return claude.NativeSession{}, nil, err
	}
	// A fork may split off before a parent turn finished. Do not associate
	// any child messages with that unfinished turn.
	if r.turn.active != "" {
		r.turn.active, r.turn.priorKnown = "", false
	}
	bounds := rolloutReadBounds{before: before}
	session, events, err := parseNativeFrom(f, info, true, r.remaining, newNativeSession(info), 0, &r.turn, &bounds)
	if err != nil {
		return claude.NativeSession{}, nil, err
	}
	r.remaining -= bounds.read
	session.Truncated = session.Truncated || parent.Truncated
	if session.ID == "" {
		// The cutoff can lie entirely in an ancestor of this segment.
		return parent, inherited, nil
	}
	if session.LastConversationAt == "" {
		session.LastConversationAt, session.ConversationTimeEstimated = parent.LastConversationAt, parent.ConversationTimeEstimated
		if session.Title == "Codex session" && parent.Title != "" {
			session.Title = parent.Title
		}
	}
	return session, append(inherited, events...), nil
}

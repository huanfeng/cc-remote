package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"cc-remote/internal/claude"
)

// SearchNative scans user and assistant messages across the whole rollout.
func SearchNative(ctx context.Context, configDir, id string, visit func(claude.Event, string) bool) (claude.NativeSession, error) {
	f, session, err := openNativeSearch(ctx, configDir, id)
	if err != nil {
		return claude.NativeSession{}, err
	}
	defer f.Close()
	return session, claude.ScanNativeMessages(ctx, f, codexSearchMessages, visit)
}

// NativeSearchContext returns messages around a search hit and its index.
func NativeSearchContext(ctx context.Context, configDir, id, cursor string) (claude.NativeSession, []claude.Event, int, error) {
	f, session, err := openNativeSearch(ctx, configDir, id)
	if err != nil {
		return claude.NativeSession{}, nil, 0, err
	}
	defer f.Close()
	events, focus, err := claude.ReadNativeMessageContext(ctx, f, codexSearchMessages, cursor)
	return session, events, focus, err
}

func openNativeSearch(ctx context.Context, configDir, id string) (*os.File, claude.NativeSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, claude.NativeSession{}, err
	}
	if !nativeUUID.MatchString(id) {
		return nil, claude.NativeSession{}, errors.New("invalid Codex session UUID")
	}
	root, err := nativeRoot(configDir)
	if err != nil {
		return nil, claude.NativeSession{}, err
	}
	defer root.Close()
	path, session, err := nativeHistoryPath(ctx, root, id)
	if err != nil {
		return nil, claude.NativeSession{}, err
	}
	f, err := openNative(root, path)
	if err != nil {
		return nil, claude.NativeSession{}, err
	}
	if title := nativeTitles(root)[strings.ToLower(session.ID)]; title != "" {
		session.Title = title
	}
	return f, session, nil
}

// SearchNativeAll resolves each eligible rollout once for a host-wide search.
func SearchNativeAll(ctx context.Context, configDir string, include func(claude.NativeSession) bool, visit func(claude.NativeSession, claude.Event, string) bool) error {
	root, err := nativeRoot(configDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	paths, err := nativeFiles(root)
	if err != nil {
		return err
	}
	type candidate struct {
		path         string
		session      claude.NativeSession
		matchingName bool
	}
	titles := nativeTitles(root)
	candidates := make([]candidate, 0, len(paths))
	seen := make(map[string]int, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		session, err := nativeMetadataCache.metadata(root, path)
		if err != nil || !nativeUUID.MatchString(session.ID) {
			continue
		}
		id := strings.ToLower(session.ID)
		if title := titles[id]; title != "" {
			session.Title = title
		}
		next := candidate{path, session, strings.Contains(strings.ToLower(filepath.Base(path)), id)}
		if index, duplicate := seen[id]; duplicate {
			if next.matchingName && !candidates[index].matchingName {
				candidates[index] = next
			}
		} else {
			seen[id] = len(candidates)
			candidates = append(candidates, next)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return nativeSessionTime(candidates[i].session).After(nativeSessionTime(candidates[j].session))
	})
	var scanError error
	for _, item := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !include(item.session) {
			continue
		}
		f, err := openNative(root, item.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			scanError = errors.Join(scanError, err)
			continue
		}
		keepGoing := true
		err = claude.ScanNativeMessages(ctx, f, codexSearchMessages, func(event claude.Event, cursor string) bool {
			keepGoing = visit(item.session, event, cursor)
			return keepGoing
		})
		f.Close()
		if err != nil {
			scanError = errors.Join(scanError, err)
		}
		if !keepGoing {
			return scanError
		}
	}
	return scanError
}

func codexSearchMessages(line []byte) []claude.Event {
	var r rolloutRecord
	if json.Unmarshal(line, &r) != nil || r.Type != "response_item" || r.Payload.Type != "message" {
		return nil
	}
	event, ok := r.message()
	if !ok {
		return nil
	}
	event.CreatedAt = nativeTimestamp(r.Timestamp)
	return []claude.Event{event}
}

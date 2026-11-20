package claude

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"cc-remote/internal/i18n"
)

const (
	nativeSearchRecordLimit  = 64 << 20
	nativeSearchContextSize  = 10
	nativeSearchContextSpan  = 2 << 20
	nativeSearchNeighborSize = 16 << 10
)

// SearchNative visits user and assistant messages in log order, including
// messages outside the normal history window. The callback may stop the scan.
func SearchNative(ctx context.Context, configDir, id string, visit func(Event, string) bool) (NativeSession, error) {
	if err := ctx.Err(); err != nil {
		return NativeSession{}, err
	}
	f, session, err := openNativeSearch(configDir, id)
	if err != nil {
		return NativeSession{}, err
	}
	defer f.Close()
	return session, ScanNativeMessages(ctx, f, claudeSearchMessages, visit)
}

// SearchNativeAll resolves transcripts once for a host-wide search.
func SearchNativeAll(ctx context.Context, configDir string, include func(NativeSession) bool, visit func(NativeSession, Event, string) bool) error {
	projects, err := projectsDir(configDir)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(projects)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	files, err := nativeFileInfos(configDir)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(files))
	unique := files[:0]
	for _, file := range files {
		id := strings.ToLower(strings.TrimSuffix(filepath.Base(file.path), ".jsonl"))
		if !seen[id] {
			seen[id] = true
			unique = append(unique, file)
		}
	}
	sort.SliceStable(unique, func(i, j int) bool { return unique[i].mod.After(unique[j].mod) })
	var scanError error
	for _, file := range unique {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, session, err := openNativeSearchFile(root, projects, file.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			scanError = errors.Join(scanError, err)
			continue
		}
		keepGoing := true
		if include(session) {
			err = ScanNativeMessages(ctx, f, claudeSearchMessages, func(event Event, cursor string) bool {
				keepGoing = visit(session, event, cursor)
				return keepGoing
			})
		}
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

// NativeSearchContext returns messages around a search hit and its index.
func NativeSearchContext(ctx context.Context, configDir, id, cursor string) (NativeSession, []Event, int, error) {
	if err := ctx.Err(); err != nil {
		return NativeSession{}, nil, 0, err
	}
	f, session, err := openNativeSearch(configDir, id)
	if err != nil {
		return NativeSession{}, nil, 0, err
	}
	defer f.Close()
	events, focus, err := ReadNativeMessageContext(ctx, f, claudeSearchMessages, cursor)
	return session, events, focus, err
}

func openNativeSearch(configDir, id string) (*os.File, NativeSession, error) {
	if !uuidPattern.MatchString(id) {
		return nil, NativeSession{}, errors.New(i18n.T("invalid Claude session UUID", "无效的 Claude 会话 UUID"))
	}
	path, err := nativePath(configDir, id)
	if err != nil {
		return nil, NativeSession{}, err
	}
	projects, err := projectsDir(configDir)
	if err != nil {
		return nil, NativeSession{}, err
	}
	root, err := os.OpenRoot(projects)
	if err != nil {
		return nil, NativeSession{}, err
	}
	defer root.Close()
	return openNativeSearchFile(root, projects, path)
}

func openNativeSearchFile(root *os.Root, projects, path string) (*os.File, NativeSession, error) {
	relative, err := filepath.Rel(projects, path)
	if err != nil {
		return nil, NativeSession{}, err
	}
	dir, err := root.Lstat(filepath.Dir(relative))
	if err != nil {
		return nil, NativeSession{}, err
	}
	info, err := root.Lstat(relative)
	if err != nil {
		return nil, NativeSession{}, err
	}
	if !dir.IsDir() || !info.Mode().IsRegular() {
		return nil, NativeSession{}, os.ErrNotExist
	}
	f, err := root.Open(relative)
	if err != nil {
		return nil, NativeSession{}, err
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		f.Close()
		if err != nil {
			return nil, NativeSession{}, err
		}
		return nil, NativeSession{}, os.ErrNotExist
	}
	session := inspectNativeFile(f, nativeFile{path, opened.Size(), opened.ModTime()})
	session.Provider = "claude"
	return f, session, nil
}

func claudeSearchMessages(line []byte) []Event {
	var session NativeSession
	events := transcriptRecordEvents(line, &session)
	var source struct {
		Timestamp string `json:"timestamp"`
	}
	decoded := false
	for i, event := range events {
		if event.Kind != "user" && event.Kind != "assistant" {
			continue
		}
		if !decoded {
			_ = json.Unmarshal(line, &source)
			decoded = true
		}
		events[i].CreatedAt = nativeTimestamp(source.Timestamp)
	}
	return events
}

// ScanNativeMessages scans a snapshot of a JSONL transcript with bounded
// memory. The cursor identifies a byte offset, a message within the record,
// and a hash that detects records replaced between searching and opening.
func ScanNativeMessages(ctx context.Context, f *os.File, decode func([]byte) []Event, visit func(Event, string) bool) error {
	return scanNativeMessages(ctx, f, 0, -1, decode, visit)
}

// ReadNativeMessageContext reads nearby messages without loading a whole
// transcript. A missing or rewritten hit returns os.ErrNotExist.
func ReadNativeMessageContext(ctx context.Context, f *os.File, decode func([]byte) []Event, cursor string) ([]Event, int, error) {
	offset, rest, ok := strings.Cut(cursor, ".")
	position, err := strconv.ParseInt(offset, 16, 64)
	if !ok || err != nil || position < 0 {
		return nil, 0, os.ErrNotExist
	}
	index, hash, ok := strings.Cut(rest, ".")
	_, indexErr := strconv.ParseUint(index, 16, 32)
	var digest [12]byte
	if !ok || indexErr != nil || len(hash) != hex.EncodedLen(len(digest)) {
		return nil, 0, os.ErrNotExist
	}
	if _, err := hex.Decode(digest[:], []byte(hash)); err != nil {
		return nil, 0, os.ErrNotExist
	}
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if position >= info.Size() {
		return nil, 0, os.ErrNotExist
	}
	start, err := lineStart(f, max(0, position-nativeSearchContextSpan))
	if err != nil {
		return nil, 0, err
	}
	focus := -1
	events := make([]Event, 0, 2*nativeSearchContextSize+1)
	end := position + min(info.Size()-position, nativeSearchContextSpan)
	err = scanNativeMessages(ctx, f, start, end, decode, func(event Event, token string) bool {
		hit := token == cursor
		if focus < 0 && !hit {
			current, _, _ := strings.Cut(token, ".")
			at, _ := strconv.ParseInt(current, 16, 64)
			if at > position {
				return false
			}
			if len(events) == nativeSearchContextSize {
				copy(events, events[1:])
				events = events[:len(events)-1]
			}
		}
		if hit {
			focus = len(events)
		} else {
			event.Payload = nil
			if len(event.Text) > nativeSearchNeighborSize {
				n := nativeSearchNeighborSize
				for !utf8.RuneStart(event.Text[n]) {
					n--
				}
				event.Text = event.Text[:n] + "\n…"
			}
		}
		events = append(events, event)
		return focus < 0 || len(events) <= focus+nativeSearchContextSize
	})
	if err != nil {
		return nil, 0, err
	}
	if focus < 0 {
		return nil, 0, os.ErrNotExist
	}
	return events, focus, nil
}

func scanNativeMessages(ctx context.Context, f *os.File, offset, through int64, decode func([]byte) []Event, visit func(Event, string) bool) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	recordStart, consumed := offset, offset
	scanner := bufio.NewScanner(io.NewSectionReader(f, offset, info.Size()-offset))
	scanner.Buffer(make([]byte, 64<<10), nativeSearchRecordLimit)
	scanner.Split(func(data []byte, eof bool) (int, []byte, error) {
		if through >= 0 && consumed > through {
			return 0, nil, bufio.ErrFinalToken
		}
		advance, record, err := bufio.ScanLines(data, eof)
		recordStart, consumed = consumed, consumed+int64(advance)
		return advance, record, err
	})
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		record := scanner.Bytes()
		events := decode(record)
		var digest []byte
		for i, event := range events {
			if (event.Kind != "user" && event.Kind != "assistant") || strings.TrimSpace(event.Text) == "" {
				continue
			}
			if digest == nil {
				hash := sha256.Sum256(record)
				digest = hash[:12]
			}
			if !visit(event, fmt.Sprintf("%x.%x.%x", recordStart, i, digest)) {
				return nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return ctx.Err()
}

package server

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

type forkUploadTransfer struct {
	s             *Server
	lang          i18n.Lang
	source, child string
	copied        map[string]attachment
	created       []string
	dir           string
	keep          bool
}

func (t *forkUploadTransfer) prepare(ids []string) error {
	for _, id := range ids {
		if _, ok := t.copied[id]; ok {
			return t.lang.Errorf("The same file cannot be attached twice", "不能重复附加同一个文件")
		}
		a, path, err := t.s.loadAttachment(t.lang, t.source, id)
		if err != nil {
			return err
		}
		if _, err = t.copy(a, path); err != nil {
			return t.lang.Errorf("Could not copy a branch attachment", "无法复制分支附件")
		}
	}
	return nil
}

// A fork's visible history should still open its own attachments after the
// original web session is removed. Only copy files owned by its source.
func (t *forkUploadTransfer) rewrite(events []store.Event) error {
	if t.source == "" {
		return nil
	}
	for i := range events {
		ev := &events[i]
		if ev.Kind != "user" {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(ev.Payload, &fields) != nil {
			continue
		}
		var items []attachment
		if json.Unmarshal(fields["attachments"], &items) != nil {
			continue
		}
		for j, v := range items {
			if cached, ok := t.copied[v.ID]; ok {
				items[j] = cached
				continue
			}
			a, path, err := t.s.loadAttachment(t.lang, t.source, v.ID)
			if err != nil {
				// Old histories can refer to uploads already missing from the host.
				continue
			}
			items[j], err = t.copy(a, path)
			if err != nil {
				return err
			}
		}
		fields["attachments"], _ = json.Marshal(items)
		ev.Payload, _ = json.Marshal(fields)
	}
	return nil
}

func (t *forkUploadTransfer) copy(a attachment, path string) (attachment, error) {
	id := a.ID
	base, metadata, err := t.s.uploadPaths(t.lang, t.child, id)
	if err != nil {
		return attachment{}, err
	}
	dir := filepath.Dir(base)
	if t.dir == "" {
		if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
			return attachment{}, err
		}
		if err := os.Mkdir(dir, 0700); err != nil {
			return attachment{}, err
		}
		t.dir = dir
	}
	file := base + strings.TrimPrefix(filepath.Base(path), id)
	source, err := os.Open(path)
	if err != nil {
		return attachment{}, err
	}
	defer source.Close()
	destination, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return attachment{}, err
	}
	t.created = append(t.created, file)
	written, copyErr := io.Copy(destination, io.LimitReader(source, maxUploadBytes+1))
	if closeErr := destination.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return attachment{}, copyErr
	}
	if written != int64(a.Size) || written > maxUploadBytes {
		return attachment{}, fmt.Errorf("attachment changed during branch: %s", id)
	}
	a.URL = fmt.Sprintf("/api/sessions/%s/uploads/%s", t.child, id)
	raw, _ := json.Marshal(a)
	t.created = append(t.created, metadata)
	if err := os.WriteFile(metadata, raw, 0600); err != nil {
		return attachment{}, err
	}
	t.copied[id] = a
	return a, nil
}

func (t *forkUploadTransfer) cleanup() {
	if t.keep {
		return
	}
	for _, path := range t.created {
		_ = os.Remove(path)
	}
	if t.dir != "" {
		_ = os.Remove(t.dir)
	}
}

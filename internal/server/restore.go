package server

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
)

// restoreFile lists the terminal sessions whose Claude Code ran when the host
// stopped (an update, a restart): the next host starts them again, resumed.
const restoreFile = "restore.json"

// saveRunningTerminalsLocked records the terminal sessions running now.
func (s *Server) saveRunningTerminalsLocked() {
	if s.cfg.DataDir == "" {
		return
	}
	ids := []string{}
	for id, ts := range s.terminals {
		if !ts.closing {
			ids = append(ids, id)
		}
	}
	path := filepath.Join(s.cfg.DataDir, restoreFile)
	if len(ids) == 0 {
		_ = os.Remove(path)
		return
	}
	data, _ := json.Marshal(ids)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Printf("restore list: %v", err)
	}
}

// RestoreTerminals starts the terminal sessions that ran when the host last
// stopped. Call it once the host listens: Claude Code's hooks report to it.
func (s *Server) RestoreTerminals() {
	if s.cfg.DataDir == "" {
		return
	}
	path := filepath.Join(s.cfg.DataDir, restoreFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	_ = os.Remove(path) // once: a session that fails to start is not retried forever
	var ids []string
	if err != nil || json.Unmarshal(data, &ids) != nil {
		log.Printf("restore list unreadable: %v", err)
		return
	}
	for _, id := range ids {
		x, err := s.store.Get(id)
		if err != nil || x.Mode != "terminal" {
			continue
		}
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			return
		}
		_, err = s.terminalLocked(x)
		s.mu.Unlock()
		if err != nil {
			log.Printf("restore terminal session %s: %v", id, err)
			continue
		}
		log.Printf("Restored terminal session %s (%s)", id, x.Title)
	}
}

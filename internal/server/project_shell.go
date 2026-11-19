package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"cc-remote/internal/terminal"
)

const (
	projectShellFile    = "project-shells.json"
	maxProjectShellRuns = 16
)

type projectShellRecord struct {
	ID         string `json:"id"`
	ProjectKey string `json:"projectKey"`
	Cwd        string `json:"cwd"`
}

type projectShellView struct {
	projectShellRecord
	State     string   `json:"state"`
	RunID     string   `json:"runId,omitempty"`
	Aliases   []string `json:"aliases,omitempty"`
	Recovered bool     `json:"recovered,omitempty"`
}

type projectShellEntry struct {
	projectShellRecord
	run      *projectShellRun
	restored bool
	ended    string
}

func (e *projectShellEntry) view() projectShellView {
	v := projectShellView{projectShellRecord: e.projectShellRecord, State: "stopped"}
	if e.run != nil {
		v.State = e.run.state()
		if v.State == "running" {
			v.RunID = e.run.id
		}
	} else if e.restored {
		v.State = "interrupted"
	} else if e.ended != "" {
		v.State = e.ended
	}
	return v
}

type projectShellRegistry struct {
	mu      sync.Mutex
	entries map[string]*projectShellEntry // stable Shell IDs, independent of projects and PTY runs
	closing bool
	tickets downloadTickets
}

func (m *projectShellRegistry) loadLocked(dataDir string) error {
	if m.entries != nil {
		return nil
	}
	m.entries = make(map[string]*projectShellEntry)
	data, err := os.ReadFile(filepath.Join(dataDir, projectShellFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var records []projectShellRecord
	if err == nil {
		err = json.Unmarshal(data, &records)
	}
	if err != nil {
		m.entries = nil
		return err
	}
	for _, rec := range records {
		m.entries[rec.ID] = &projectShellEntry{projectShellRecord: rec, restored: true}
	}
	return nil
}

func (m *projectShellRegistry) saveLocked(dataDir string) error {
	records := make([]projectShellRecord, 0, len(m.entries))
	for _, e := range m.entries {
		records = append(records, e.projectShellRecord)
	}
	slices.SortFunc(records, func(a, b projectShellRecord) int { return strings.Compare(a.ID, b.ID) })
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dataDir, "project-shells.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dataDir, projectShellFile))
}

func (m *projectShellRegistry) shutdown() func(context.Context) {
	m.mu.Lock()
	m.closing = true
	var done []<-chan struct{}
	for _, e := range m.entries {
		if e.run != nil {
			e.run.pty.Close()
			done = append(done, e.run.pty.Done())
		}
	}
	m.mu.Unlock()
	return func(ctx context.Context) {
		for _, ch := range done {
			select {
			case <-ch:
			case <-ctx.Done():
				return
			}
		}
	}
}

func projectShellDir(cwd string) (dir, key string, err error) {
	if !filepath.IsAbs(cwd) {
		return "", "", errors.New("project directory must be absolute")
	}
	dir, err = filepath.EvalSymlinks(filepath.Clean(cwd))
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", "", err
	}
	if !info.IsDir() {
		return "", "", errors.New("project path is not a directory")
	}
	key = filepath.ToSlash(dir)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return dir, key, nil
}

func (s *Server) projectShellAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.projectShellEnabled() {
		return true
	}
	fail(w, http.StatusForbidden, s.lang(r).T("Project Shell is disabled on this host", "此主机未启用项目终端"))
	return false
}

func (s *Server) projectShellEnabled() bool {
	return !s.cfg.DisableProjectShell && s.cfg.Token != "" && s.cfg.DataDir != ""
}

func (s *Server) projectShellStorageError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("project shell storage: %v", err)
	fail(w, http.StatusInternalServerError, s.lang(r).T("Could not access project shell records", "无法访问项目终端记录"))
}

func (s *Server) listProjectShells(w http.ResponseWriter, r *http.Request) {
	if !s.projectShellAvailable(w, r) {
		return
	}
	var projectKey string
	cwd := r.URL.Query().Get("cwd")
	if cwd != "" {
		var err error
		_, projectKey, err = projectShellDir(cwd)
		if err != nil {
			fail(w, http.StatusBadRequest, s.lang(r).T("Project directory is unavailable", "项目目录不可用"))
			return
		}
	}
	m := &s.projectShells
	m.mu.Lock()
	err := m.loadLocked(s.cfg.DataDir)
	views := make([]projectShellView, 0, len(m.entries))
	for _, e := range m.entries {
		if projectKey == "" || e.ProjectKey == projectKey {
			views = append(views, e.view())
		}
	}
	m.mu.Unlock()
	if err != nil {
		s.projectShellStorageError(w, r, err)
		return
	}
	if len(views) != 0 {
		aliases := s.projectShellAliases()
		for i := range views {
			views[i].Aliases = aliases[views[i].ProjectKey]
			if cwd != "" && cwd != views[i].Cwd && !slices.Contains(views[i].Aliases, cwd) {
				views[i].Aliases = append(views[i].Aliases, cwd)
			}
		}
	}
	slices.SortFunc(views, func(a, b projectShellView) int { return strings.Compare(a.Cwd, b.Cwd) })
	respond(w, http.StatusOK, views)
}

func (s *Server) projectShellAliases() map[string][]string {
	byKey := make(map[string][]string)
	sessions, err := s.store.List()
	if err != nil {
		return byKey
	}
	seen := make(map[string]bool)
	for _, session := range sessions {
		if seen[session.Cwd] {
			continue
		}
		seen[session.Cwd] = true
		_, key, err := projectShellDir(session.Cwd)
		if err == nil {
			byKey[key] = append(byKey[key], session.Cwd)
		}
	}
	return byKey
}

func (s *Server) startProjectShell(w http.ResponseWriter, r *http.Request) {
	if !s.projectShellAvailable(w, r) {
		return
	}
	var req struct {
		Cwd     string `json:"cwd"`
		ShellID string `json:"shellId"`
		Restart bool   `json:"restart"`
	}
	if !decode(w, r, &req) {
		return
	}
	l := s.lang(r)
	if (req.Cwd == "") == (req.ShellID == "") {
		fail(w, http.StatusBadRequest, l.T("Specify a project directory or shell ID", "请选择项目目录或终端 ID"))
		return
	}
	var dir, key string
	var aliases []string
	if req.Cwd != "" {
		var err error
		dir, key, err = projectShellDir(req.Cwd)
		if err != nil {
			fail(w, http.StatusBadRequest, l.T("Project directory is unavailable", "项目目录不可用"))
			return
		}
		aliases = s.projectShellAliases()[key]
	}
	m := &s.projectShells
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		fail(w, http.StatusServiceUnavailable, l.T("Host is shutting down", "主机正在关闭"))
		return
	}
	if err := m.loadLocked(s.cfg.DataDir); err != nil {
		s.projectShellStorageError(w, r, err)
		return
	}
	entry := m.entries[req.ShellID]
	if req.ShellID != "" && entry == nil {
		fail(w, http.StatusNotFound, l.T("Project shell not found", "找不到项目终端"))
		return
	}
	if req.Cwd != "" {
		for _, e := range m.entries {
			if e.ProjectKey == key {
				entry = e
				break
			}
		}
	}
	if entry != nil {
		view := entry.view()
		view.Aliases = aliases
		if view.State == "running" || (view.State != "interrupted" && !req.Restart) {
			respond(w, http.StatusOK, view)
			return
		}
		if entry.run != nil && !entry.run.finished() {
			fail(w, http.StatusConflict, l.T("The previous terminal is still stopping", "上一个终端正在结束"))
			return
		}
		var err error
		dir, _, err = projectShellDir(entry.Cwd)
		if err != nil {
			fail(w, http.StatusBadRequest, l.T("Saved startup directory is unavailable", "保存的启动目录不可用"))
			return
		}
	}
	active := 0
	for _, e := range m.entries {
		if e.run != nil && !e.run.finished() {
			active++
		}
	}
	if active >= maxProjectShellRuns {
		fail(w, http.StatusConflict, l.T("This host has reached its project shell limit", "此主机的项目终端数量已达上限"))
		return
	}
	pty, err := terminal.StartProjectShell(dir)
	if err != nil {
		log.Printf("start project shell: %v", err)
		fail(w, http.StatusInternalServerError, l.T("Could not start the project shell", "无法启动项目终端"))
		return
	}
	if entry == nil {
		entry = &projectShellEntry{projectShellRecord: projectShellRecord{ID: rand.Text(), ProjectKey: key, Cwd: dir}}
		m.entries[entry.ID] = entry
		if err := m.saveLocked(s.cfg.DataDir); err != nil {
			delete(m.entries, entry.ID)
			pty.Close()
			s.projectShellStorageError(w, r, err)
			return
		}
	}
	recovered := entry.restored
	entry.restored = false
	run := &projectShellRun{id: rand.Text(), pty: pty}
	entry.run = run
	go m.releaseFinishedRun(entry, run)
	view := entry.view()
	view.Aliases = aliases
	view.Recovered = recovered
	respond(w, http.StatusOK, view)
}

func (m *projectShellRegistry) releaseFinishedRun(entry *projectShellEntry, run *projectShellRun) {
	<-run.pty.Done()
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.run == run {
		entry.ended = run.state()
		entry.run = nil
	}
}

func (s *Server) findProjectShellRun(w http.ResponseWriter, r *http.Request) *projectShellRun {
	m := &s.projectShells
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		fail(w, http.StatusServiceUnavailable, s.lang(r).T("Host is shutting down", "主机正在关闭"))
		return nil
	}
	e := m.entries[r.PathValue("id")]
	if e == nil || e.run == nil || e.run.id != r.PathValue("run") || e.run.state() != "running" {
		fail(w, http.StatusGone, s.lang(r).T("This terminal run has ended", "这次终端运行已结束"))
		return nil
	}
	return e.run
}

func (s *Server) stopProjectShell(w http.ResponseWriter, r *http.Request) {
	if !s.projectShellAvailable(w, r) {
		return
	}
	if run := s.findProjectShellRun(w, r); run != nil {
		run.stopped.Store(true)
		run.pty.Close()
		select {
		case <-run.pty.Done():
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	}
}

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
	"unicode"
	"unicode/utf8"

	"cc-remote/internal/terminal"
)

const (
	projectShellFile    = "project-shells.json"
	maxProjectShellRuns = 16
)

type projectShellRecord struct {
	ID             string `json:"id"`
	ProjectKey     string `json:"projectKey"`
	Cwd            string `json:"cwd"`
	Name           string `json:"name,omitempty"`
	Default        *bool  `json:"default"`
	TabID          string `json:"tabId,omitempty"`
	SplitDirection string `json:"splitDirection,omitempty"`
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

func (e *projectShellEntry) state() string {
	if e.run != nil {
		return e.run.state()
	}
	if e.restored {
		return "interrupted"
	}
	if e.ended != "" {
		return e.ended
	}
	return "stopped"
}

func (e *projectShellEntry) view() projectShellView {
	v := projectShellView{projectShellRecord: e.projectShellRecord, State: e.state()}
	if v.State == "running" {
		v.RunID = e.run.id
	}
	return v
}

func isOpenProjectShell(state string) bool {
	return state == "running" || state == "interrupted"
}

func (e *projectShellEntry) tabID() string {
	if e.TabID != "" {
		return e.TabID
	}
	return e.ID
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
		// Records written before multiple shells were introduced belong to the default shell.
		if rec.Default == nil {
			isDefault := true
			rec.Default = &isDefault
		}
		m.entries[rec.ID] = &projectShellEntry{projectShellRecord: rec, restored: true}
	}
	return nil
}

func (m *projectShellRegistry) saveLocked(dataDir, closingID string) error {
	records := make([]projectShellRecord, 0, len(m.entries))
	for _, e := range m.entries {
		if e.ID != closingID && isOpenProjectShell(e.state()) {
			records = append(records, e.projectShellRecord)
		}
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
			if e.run.state() == "running" {
				e.run.interrupted.Store(true)
			}
			e.run.pty.Close()
			done = append(done, e.run.released)
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
		// Older CLI clients use this filter to resolve attach/stop without an ID.
		view := e.view()
		if (projectKey == "" && isOpenProjectShell(view.State)) || (projectKey != "" && e.ProjectKey == projectKey && *e.Default) {
			views = append(views, view)
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
	slices.SortFunc(views, func(a, b projectShellView) int {
		if order := strings.Compare(a.Cwd, b.Cwd); order != 0 {
			return order
		}
		if *a.Default != *b.Default {
			if *a.Default {
				return -1
			}
			return 1
		}
		if order := strings.Compare(a.Name, b.Name); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
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
		Cwd            string `json:"cwd"`
		ShellID        string `json:"shellId"`
		Restart        bool   `json:"restart"`
		Create         bool   `json:"create"`
		Name           string `json:"name"`
		TabID          string `json:"tabId"`
		SplitDirection string `json:"splitDirection"`
	}
	if !decode(w, r, &req) {
		return
	}
	l := s.lang(r)
	if (req.Cwd == "") == (req.ShellID == "") {
		fail(w, http.StatusBadRequest, l.T("Specify a project directory or shell ID", "请选择项目目录或终端 ID"))
		return
	}
	if req.Create && req.Cwd == "" {
		fail(w, http.StatusBadRequest, l.T("Specify a project directory for the new shell", "请选择新终端的项目目录"))
		return
	}
	if (req.TabID != "" || req.SplitDirection != "") &&
		(!req.Create || req.TabID == "" || (req.SplitDirection != "columns" && req.SplitDirection != "rows")) {
		fail(w, http.StatusBadRequest, l.T("Specify a tab and a split direction for the new shell", "请为新终端指定标签页和分屏方向"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validProjectShellName(name) {
		fail(w, http.StatusBadRequest, l.T("Shell names must be at most 80 characters without control characters", "终端名称不得超过 80 个字符或包含控制字符"))
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
	var splitMember *projectShellEntry
	if req.TabID != "" {
		for _, member := range m.entries {
			if member.ProjectKey == key && member.tabID() == req.TabID && isOpenProjectShell(member.state()) {
				if splitMember != nil {
					fail(w, http.StatusConflict, l.T("This tab already has two panes", "此标签页已有两个分屏"))
					return
				}
				splitMember = member
			}
		}
		if splitMember == nil {
			fail(w, http.StatusNotFound, l.T("Project shell tab not found", "找不到项目终端标签页"))
			return
		}
	}
	entry := m.entries[req.ShellID]
	if req.ShellID != "" && (entry == nil || (!*entry.Default && !isOpenProjectShell(entry.state()))) {
		fail(w, http.StatusNotFound, l.T("Project shell not found", "找不到项目终端"))
		return
	}
	if req.Cwd != "" && !req.Create {
		for _, e := range m.entries {
			if e.ProjectKey == key && *e.Default {
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
	var replacedDefault *projectShellEntry
	if entry != nil && !isOpenProjectShell(entry.state()) {
		// An orphaned split keeps its tab ID. Reopening the default must create a separate tab.
		for _, member := range m.entries {
			if member.TabID == entry.ID && isOpenProjectShell(member.state()) {
				replacedDefault = entry
				entry = nil
				break
			}
		}
	}
	newEntry := entry == nil
	if newEntry {
		isDefault := !req.Create
		entry = &projectShellEntry{projectShellRecord: projectShellRecord{
			ID: rand.Text(), ProjectKey: key, Cwd: dir, Name: name, Default: &isDefault,
			TabID: req.TabID, SplitDirection: req.SplitDirection,
		}}
		if replacedDefault != nil {
			entry.ProjectKey = replacedDefault.ProjectKey
			delete(m.entries, replacedDefault.ID)
		}
		m.entries[entry.ID] = entry
	}
	previous := *entry
	if !isOpenProjectShell(entry.state()) {
		entry.Name = name
	}
	entry.restored = false
	run := &projectShellRun{id: rand.Text(), pty: pty, released: make(chan struct{})}
	entry.run = run
	var previousDirection string
	if splitMember != nil {
		// The new pane owns the direction, including when it replaces a tab's original root.
		previousDirection = splitMember.SplitDirection
		splitMember.SplitDirection = ""
	}
	if err := m.saveLocked(s.cfg.DataDir, ""); err != nil {
		*entry = previous
		if splitMember != nil {
			splitMember.SplitDirection = previousDirection
		}
		if newEntry {
			delete(m.entries, entry.ID)
		}
		if replacedDefault != nil {
			m.entries[replacedDefault.ID] = replacedDefault
		}
		pty.Close()
		s.projectShellStorageError(w, r, err)
		return
	}
	go m.releaseFinishedRun(s.cfg.DataDir, entry, run)
	view := entry.view()
	view.Aliases = aliases
	view.Recovered = previous.restored
	respond(w, http.StatusOK, view)
}

func validProjectShellName(name string) bool {
	return utf8.RuneCountInString(name) <= 80 && strings.IndexFunc(name, unicode.IsControl) < 0
}

func (s *Server) renameProjectShell(w http.ResponseWriter, r *http.Request) {
	if !s.projectShellAvailable(w, r) {
		return
	}
	var req struct {
		Name *string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	l := s.lang(r)
	if req.Name == nil {
		fail(w, http.StatusBadRequest, l.T("Specify a shell name", "请指定终端名称"))
		return
	}
	name := strings.TrimSpace(*req.Name)
	if !validProjectShellName(name) {
		fail(w, http.StatusBadRequest, l.T("Shell names must be at most 80 characters without control characters", "终端名称不得超过 80 个字符或包含控制字符"))
		return
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
	entry := m.entries[r.PathValue("id")]
	if entry == nil {
		fail(w, http.StatusNotFound, l.T("Project shell not found", "找不到项目终端"))
		return
	}
	if name != entry.Name {
		previous := entry.Name
		entry.Name = name
		if err := m.saveLocked(s.cfg.DataDir, ""); err != nil {
			entry.Name = previous
			s.projectShellStorageError(w, r, err)
			return
		}
	}
	respond(w, http.StatusOK, entry.view())
}

func (m *projectShellRegistry) releaseFinishedRun(dataDir string, entry *projectShellEntry, run *projectShellRun) {
	<-run.pty.Done()
	m.mu.Lock()
	defer m.mu.Unlock()
	defer close(run.released)
	if entry.run != run || m.entries[entry.ID] != entry {
		return
	}
	entry.run = nil
	entry.ended = run.state()
	if entry.ended == "interrupted" {
		entry.restored = true
		return
	}
	if !*entry.Default {
		delete(m.entries, entry.ID)
	}
	if entry.ended == "exited" {
		if err := m.saveLocked(dataDir, ""); err != nil {
			log.Printf("could not remove exited project shell from disk: %v", err)
		}
	}
}

func (s *Server) dismissProjectShell(w http.ResponseWriter, r *http.Request) {
	if !s.projectShellAvailable(w, r) {
		return
	}
	m := &s.projectShells
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		fail(w, http.StatusServiceUnavailable, s.lang(r).T("Host is shutting down", "主机正在关闭"))
		return
	}
	if err := m.loadLocked(s.cfg.DataDir); err != nil {
		s.projectShellStorageError(w, r, err)
		return
	}
	id := r.PathValue("id")
	if entry := m.entries[id]; entry != nil && isOpenProjectShell(entry.state()) {
		if entry.run != nil {
			fail(w, http.StatusConflict, s.lang(r).T("End the active terminal run first", "请先结束正在运行的终端"))
			return
		}
		delete(m.entries, id)
		if err := m.saveLocked(s.cfg.DataDir, ""); err != nil {
			m.entries[id] = entry
			s.projectShellStorageError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
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
	m := &s.projectShells
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		fail(w, http.StatusServiceUnavailable, s.lang(r).T("Host is shutting down", "主机正在关闭"))
		return
	}
	entry := m.entries[r.PathValue("id")]
	if entry == nil || entry.run == nil || entry.run.id != r.PathValue("run") || entry.run.state() != "running" {
		m.mu.Unlock()
		fail(w, http.StatusGone, s.lang(r).T("This terminal run has ended", "这次终端运行已结束"))
		return
	}
	run := entry.run
	if err := m.saveLocked(s.cfg.DataDir, entry.ID); err != nil {
		m.mu.Unlock()
		s.projectShellStorageError(w, r, err)
		return
	}
	run.stopped.Store(true)
	run.pty.Close()
	m.mu.Unlock()
	select {
	case <-run.released:
		w.WriteHeader(http.StatusNoContent)
	case <-r.Context().Done():
	}
}

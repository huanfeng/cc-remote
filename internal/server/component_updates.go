package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
	"cc-remote/internal/update"
)

const updateHistoryLimit = 30

type componentUpdate struct {
	ID             string `json:"id"`
	Phase          string `json:"phase"`
	CurrentVersion string `json:"currentVersion,omitempty"`
	Version        string `json:"version,omitempty"`
	Notes          string `json:"notes,omitempty"`
	NotesURL       string `json:"notesUrl,omitempty"`
	Message        string `json:"message,omitempty"`
}

type updateHistoryEntry struct {
	ID        string    `json:"id"`
	Component string    `json:"component"`
	From      string    `json:"from,omitempty"`
	Version   string    `json:"version,omitempty"`
	Status    string    `json:"status"`
	At        time.Time `json:"at"`
	Message   string    `json:"message,omitempty"`
	NotesURL  string    `json:"notesUrl,omitempty"`
}

type updateSnapshot struct {
	Components []componentUpdate    `json:"components"`
	History    []updateHistoryEntry `json:"history"`
}

type updateJournal struct {
	Pending *updateHistoryEntry  `json:"pending,omitempty"`
	History []updateHistoryEntry `json:"history"`
}

type componentAdapter struct {
	check   func(context.Context, i18n.Lang) (componentUpdate, error)
	install func(context.Context, componentUpdate) (string, error)
}

type componentUpdates struct {
	mu        sync.Mutex
	order     []string
	adapters  map[string]componentAdapter
	state     map[string]componentUpdate
	journal   updateJournal
	path      string
	readError error
}

func (s *Server) initComponentUpdates() {
	u := &s.updates
	u.adapters = map[string]componentAdapter{
		"core":   {check: s.checkCoreUpdate, install: s.installCoreUpdate},
		"claude": s.cliUpdateAdapter("claude", s.cfg.ClaudeExecutable, s.cfg.ClaudeConfigEnv),
		"codex":  s.cliUpdateAdapter("codex", s.cfg.CodexExecutable, s.cfg.CodexConfigDir),
	}
	u.order = []string{"core", "claude", "codex"}
	u.state = make(map[string]componentUpdate, len(u.order))
	for _, id := range u.order {
		u.state[id] = componentUpdate{ID: id, Phase: "idle"}
	}
	core := u.state["core"]
	core.CurrentVersion = s.cfg.Version
	u.state["core"] = core
	if s.cfg.DataDir == "" {
		return
	}
	u.path = filepath.Join(s.cfg.DataDir, "component-updates.json")
	data, err := os.ReadFile(u.path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err == nil {
		err = json.Unmarshal(data, &u.journal)
	}
	if err != nil {
		u.journal = updateJournal{}
		u.readError = err
		log.Printf("Read component update history: %v", err)
		return
	}
	if pending := u.journal.Pending; pending != nil {
		state := u.state[pending.Component]
		if pending.Component == "core" && sameUpdateVersion(pending.Version, s.cfg.Version) {
			state.Phase, state.Version, state.NotesURL = "updated", pending.Version, pending.NotesURL
			pending.Status = "updated"
		} else {
			state.Phase = "error"
			state.Message = s.language().T("The host restarted before the update could be confirmed. Check the installed version again.", "宿主重启前未能确认更新结果，请重新检查已安装版本。")
			pending.Status, pending.Message = state.Phase, state.Message
		}
		if state.ID != "" {
			u.state[state.ID] = state
		}
		if err := u.finish(*pending); err != nil {
			u.readError = err
			log.Printf("Recover component update history: %v", err)
		}
	}
}

func sameUpdateVersion(a, b string) bool {
	return a != "" && b != "" && strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v")
}

func (u *componentUpdates) save(journal updateJournal) error {
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(u.path), ".component-updates-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), u.path)
	}
	return err
}

// finish persists the result and releases the installation slot in one write.
func (u *componentUpdates) finish(entry updateHistoryEntry) error {
	entry.At = time.Now().UTC()
	history := append([]updateHistoryEntry{entry}, u.journal.History...)
	if len(history) > updateHistoryLimit {
		history = history[:updateHistoryLimit]
	}
	journal := updateJournal{History: history}
	if err := u.save(journal); err != nil {
		return err
	}
	u.journal = journal
	return nil
}

func (u *componentUpdates) snapshot() updateSnapshot {
	result := updateSnapshot{
		Components: make([]componentUpdate, 0, len(u.order)),
		History:    append([]updateHistoryEntry{}, u.journal.History...),
	}
	for _, id := range u.order {
		result.Components = append(result.Components, u.state[id])
	}
	return result
}

func (s *Server) componentUpdatesAPI(w http.ResponseWriter, r *http.Request) {
	u := &s.updates
	u.mu.Lock()
	result := u.snapshot()
	u.mu.Unlock()
	respond(w, http.StatusOK, result)
}

func (s *Server) checkComponentUpdateAPI(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	l := s.lang(r)
	u := &s.updates
	adapter, ok := u.adapters[id]
	if !ok {
		fail(w, http.StatusNotFound, l.T("Unknown update component", "未知更新组件"))
		return
	}
	u.mu.Lock()
	if u.state[id].Phase == "checking" || (u.journal.Pending != nil && u.journal.Pending.Component == id) {
		result := u.snapshot()
		u.mu.Unlock()
		respond(w, http.StatusAccepted, result)
		return
	}
	u.state[id] = componentUpdate{ID: id, Phase: "checking", CurrentVersion: u.state[id].CurrentVersion}
	result := u.snapshot()
	u.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		state, err := adapter.check(ctx, l)
		u.mu.Lock()
		defer u.mu.Unlock()
		state.ID = id
		if state.CurrentVersion == "" {
			state.CurrentVersion = u.state[id].CurrentVersion
		}
		if err != nil {
			state.Phase, state.Message = "error", i18n.Text(l, err)
		}
		u.state[id] = state
	}()
	respond(w, http.StatusAccepted, result)
}

func (s *Server) installComponentUpdateAPI(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	l := s.lang(r)
	u := &s.updates
	adapter, ok := u.adapters[id]
	if !ok {
		fail(w, http.StatusNotFound, l.T("Unknown update component", "未知更新组件"))
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	restarting := s.closing || s.restartPending
	s.mu.Unlock()
	u.mu.Lock()
	state := u.state[id]
	switch {
	case u.journal.Pending != nil || restarting:
		u.mu.Unlock()
		fail(w, http.StatusConflict, l.T("The host is already updating or restarting", "宿主已有更新或重启任务"))
		return
	case u.path == "" || u.readError != nil:
		u.mu.Unlock()
		fail(w, http.StatusServiceUnavailable, l.T("The host cannot save its update history", "宿主无法保存更新记录"))
		return
	case (state.Phase != "available" && state.Phase != "ready") || req.Version != state.Version:
		u.mu.Unlock()
		fail(w, http.StatusConflict, l.T("Check for an update before installing it", "请先检查组件更新"))
		return
	}
	entry := updateHistoryEntry{ID: store.ID(), Component: id, From: state.CurrentVersion, Version: state.Version, NotesURL: state.NotesURL}
	journal := u.journal
	journal.Pending = &entry
	if err := u.save(journal); err != nil {
		u.mu.Unlock()
		log.Printf("Save pending component update: %v", err)
		fail(w, http.StatusInternalServerError, l.T("Could not save the pending update", "无法保存待执行的更新"))
		return
	}
	u.journal = journal
	u.setPhase(state, "installing", "")
	result := u.snapshot()
	u.mu.Unlock()
	go s.runComponentUpdate(adapter, state, entry, l)
	respond(w, http.StatusAccepted, result)
}

func (u *componentUpdates) setPhase(state componentUpdate, phase, message string) {
	state.Phase, state.Message = phase, message
	u.state[state.ID] = state
}

func (s *Server) runComponentUpdate(adapter componentAdapter, state componentUpdate, entry updateHistoryEntry, l i18n.Lang) {
	u := &s.updates
	if state.ID != "core" {
		if !s.waitForCLIUpdate(state, l) {
			// Keep the journal for the next host to report the interruption.
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	version, err := adapter.install(ctx, state)
	if state.ID != "core" {
		s.mu.Lock()
		s.updatingProvider = ""
		s.mu.Unlock()
	}
	restarting := state.ID == "core" && s.pendingRestart()
	u.mu.Lock()
	defer u.mu.Unlock()
	if restarting {
		u.setPhase(state, "waiting", l.T("The host will restart when idle", "宿主将在会话空闲后重启"))
		return
	}
	switch {
	case err != nil:
		entry.Status, entry.Message = "error", i18n.Text(l, err)
	case state.ID == "core":
		entry.Status = "error"
		entry.Message = l.T("The new Core was installed, but this host did not confirm a restart. Check the service on the host.", "新 Core 已安装，当前宿主未确认重启。请检查宿主服务。")
	default:
		entry.Status, entry.Version = "updated", version
		if sameUpdateVersion(version, entry.From) {
			entry.Status = "current"
		}
		state.CurrentVersion = version
		state.Version = version
	}
	if err := u.finish(entry); err != nil {
		u.readError = err
		log.Printf("Save component update result: %v", err)
		u.setPhase(state, "error", l.T("Could not save the update result", "无法保存更新结果"))
		return
	}
	u.setPhase(state, entry.Status, entry.Message)
}

func (s *Server) checkCoreUpdate(ctx context.Context, l i18n.Lang) (componentUpdate, error) {
	state := componentUpdate{CurrentVersion: s.cfg.Version}
	candidate, err := update.Check(ctx, s.cfg.Version)
	if err != nil {
		log.Printf("Check Core update: %v", err)
		return state, errors.New(l.T("Could not check the Core release. See the host log for details.", "无法检查 Core 版本，详情请查看宿主日志。"))
	}
	state.Version, state.Notes, state.NotesURL = candidate.Version, candidate.Notes, candidate.NotesURL
	switch {
	case s.cfg.Restart == nil || s.cfg.DataDir == "":
		state.Phase = "unavailable"
		state.Message = l.T("Install Core as a CC Remote service to update it here.", "将 Core 安装为 CC Remote 服务后，即可在此更新。")
	case !candidate.Comparable:
		state.Phase = "unavailable"
		state.Message = l.T("This Core build has no comparable release version.", "此 Core 构建没有可比较的正式版本。")
	case candidate.Available:
		state.Phase = "available"
	default:
		state.Phase = "current"
	}
	return state, nil
}

func (s *Server) installCoreUpdate(ctx context.Context, state componentUpdate) (string, error) {
	if err := update.InstallManaged(ctx, s.cfg.Version, state.Version, s.cfg.DataDir, io.Discard); err != nil {
		log.Printf("Install Core update: %v", err)
		return "", i18n.NewError("Could not install Core. See the host log for details.", "无法安装 Core，详情请查看宿主日志。")
	}
	return state.Version, nil
}

func (s *Server) cliUpdateAdapter(id, executable, configDir string) componentAdapter {
	return componentAdapter{
		check: func(ctx context.Context, _ i18n.Lang) (componentUpdate, error) {
			c, err := update.CheckCLI(ctx, id, executable, configDir)
			state := componentUpdate{CurrentVersion: c.CurrentVersion, Version: c.Version, Notes: c.Notes, NotesURL: c.NotesURL, Message: c.Message}
			switch {
			case c.Unavailable:
				state.Phase = "unavailable"
			case c.Ready:
				state.Phase = "ready"
			case c.Available:
				state.Phase = "available"
			default:
				state.Phase = "current"
			}
			return state, err
		},
		install: func(ctx context.Context, state componentUpdate) (string, error) {
			return update.InstallCLI(ctx, id, executable, configDir, update.CLICandidate{
				CurrentVersion: state.CurrentVersion, Version: state.Version,
				Ready: state.Phase == "ready", Available: state.Phase == "available",
			})
		},
	}
}

// Lease the provider only after its terminals and managed turns are idle, so
// package files can be replaced and subsequent turns start a fresh process.
func (s *Server) waitForCLIUpdate(state componentUpdate, l i18n.Lang) bool {
	u := &s.updates
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		idle, closing, runtimes := s.leaseUpdateProvider(state.ID)
		if closing {
			return false
		}
		if idle {
			for _, rt := range runtimes {
				closeRuntime(rt)
			}
			u.mu.Lock()
			u.setPhase(state, "installing", "")
			u.mu.Unlock()
			return true
		}
		u.mu.Lock()
		u.setPhase(state, "waiting", l.T("Waiting for this component's active turns and terminal sessions to end", "等待该组件当前的对话轮次及终端会话结束"))
		u.mu.Unlock()
		select {
		case <-s.stopReaper:
			return false
		case <-ticker.C:
		}
	}
}

func (s *Server) leaseUpdateProvider(provider string) (idle, closing bool, runtimes []*sessionRuntime) {
	// Forking uses the provider CLI outside s.mu. Acquire its lock before
	// checking sessions, in the same order as the fork handler.
	if !s.forkMu.TryLock() {
		return false, false, nil
	}
	defer s.forkMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false, true, nil
	}
	for _, ts := range s.terminals {
		if ts.codex == (provider == "codex") && !isDone(ts.runner.Done()) {
			return false, false, nil
		}
	}
	for _, rt := range s.procs {
		if rt.provider == provider && (rt.proc == nil || rt.run != nil || rt.proc.BackgroundTasks() > 0) {
			return false, false, nil
		}
	}
	s.updatingProvider = provider
	for id, rt := range s.procs {
		if rt.provider == provider {
			runtimes = append(runtimes, s.detachLocked(id))
		}
	}
	return true, false, runtimes
}

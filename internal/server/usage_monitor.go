package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
)

type monitorAccount struct {
	ID            string              `json:"id"`
	Provider      string              `json:"provider"`
	Name          string              `json:"name"`
	Profile       string              `json:"profile,omitempty"`
	Active        bool                `json:"active"`
	IdentityKnown bool                `json:"identityKnown"`
	Available     bool                `json:"available"`
	Reason        string              `json:"reason,omitempty"`
	Plan          string              `json:"plan,omitempty"`
	Windows       []codex.QuotaWindow `json:"windows"`
	FetchedAt     *time.Time          `json:"fetchedAt,omitempty"`
	Stale         bool                `json:"stale,omitempty"`
	Error         string              `json:"error,omitempty"`
}
type monitorBinding struct {
	ID      string    `json:"id"`
	Known   bool      `json:"known"`
	RetryAt time.Time `json:"retryAt"`
}
type monitorState struct {
	IntervalMinutes int                       `json:"intervalMinutes"`
	Accounts        map[string]monitorAccount `json:"accounts"`
	Bindings        map[string]monitorBinding `json:"bindings"`
}
type monitorReply struct {
	IntervalMinutes int              `json:"intervalMinutes"`
	Accounts        []monitorAccount `json:"accounts"`
}
type monitorPoint struct {
	At              time.Time           `json:"at"`
	IntervalMinutes int                 `json:"intervalMinutes"`
	Windows         []codex.QuotaWindow `json:"windows"`
	Error           string              `json:"error,omitempty"`
}
type usageMonitor struct {
	mu          sync.Mutex
	refreshMu   sync.Mutex
	nextRefresh time.Time
	dir         string
	loaded      bool
	state       monitorState
	// Injectable only in tests; production always asks the official endpoint.
	identity func(context.Context, string) (string, error)
}

func monitorHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func monitorID(provider, identity string) string {
	return provider + ":" + monitorHash(provider + ":" + identity)[:32]
}

func (m *usageMonitor) loadLocked(dir string) error {
	if m.loaded && m.dir == dir {
		return nil
	}
	state := monitorState{IntervalMinutes: 10, Accounts: map[string]monitorAccount{}, Bindings: map[string]monitorBinding{}}
	if dir != "" {
		data, err := os.ReadFile(filepath.Join(dir, "usage-monitor.json"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			if err = json.Unmarshal(data, &state); err != nil {
				return err
			}
		}
	}
	if state.IntervalMinutes != 10 && state.IntervalMinutes != 30 {
		return fmt.Errorf("invalid monitor interval")
	}
	if state.Accounts == nil {
		state.Accounts = map[string]monitorAccount{}
	}
	if state.Bindings == nil {
		state.Bindings = map[string]monitorBinding{}
	}
	m.dir, m.state, m.loaded = dir, state, true
	return nil
}
func (m *usageMonitor) saveLocked() error {
	if m.dir == "" {
		return nil
	}
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.dir, "usage-monitor.*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(m.dir, "usage-monitor.json"))
}
func (s *Server) monitorSnapshot() (monitorReply, error) {
	m := &s.monitor
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadLocked(s.cfg.DataDir); err != nil {
		return monitorReply{}, err
	}
	reply := monitorReply{IntervalMinutes: m.state.IntervalMinutes, Accounts: []monitorAccount{}}
	for _, a := range m.state.Accounts {
		if a.Windows == nil {
			a.Windows = []codex.QuotaWindow{}
		}
		reply.Accounts = append(reply.Accounts, a)
	}
	sort.Slice(reply.Accounts, func(i, j int) bool {
		a, b := reply.Accounts[i], reply.Accounts[j]
		if a.Active != b.Active {
			return a.Active
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	return reply, nil
}
func (s *Server) monitorAPI(w http.ResponseWriter, r *http.Request) {
	reply, err := s.monitorSnapshot()
	if err != nil {
		fail(w, 500, "Could not read usage monitor settings")
		return
	}
	respond(w, 200, reply)
}
func (s *Server) monitorSettingsAPI(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IntervalMinutes int `json:"intervalMinutes"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.IntervalMinutes != 10 && body.IntervalMinutes != 30 {
		fail(w, 400, "intervalMinutes must be 10 or 30")
		return
	}
	m := &s.monitor
	m.mu.Lock()
	err := m.loadLocked(s.cfg.DataDir)
	if err == nil {
		old := m.state.IntervalMinutes
		m.state.IntervalMinutes = body.IntervalMinutes
		err = m.saveLocked()
		if err != nil {
			m.state.IntervalMinutes = old
		}
	}
	m.mu.Unlock()
	if err != nil {
		fail(w, 500, "Could not save usage monitor settings")
		return
	}
	s.monitorAPI(w, r)
}
func (s *Server) monitorRefreshAPI(w http.ResponseWriter, r *http.Request) {
	if err := s.refreshMonitor(r.Context()); err != nil {
		fail(w, 500, "Could not update usage monitor")
		return
	}
	s.monitorAPI(w, r)
}
func (s *Server) monitorHistoryAPI(w http.ResponseWriter, r *http.Request) {
	reply, err := s.monitorSnapshot()
	if err != nil {
		fail(w, 500, "Could not read usage monitor")
		return
	}
	id := r.URL.Query().Get("account")
	known := false
	for _, a := range reply.Accounts {
		if a.ID == id {
			known = true
		}
	}
	if !known {
		fail(w, 404, "Monitored account not found")
		return
	}
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 {
		hours = 24
	}
	hours = min(hours, 720)
	rows, err := s.store.AccountUsageSamples("monitor:"+id, time.Now().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		fail(w, 500, "Could not read usage history")
		return
	}
	points := []monitorPoint{}
	for _, row := range rows {
		var p monitorPoint
		if json.Unmarshal(row.Data, &p) == nil {
			p.At = row.At
			points = append(points, p)
		}
	}
	respond(w, 200, map[string]any{"hours": hours, "points": points})
}

func (s *Server) monitorClaudeIdentity(ctx context.Context, token string) (string, bool, error) {
	m := &s.monitor
	key := monitorHash(token)
	m.mu.Lock()
	binding, ok := m.state.Bindings[key]
	m.mu.Unlock()
	if ok && (binding.Known || time.Now().Before(binding.RetryAt)) {
		return binding.ID, binding.Known, nil
	}
	fetch := m.identity
	if fetch == nil {
		fetch = func(ctx context.Context, t string) (string, error) {
			return claude.FetchAccountIdentity(ctx, usageClient, t)
		}
	}
	identity, err := fetch(ctx, token)
	known := err == nil && identity != ""
	id := monitorID("claude", "credential:"+key)
	if known {
		id = monitorID("claude", "account:"+identity)
	}
	// A newly verified credential can safely adopt its own previously isolated samples.
	if ok && binding.ID != id && !binding.Known {
		if err := s.store.MoveAccountUsage(binding.ID, id); err != nil {
			return "", false, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ok && binding.ID != id && !binding.Known {
		delete(m.state.Accounts, binding.ID)
	}
	m.state.Bindings[key] = monitorBinding{ID: id, Known: known, RetryAt: time.Now().Add(30 * time.Minute)}
	return id, known, nil
}

func claudeMonitorWindows(u *claude.Usage) []codex.QuotaWindow {
	windows := []codex.QuotaWindow{}
	if u == nil {
		return windows
	}
	add := func(name string, minutes int64, w *claude.UsageWindow) {
		if w != nil {
			windows = append(windows, codex.QuotaWindow{Name: name, Percent: w.Percent, WindowMinutes: minutes, ResetsAt: w.ResetsAt})
		}
	}
	add("5h", 300, u.FiveHour)
	add("7d", 10080, u.Weekly)
	for _, model := range u.Models {
		w := model.UsageWindow
		add(model.Name, 10080, &w)
	}
	return windows
}

func (s *Server) recordMonitor(a monitorAccount, at time.Time) error {
	m := &s.monitor
	m.mu.Lock()
	old := m.state.Accounts[a.ID]
	interval := m.state.IntervalMinutes
	m.mu.Unlock()
	// Cached successes are not new observations. Failures are explicit gaps.
	if a.Stale || a.FetchedAt != nil && (old.FetchedAt == nil || !old.FetchedAt.Equal(*a.FetchedAt)) {
		p := monitorPoint{At: at, Windows: a.Windows, IntervalMinutes: interval}
		if a.Stale {
			p.Windows = nil
			p.Error = a.Error
		} else {
			p.At = *a.FetchedAt
		}
		data, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if err = s.store.AddAccountUsageSample("monitor:"+a.ID, p.At, data, usageKeep); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.state.Accounts[a.ID] = a
	m.mu.Unlock()
	return nil
}

func (s *Server) refreshMonitor(ctx context.Context) error {
	m := &s.monitor
	if !m.refreshMu.TryLock() {
		return nil
	}
	defer m.refreshMu.Unlock()
	if time.Now().Before(m.nextRefresh) {
		return nil
	}
	if _, err := s.monitorSnapshot(); err != nil {
		return err
	}
	if err := s.store.PruneMonitorUsage(time.Now().Add(-usageKeep)); err != nil {
		return err
	}
	m.nextRefresh = time.Now().Add(time.Minute)
	active := map[string]bool{}
	collected := map[string]bool{}
	type source struct {
		profile, name string
		token         claude.OAuthToken
	}
	sources := []source{}
	if token, reason, _ := s.usageSource(); reason == "" {
		sources = append(sources, source{name: "Claude · 本机登录", token: token})
	}
	entries, err := loadOAuthEntries(s.cfg.DataDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		sources = append(sources, source{profile: oauthIDPrefix + entry.ID, name: entry.Name, token: claude.OAuthToken{AccessToken: entry.Token}})
	}
	for _, src := range sources {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-s.stopReaper:
			return nil
		default:
		}
		identityCtx, cancel := context.WithTimeout(ctx, usageTimeout)
		id, known, err := s.monitorClaudeIdentity(identityCtx, src.token.AccessToken)
		cancel()
		if err != nil {
			return err
		}
		if collected[id] {
			continue
		}
		active[id] = true
		u := s.readUsageOfContext(ctx, "monitor-cache:"+id, s.accountUsage.get("monitor-cache:"+id), src.token, usageFresh)
		collected[id] = !u.Stale
		a := monitorAccount{ID: id, Provider: "claude", Name: src.name, Profile: src.profile, Active: true, IdentityKnown: known, Available: u.Available, Reason: u.Reason, Plan: u.Plan, Windows: claudeMonitorWindows(u.Usage), FetchedAt: u.FetchedAt, Stale: u.Stale, Error: u.Error}
		if err := s.recordMonitor(a, time.Now()); err != nil {
			return err
		}
	}
	if s.cfg.CodexExecutable != "" || s.codexUsageCache.fetch != nil {
		u := s.readCodexUsage(ctx, claude.Options{Executable: s.cfg.CodexExecutable, ConfigDir: s.cfg.CodexConfigDir, Cwd: s.cfg.Cwd})
		if u.Available && u.Identity != "" {
			id := monitorID("codex", u.Identity)
			active[id] = true
			windows := []codex.QuotaWindow{}
			if u.Usage != nil {
				windows = u.Usage.Windows
			}
			a := monitorAccount{ID: id, Provider: "codex", Name: "Codex · 本机登录", Active: true, IdentityKnown: true, Available: u.Available, Plan: u.Plan, Windows: windows, FetchedAt: u.FetchedAt, Stale: u.Stale, Error: u.Error}
			if err := s.recordMonitor(a, time.Now()); err != nil {
				return err
			}
		} else if u.Available && !u.Stale {
			// Older CLIs expose quota but not a stable identity. Show the current
			// snapshot without accumulating potentially unrelated account history.
			id := "codex:identity-unavailable"
			active[id] = true
			windows := []codex.QuotaWindow{}
			if u.Usage != nil {
				windows = u.Usage.Windows
			}
			m.mu.Lock()
			m.state.Accounts[id] = monitorAccount{ID: id, Provider: "codex", Name: "Codex · 本机登录", Active: true, Available: true, Reason: "identity-unavailable", Plan: u.Plan, Windows: windows, FetchedAt: u.FetchedAt}
			m.mu.Unlock()
		} else if u.Stale {
			var failed []monitorAccount
			m.mu.Lock()
			for id, a := range m.state.Accounts {
				if a.Provider == "codex" && a.Active {
					active[id] = true
					a.Stale = true
					a.Error = u.Error
					failed = append(failed, a)
				}
			}
			m.mu.Unlock()
			for _, a := range failed {
				if a.IdentityKnown {
					if err := s.recordMonitor(a, time.Now()); err != nil {
						return err
					}
				}
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, a := range m.state.Accounts {
		a.Active = active[id]
		m.state.Accounts[id] = a
	}
	return m.saveLocked()
}

// The scheduler belongs to the host, so neither a page nor the tray is needed.
func (s *Server) sampleMonitor(stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	if s.cfg.DataDir != "" {
		if err := s.refreshMonitor(ctx); err != nil && ctx.Err() == nil {
			log.Printf("usage monitor: %v", err)
		}
	}
	last := time.Now()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case at := <-ticker.C:
			reply, err := s.monitorSnapshot()
			if err != nil {
				log.Printf("usage monitor: %v", err)
				continue
			}
			if at.Sub(last) < time.Duration(reply.IntervalMinutes)*time.Minute {
				continue
			}
			// Keep the ticker's cadence instead of accumulating request/scheduling
			// latency into an extra minute on every subsequent collection.
			last = at
			if err = s.refreshMonitor(ctx); err != nil && ctx.Err() == nil {
				log.Printf("usage monitor: %v", err)
			}
		}
	}
}

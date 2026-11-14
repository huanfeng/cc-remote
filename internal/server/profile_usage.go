package server

import (
	"net/http"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
)

// profileUsageSummary deliberately excludes credentials, provider environment,
// raw errors and credential fingerprints. AccountID is the opaque monitor ID.
type profileUsageSummary struct {
	Available     bool                `json:"available"`
	Reason        string              `json:"reason,omitempty"`
	AccountID     string              `json:"accountId,omitempty"`
	IdentityKnown bool                `json:"identityKnown"`
	Plan          string              `json:"plan,omitempty"`
	Windows       []codex.QuotaWindow `json:"windows"`
	FetchedAt     *time.Time          `json:"fetchedAt,omitempty"`
	Stale         bool                `json:"stale,omitempty"`
	Error         string              `json:"error,omitempty"`
	Shared        bool                `json:"shared,omitempty"`
}
type profileUsageReply struct {
	Profiles map[string]profileUsageSummary `json:"profiles"`
}

func emptyProfileUsage(reason string) profileUsageSummary {
	return profileUsageSummary{Reason: reason, Windows: []codex.QuotaWindow{}}
}

// cachedProfileUsage never probes an account identity or contacts a provider.
// A current credential must have its own binding even when a profile kept its ID.
func (s *Server) cachedProfileUsage(token string) profileUsageSummary {
	if token == "" {
		return emptyProfileUsage("no-login")
	}
	m := &s.monitor
	m.mu.Lock()
	defer m.mu.Unlock()
	binding, ok := m.state.Bindings[monitorHash(token)]
	if !ok {
		return emptyProfileUsage("pending")
	}
	a, ok := m.state.Accounts[binding.ID]
	if !ok || !a.Active || a.Provider != "claude" {
		return emptyProfileUsage("pending")
	}
	windows := append([]codex.QuotaWindow{}, a.Windows...)
	ageLimit := time.Duration(m.state.IntervalMinutes) * time.Minute * 2
	if ageLimit < 20*time.Minute {
		ageLimit = 20 * time.Minute
	}
	stale := a.Stale || a.Error != "" || a.FetchedAt != nil && time.Since(*a.FetchedAt) > ageLimit
	reason := ""
	switch a.Reason {
	case "pending", "no-login", "api-key", "third-party", "identity-unavailable":
		reason = a.Reason
	}
	return profileUsageSummary{Available: a.Available, Reason: reason, AccountID: a.ID,
		IdentityKnown: binding.Known && a.IdentityKnown, Plan: a.Plan, Windows: windows,
		FetchedAt: a.FetchedAt, Stale: stale, Error: safeProfileUsageError(a.Error)}
}

func safeProfileUsageError(value string) string {
	switch value {
	case "", "expired", "rate-limited", "request-failed":
		return value
	}
	return "request-failed"
}

func (s *Server) profileUsageAPI(w http.ResponseWriter, r *http.Request) {
	app := r.URL.Query().Get("app")
	if app != "" && app != "claude" {
		fail(w, 400, "app must be claude")
		return
	}
	catalog, err := s.profileCatalog()
	if err != nil {
		fail(w, 500, "Could not read supplier configurations")
		return
	}
	// Loading the monitor file is read-only; unlike /api/usage, this endpoint must
	// not trigger refreshes, identity requests, header probes or new samples.
	m := &s.monitor
	m.mu.Lock()
	err = m.loadLocked(s.cfg.DataDir)
	m.mu.Unlock()
	if err != nil {
		fail(w, 500, "Could not read usage cache")
		return
	}
	reply := profileUsageReply{Profiles: map[string]profileUsageSummary{}}
	native, reason, _ := s.usageSource()
	if reason != "" {
		reply.Profiles["@native"] = emptyProfileUsage(reason)
	} else {
		reply.Profiles["@native"] = s.cachedProfileUsage(native.AccessToken)
	}
	counts := map[string]int{}
	if id := reply.Profiles["@native"].AccountID; id != "" {
		counts[id]++
	}
	for _, p := range catalog.Profiles {
		if p.App != "claude" {
			continue
		}
		v := emptyProfileUsage("third-party")
		if p.Settings.Kind == oauthKind {
			v = s.cachedProfileUsage(p.Settings.Env[claude.OAuthEnvKey])
		}
		reply.Profiles[p.ID] = v
		if v.AccountID != "" {
			counts[v.AccountID]++
		}
	}
	for id, v := range reply.Profiles {
		v.Shared = v.AccountID != "" && counts[v.AccountID] > 1
		reply.Profiles[id] = v
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, 200, reply)
}

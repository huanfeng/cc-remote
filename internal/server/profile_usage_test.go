package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/codex"
)

func TestProfileUsageCacheBindingsAndPrivacy(t *testing.T) {
	clearProviderEnv(t)
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	writeLogin(t, s.cfg.ClaudeConfigDir, time.Now().Add(time.Hour))
	entries := []oauthEntry{{ID: "a", Name: "Work", Token: "token-a"}, {ID: "b", Name: "Work renamed", Token: "token-b"}, {ID: "changed", Name: "Old name", Token: "new-token"}}
	if err := writeOAuthEntries(s.cfg.DataDir, entries); err != nil {
		t.Fatal(err)
	}
	if _, err := s.monitorSnapshot(); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-2 * time.Minute).UTC()
	reset := time.Now().Add(time.Hour).UTC()
	id := monitorID("claude", "account:shared")
	s.monitor.mu.Lock()
	for _, token := range []string{"sk-ant-oat-test", "token-a", "token-b", "old-token"} {
		s.monitor.state.Bindings[monitorHash(token)] = monitorBinding{ID: id, Known: true}
	}
	s.monitor.state.Accounts[id] = monitorAccount{ID: id, Provider: "claude", Active: true, Available: true, IdentityKnown: true, Plan: "max", FetchedAt: &at, Windows: []codex.QuotaWindow{{Name: "5h", Percent: 42, WindowMinutes: 300, ResetsAt: reset.Format(time.RFC3339)}}}
	s.monitor.mu.Unlock()
	s.monitor.identity = func(context.Context, string) (string, error) {
		t.Fatal("identity lookup from cache endpoint")
		return "", nil
	}
	if code, _ := request(t, ts, "GET", "/api/profiles/usage?app=claude", nil, ""); code != 401 {
		t.Fatal(code)
	}
	code, b := request(t, ts, "GET", "/api/profiles/usage?app=claude", nil, testToken)
	var reply profileUsageReply
	if code != 200 || json.Unmarshal(b, &reply) != nil {
		t.Fatalf("%d %s", code, b)
	}
	for _, key := range []string{"@native", "oauth:a", "oauth:b"} {
		v := reply.Profiles[key]
		if !v.Available || !v.Shared || !v.IdentityKnown || v.AccountID != id || len(v.Windows) != 1 || v.Windows[0].Percent != 42 || v.Windows[0].ResetsAt == "" || !v.FetchedAt.Equal(at) {
			t.Fatalf("%s %+v", key, v)
		}
	}
	if v := reply.Profiles["oauth:changed"]; v.Available || v.AccountID != "" || v.Reason != "pending" {
		t.Fatalf("old account reused after credential replacement: %+v", v)
	}
	for _, secret := range []string{"token-a", "token-b", "new-token", "sk-ant-oat-test", monitorHash("token-a"), "settings", "accessToken"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("secret leaked: %s", secret)
		}
	}
	if code, _ := request(t, ts, "GET", "/api/profiles/usage?app=invalid", nil, testToken); code != 400 {
		t.Fatal(code)
	}

	// The native login on disk must not leak through an effective API key.
	t.Setenv("ANTHROPIC_API_KEY", "secret-key")
	code, b = request(t, ts, "GET", "/api/profiles/usage", nil, testToken)
	if code != 200 || json.Unmarshal(b, &reply) != nil || reply.Profiles["@native"].Available || reply.Profiles["@native"].Reason != "api-key" {
		t.Fatalf("native override %d %s", code, b)
	}
}

func TestProfileUsageStaleUnknownAndInactive(t *testing.T) {
	clearProviderEnv(t)
	s, _, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	_, _ = s.monitorSnapshot()
	id := "claude:isolated"
	at := time.Now().Add(-time.Hour)
	m := &s.monitor
	m.mu.Lock()
	m.state.Bindings[monitorHash("credential")] = monitorBinding{ID: id}
	m.state.Accounts[id] = monitorAccount{ID: id, Provider: "claude", Active: true, Available: true, FetchedAt: &at, Error: "private token credential", Windows: []codex.QuotaWindow{{Name: "7d", Percent: 24}}}
	m.mu.Unlock()
	v := s.cachedProfileUsage("credential")
	if !v.Available || !v.Stale || v.IdentityKnown || v.Error != "request-failed" || len(v.Windows) != 1 || !v.FetchedAt.Equal(at) {
		t.Fatalf("%+v", v)
	}
	// Successful but old samples must be marked stale even without a failed
	// provider attempt, and use the configured sampling interval.
	m.mu.Lock()
	a := m.state.Accounts[id]
	a.Error = ""
	at = time.Now().Add(-25 * time.Minute)
	a.FetchedAt = &at
	m.state.Accounts[id] = a
	m.mu.Unlock()
	if v = s.cachedProfileUsage("credential"); !v.Stale || v.Error != "" {
		t.Fatalf("old success looks fresh: %+v", v)
	}
	m.mu.Lock()
	m.state.IntervalMinutes = 30
	m.mu.Unlock()
	if v = s.cachedProfileUsage("credential"); v.Stale {
		t.Fatalf("30-minute schedule marked stale early: %+v", v)
	}
	m.mu.Lock()
	a = m.state.Accounts[id]
	a.Active = false
	m.state.Accounts[id] = a
	m.mu.Unlock()
	if v = s.cachedProfileUsage("credential"); v.Available || v.AccountID != "" || v.Reason != "pending" {
		t.Fatalf("inactive cache reused: %+v", v)
	}
}

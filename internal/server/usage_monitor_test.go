package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
)

func TestMonitorCacheAndSettings(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	if code, _ := request(t, ts, "GET", "/api/usage/monitor", nil, ""); code != 401 {
		t.Fatal(code)
	}
	// A cache read and a settings change must never call a provider.
	s.monitor.identity = func(context.Context, string) (string, error) { t.Fatal("network from cache read"); return "", nil }
	code, body := request(t, ts, "GET", "/api/usage/monitor", nil, testToken)
	if code != 200 || !strings.Contains(string(body), `"intervalMinutes":10`) {
		t.Fatalf("%d %s", code, body)
	}
	code, body = request(t, ts, "PUT", "/api/usage/monitor/settings", map[string]int{"intervalMinutes": 30}, testToken)
	if code != 200 || !strings.Contains(string(body), `"intervalMinutes":30`) {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ = request(t, ts, "PUT", "/api/usage/monitor/settings", map[string]int{"intervalMinutes": 1}, testToken); code != 400 {
		t.Fatal(code)
	}
	var restored usageMonitor
	restored.mu.Lock()
	err := restored.loadLocked(s.cfg.DataDir)
	restored.mu.Unlock()
	if err != nil || restored.state.IntervalMinutes != 30 {
		t.Fatalf("restore: %v %+v", err, restored.state)
	}
}

func TestMonitorUnknownCredentialsStaySeparateUntilVerified(t *testing.T) {
	s, _, st := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	_, _ = s.monitorSnapshot()
	verified := false
	s.monitor.identity = func(context.Context, string) (string, error) {
		if !verified {
			return "", errors.New("scope unavailable")
		}
		return "real-account/org", nil
	}
	one, known, err := s.monitorClaudeIdentity(context.Background(), "token-one")
	if err != nil || known {
		t.Fatalf("%v %v", known, err)
	}
	two, _, _ := s.monitorClaudeIdentity(context.Background(), "token-two")
	if one == two {
		t.Fatal("unknown credentials merged")
	}
	at := time.Now().UTC()
	if err = s.recordMonitor(monitorAccount{ID: one, Provider: "claude", FetchedAt: &at, Windows: []codex.QuotaWindow{{Name: "5h", Percent: 42}}}, at); err != nil {
		t.Fatal(err)
	}
	verified = true
	binding := s.monitor.state.Bindings[monitorHash("token-one")]
	binding.RetryAt = time.Time{}
	s.monitor.state.Bindings[monitorHash("token-one")] = binding
	id, known, err := s.monitorClaudeIdentity(context.Background(), "token-one")
	if err != nil || !known || id == one {
		t.Fatalf("%s %v %v", id, known, err)
	}
	rows, err := st.AccountUsageSamples("monitor:"+id, at.Add(-time.Hour))
	if err != nil || len(rows) != 1 {
		t.Fatalf("own history not adopted: %v %v", rows, err)
	}
	rows, _ = st.AccountUsageSamples("monitor:"+one, at.Add(-time.Hour))
	if len(rows) != 0 {
		t.Fatal("isolated history duplicated")
	}
	if _, exists := s.monitor.state.Accounts[one]; exists {
		t.Fatal("obsolete summary remains")
	}
}

func TestMonitorCodexSwitchAndFailureGaps(t *testing.T) {
	clearProviderEnv(t)
	s, _, st := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	identity := "account-a/workspace"
	failed := false
	s.codexUsageCache.fetch = func(context.Context, claude.Options) (codex.SubscriptionUsage, error) {
		if failed {
			return codex.SubscriptionUsage{}, errors.New("private details")
		}
		return codex.SubscriptionUsage{Identity: identity, Available: true, Usage: &codex.SubscriptionWindows{Windows: []codex.QuotaWindow{{Name: "primary", Percent: 42}}}}, nil
	}
	refresh := func() {
		s.monitor.nextRefresh = time.Time{}
		s.codexUsageCache.retryAt = time.Time{}
		if err := s.refreshMonitor(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	identity = "account-b/workspace"
	refresh()
	v, _ := s.monitorSnapshot()
	if len(v.Accounts) != 2 {
		t.Fatalf("accounts mixed: %+v", v)
	}
	failed = true
	refresh()
	rows, err := st.AccountUsageSamples("monitor:"+monitorID("codex", identity), time.Now().Add(-time.Hour))
	if err != nil || len(rows) != 2 {
		t.Fatalf("missing failure gap: %v %v", rows, err)
	}
	var p monitorPoint
	_ = json.Unmarshal(rows[1].Data, &p)
	if p.Windows != nil || p.Error != "request-failed" || p.IntervalMinutes != 10 {
		t.Fatalf("failure fabricated quota: %+v", p)
	}
	failed = false
	identity = ""
	refresh()
	rows, _ = st.AccountUsageSamples("monitor:codex:identity-unavailable", time.Now().Add(-time.Hour))
	if len(rows) != 0 {
		t.Fatal("unknown Codex identity accumulated mixed history")
	}
	v, _ = s.monitorSnapshot()
	found := false
	for _, a := range v.Accounts {
		if a.ID == "codex:identity-unavailable" {
			found = a.Active && !a.IdentityKnown && len(a.Windows) == 1
		}
	}
	if !found {
		t.Fatalf("unknown CLI quota disappeared: %+v", v)
	}
}

func TestUsageReauthorizationFailureKeepsOriginalTimestamp(t *testing.T) {
	s, _, _ := setup(t, nil)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer api.Close()
	oldURL := claude.UsageURL
	claude.UsageURL = api.URL
	defer func() { claude.UsageURL = oldURL }()
	at := time.Now().Add(-time.Hour)
	c := &usageCache{loaded: true, tokenKey: "old-token", last: &claude.Usage{FiveHour: &claude.UsageWindow{Percent: 42}}, fetchedAt: at}
	v := s.readUsageOf("test", c, claude.OAuthToken{AccessToken: "new-token"}, usageFresh)
	if !v.Stale || v.FetchedAt == nil || !v.FetchedAt.Equal(at) || v.Usage.FiveHour.Percent != 42 {
		t.Fatalf("lost last successful observation: %+v", v)
	}
}

func TestMonitorCollectsWithoutAViewerAndCancelsOnShutdown(t *testing.T) {
	clearProviderEnv(t)
	started, canceled := make(chan struct{}), make(chan struct{})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/profile" {
			_, _ = w.Write([]byte(`{"account":{"uuid":"account"},"organization":{"uuid":"org"}}`))
			return
		}
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer api.Close()
	oldUsage, oldProfile := claude.UsageURL, claude.ProfileURL
	claude.UsageURL, claude.ProfileURL = api.URL+"/usage", api.URL+"/profile"
	defer func() { claude.UsageURL, claude.ProfileURL = oldUsage, oldProfile }()
	base, _, st := setup(t, nil)
	cfg := base.cfg
	cfg.DataDir = t.TempDir()
	cfg.ClaudeExecutable = "fixture-missing-claude"
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "oauth-profiles.json"), []byte(`[{"id":"bg","name":"Background","token":"sk-ant-oat-test"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(st, cfg)
	defer s.Close(context.Background())
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("collector did not start without a viewer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("shutdown left the provider request running")
	}
}

func TestMonitorIdentityContinuityAndIsolation(t *testing.T) {
	clearProviderEnv(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":42}}`))
	}))
	defer api.Close()
	old := claude.UsageURL
	claude.UsageURL = api.URL
	defer func() { claude.UsageURL = old }()
	s, ts, st := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	s.monitor.identity = func(_ context.Context, token string) (string, error) {
		if strings.Contains(token, "other") {
			return "account-b/org", nil
		}
		return "account-a/org", nil
	}
	create := func(name, token string) string {
		code, b := request(t, ts, "POST", "/api/profiles/oauth", map[string]string{"name": name, "token": token}, testToken)
		if code != 201 {
			t.Fatalf("%d %s", code, b)
		}
		var p struct{ ID string }
		_ = json.Unmarshal(b, &p)
		return p.ID
	}
	refresh := func() monitorReply {
		s.monitor.refreshMu.Lock()
		s.monitor.nextRefresh = time.Time{}
		s.monitor.refreshMu.Unlock()
		code, b := request(t, ts, "POST", "/api/usage/monitor/refresh", nil, testToken)
		if code != 200 {
			t.Fatalf("%d %s", code, b)
		}
		var v monitorReply
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	p := create("First", "sk-ant-oat-first")
	v := refresh()
	if len(v.Accounts) != 1 || !v.Accounts[0].IdentityKnown || len(v.Accounts[0].Windows) != 1 {
		t.Fatalf("%+v", v)
	}
	id := v.Accounts[0].ID
	if code, _ := request(t, ts, "DELETE", "/api/profiles/oauth/"+strings.TrimPrefix(p, "oauth:"), nil, testToken); code != 200 {
		t.Fatal(code)
	}
	create("Reauthorized", "sk-ant-oat-new-token")
	v = refresh()
	if len(v.Accounts) != 1 || v.Accounts[0].ID != id {
		t.Fatalf("history did not continue: %+v", v)
	}
	create("Different", "sk-ant-oat-other")
	v = refresh()
	if len(v.Accounts) != 2 {
		t.Fatalf("accounts mixed: %+v", v)
	}
	samples, err := st.AccountUsageSamples("monitor:"+id, time.Now().Add(-time.Hour))
	if err != nil || len(samples) == 0 {
		t.Fatalf("history lost: %v %v", samples, err)
	}
	code, b := request(t, ts, "GET", "/api/usage/monitor/history?account="+id+"&hours=24", nil, testToken)
	if code != 200 || strings.Contains(string(b), "sk-ant-oat") || !strings.Contains(string(b), `"percent":42`) {
		t.Fatalf("%d %s", code, b)
	}
	// Repeated manual refreshes honour the cooldown.
	before := s.monitor.nextRefresh
	request(t, ts, "POST", "/api/usage/monitor/refresh", nil, testToken)
	if !s.monitor.nextRefresh.Equal(before) {
		t.Fatal("cooldown bypassed")
	}
}

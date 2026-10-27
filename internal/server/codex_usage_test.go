package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
)

func TestCodexQuotaCacheAndStaleFallback(t *testing.T) {
	s, ts, _ := setup(t, nil)
	calls := 0
	s.codexUsageCache.fetch = func(context.Context, claude.Options) (codex.SubscriptionUsage, error) {
		calls++
		if calls > 1 {
			return codex.SubscriptionUsage{}, errors.New("private-credential-error")
		}
		return codex.SubscriptionUsage{Available: true, Plan: "plus", Usage: &codex.SubscriptionWindows{Windows: []codex.QuotaWindow{{Name: "primary", Percent: 42, WindowMinutes: 300}}}}, nil
	}
	read := func() codexUsageReply {
		t.Helper()
		code, b := request(t, ts, "GET", "/api/usage?provider=codex", nil, testToken)
		var reply codexUsageReply
		if code != 200 || json.Unmarshal(b, &reply) != nil {
			t.Fatalf("usage: %d %s", code, b)
		}
		if strings.Contains(string(b), "private-credential") {
			t.Fatal("raw CLI error exposed")
		}
		return reply
	}
	first := read()
	if !first.Available || first.FetchedAt == nil || first.Stale {
		t.Fatalf("first: %+v", first)
	}
	read()
	if calls != 1 {
		t.Fatalf("uncached fetches: %d", calls)
	}
	s.codexUsageCache.mu.Lock()
	s.codexUsageCache.retryAt = time.Time{}
	s.codexUsageCache.mu.Unlock()
	stale := read()
	if !stale.Available || !stale.Stale || stale.Error != "request-failed" || stale.Usage.Windows[0].Percent != 42 || !stale.FetchedAt.Equal(*first.FetchedAt) {
		t.Fatalf("lost cached reading: %+v", stale)
	}
	read()
	if calls != 2 {
		t.Fatal("error retry not throttled")
	}
}

func TestUsageMatchesSessionProviderAndSupplier(t *testing.T) {
	s, ts, st := setup(t, nil)
	s.codexUsageCache.fetch = func(context.Context, claude.Options) (codex.SubscriptionUsage, error) {
		t.Error("supplier used official quota")
		return codex.SubscriptionUsage{}, nil
	}
	for _, provider := range []string{"claude", "codex"} {
		x, err := st.CreateProvider(provider, t.TempDir(), "thread", false, provider)
		if err != nil {
			t.Fatal(err)
		}
		if err = st.SetProfile(x.ID, "supplier", `{"name":"Relay"}`); err != nil {
			t.Fatal(err)
		}
		code, b := request(t, ts, "GET", "/api/usage?provider="+provider+"&sessionId="+x.ID, nil, testToken)
		var result usageReply
		if code != 200 || json.Unmarshal(b, &result) != nil || result.Available || result.Reason != "third-party" {
			t.Fatalf("supplier: %d %s", code, b)
		}
		other := "codex"
		if provider == "codex" {
			other = "claude"
		}
		if code, _ = request(t, ts, "GET", "/api/usage?provider="+other+"&sessionId="+x.ID, nil, testToken); code != 404 {
			t.Fatal("cross-provider session accepted")
		}
	}
	if code, _ := request(t, ts, "GET", "/api/usage?provider=unknown", nil, testToken); code != 400 {
		t.Fatal("invalid provider accepted")
	}
}

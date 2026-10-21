package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"cc-remote/internal/claude"
)

func clearProviderEnv(t *testing.T) {
	for _, key := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

func writeLogin(t *testing.T, dir string, expires time.Time) {
	t.Helper()
	data := `{"claudeAiOauth":{"accessToken":"sk-ant-oat-test","refreshToken":"r","expiresAt":` + strconv.FormatInt(expires.UnixMilli(), 10) + `,"subscriptionType":"max"}}`
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func getUsage(t *testing.T, ts *httptest.Server) usageReply {
	t.Helper()
	code, b := request(t, ts, "GET", "/api/usage", nil, testToken)
	var r usageReply
	if code != 200 || json.Unmarshal(b, &r) != nil {
		t.Fatalf("usage %d %s", code, b)
	}
	return r
}

// The host reads the usage with the login, at most once a minute, records it
// in the history, and backs off when the service says too many requests.
func TestUsageReadsAndRecords(t *testing.T) {
	clearProviderEnv(t)
	var calls, status atomic.Int32
	status.Store(200)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer sk-ant-oat-test" || r.Header.Get("anthropic-beta") == "" {
			w.WriteHeader(401)
			return
		}
		if s := int(status.Load()); s != 200 {
			w.WriteHeader(s)
			return
		}
		_, _ = w.Write([]byte(`{"limits":[{"kind":"session","percent":13,"resets_at":"2026-10-21T18:50:00Z"},{"kind":"weekly_all","percent":9,"resets_at":"2026-10-28T10:00:00Z"}]}`))
	}))
	defer api.Close()
	old := claude.UsageURL
	claude.UsageURL = api.URL
	defer func() { claude.UsageURL = old }()

	s, ts, st := setup(t, nil)
	writeLogin(t, s.cfg.ClaudeConfigDir, time.Now().Add(time.Hour))
	r := getUsage(t, ts)
	if !r.Available || r.Plan != "max" || r.Usage == nil || r.Usage.FiveHour.Percent != 13 || r.Usage.Weekly.Percent != 9 || r.FetchedAt == nil || r.Stale {
		t.Fatalf("usage %+v", r)
	}
	if r = getUsage(t, ts); calls.Load() != 1 {
		t.Fatalf("read again within a minute: %d calls", calls.Load())
	}
	if samples, _ := st.UsageSamples(time.Now().Add(-time.Hour)); len(samples) != 1 {
		t.Fatalf("%d history samples", len(samples))
	}
	code, b := request(t, ts, "GET", "/api/usage/history?hours=24", nil, testToken)
	var h struct {
		Points []usagePoint `json:"points"`
	}
	if code != 200 || json.Unmarshal(b, &h) != nil || len(h.Points) != 1 || *h.Points[0].FiveHour != 13 || *h.Points[0].Weekly != 9 {
		t.Fatalf("history %d %s", code, b)
	}

	// Rate limited: the last reading stays, marked stale, and no request goes out for a while.
	status.Store(429)
	s.usageCache.mu.Lock()
	s.usageCache.fetchedAt = time.Now().Add(-2 * time.Minute)
	s.usageCache.mu.Unlock()
	if r = getUsage(t, ts); !r.Stale || r.Error != "rate-limited" || r.Usage == nil || r.Usage.FiveHour.Percent != 13 {
		t.Fatalf("rate limited %+v", r)
	}
	n := calls.Load()
	if getUsage(t, ts); calls.Load() != n {
		t.Fatal("asked again during the backoff")
	}

	// An expired login is not used (nor refreshed): the last reading stays.
	s.usageCache.mu.Lock()
	s.usageCache.retryAt = time.Time{}
	s.usageCache.mu.Unlock()
	writeLogin(t, s.cfg.ClaudeConfigDir, time.Now().Add(-time.Minute))
	if r = getUsage(t, ts); r.Error != "expired" || !r.Stale || r.Usage == nil || calls.Load() != n {
		t.Fatalf("expired %+v, %d calls", r, calls.Load())
	}
}

// Claude Code pointed at another provider, or billed per token, has no
// subscription usage: nothing is requested.
func TestUsageOnlyForOfficialLogin(t *testing.T) {
	clearProviderEnv(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("usage requested") }))
	defer api.Close()
	old := claude.UsageURL
	claude.UsageURL = api.URL
	defer func() { claude.UsageURL = old }()
	s, ts, _ := setup(t, nil)
	if r := getUsage(t, ts); r.Available || r.Reason != "no-login" {
		t.Fatalf("no login %+v", r)
	}
	writeLogin(t, s.cfg.ClaudeConfigDir, time.Now().Add(time.Hour))
	settings := filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")
	_ = os.WriteFile(settings, []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://open.bigmodel.cn/api/anthropic","ANTHROPIC_AUTH_TOKEN":"x"}}`), 0600)
	if r := getUsage(t, ts); r.Available || r.Reason != "third-party" || r.Provider != "open.bigmodel.cn" {
		t.Fatalf("third party %+v", r)
	}
	_ = os.WriteFile(settings, []byte(`{"env":{"ANTHROPIC_API_KEY":"sk-ant-api"}}`), 0600)
	if r := getUsage(t, ts); r.Available || r.Reason != "api-key" {
		t.Fatalf("api key %+v", r)
	}
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	if r := getUsage(t, ts); r.Reason != "third-party" || r.Provider != "Amazon Bedrock" {
		t.Fatalf("bedrock %+v", r)
	}
}

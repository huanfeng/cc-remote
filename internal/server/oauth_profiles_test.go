package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

func TestOAuthProfileLifecycleKeepsTokenOutOfListsAndSessions(t *testing.T) {
	s, ts, st := setup(t, nil)
	s.cfg.DataDir = t.TempDir()

	for _, bad := range []map[string]string{{"name": "", "token": "sk-ant-oat01-x"}, {"name": "a", "token": "sk-ant-api03-x"}, {"name": "a", "token": "sk-ant-oat01 x"}} {
		if code, _ := request(t, ts, "POST", "/api/profiles/oauth", bad, testToken); code != 400 {
			t.Fatalf("%v accepted: %d", bad, code)
		}
	}
	code, b := request(t, ts, "POST", "/api/profiles/oauth", map[string]string{"name": "Team", "token": "sk-ant-oat01-first"}, testToken)
	if code != 201 || strings.Contains(string(b), "first") {
		t.Fatalf("create %d %s", code, b)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(b, &created)
	if !strings.HasPrefix(created.ID, "oauth:") {
		t.Fatal("id", created.ID)
	}
	if code, _ = request(t, ts, "POST", "/api/profiles/oauth", map[string]string{"name": "team", "token": "sk-ant-oat01-other"}, testToken); code != 409 {
		t.Fatal("duplicate name accepted", code)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(s.cfg.DataDir, oauthFileName)); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("token store must be 0600", err)
		}
	}

	code, b = request(t, ts, "GET", "/api/profiles", nil, testToken)
	if code != 200 || strings.Contains(string(b), "first") || !strings.Contains(string(b), `"kind":"oauth"`) || !strings.Contains(string(b), created.ID) {
		t.Fatalf("catalog %d %s", code, b)
	}
	if code, b = request(t, ts, "GET", "/api/profiles/"+created.ID+"/env", nil, testToken); code != 200 || !strings.Contains(string(b), "sk-ant-oat01-first") {
		t.Fatalf("env endpoint %d %s", code, b)
	}

	code, b = request(t, ts, "POST", "/api/sessions", map[string]string{"cwd": t.TempDir(), "profileId": created.ID}, testToken)
	if code != 201 {
		t.Fatalf("session %d %s", code, b)
	}
	var x store.Session
	_ = json.Unmarshal(b, &x)
	x, _ = st.Get(x.ID)
	if strings.Contains(x.ProfileJSON, "first") || x.ProfileName != "Team" {
		t.Fatal("session snapshot must hold the reference only:", x.ProfileJSON)
	}
	launch := func() (claude.Options, error) {
		o := claude.Options{}
		return o, s.profileOptions(x, &o)
	}
	if o, err := launch(); err != nil || o.ProfileEnv[claude.OAuthEnvKey] != "sk-ant-oat01-first" || len(o.ProfileEnv) != 4 || o.ProfileEnv["ANTHROPIC_API_KEY"] != "" || o.ProfileEnv["ANTHROPIC_BASE_URL"] != "" {
		t.Fatal("launch env", o.ProfileEnv, err)
	}

	if code, _ = request(t, ts, "PATCH", "/api/profiles/oauth/"+strings.TrimPrefix(created.ID, "oauth:"), map[string]string{"token": "sk-ant-oat01-second"}, testToken); code != 200 {
		t.Fatal("patch", code)
	}
	if o, err := launch(); err != nil || o.ProfileEnv[claude.OAuthEnvKey] != "sk-ant-oat01-second" {
		t.Fatal("replaced token must apply to existing sessions at next launch", o.ProfileEnv, err)
	}

	if code, _ = request(t, ts, "DELETE", "/api/profiles/oauth/"+strings.TrimPrefix(created.ID, "oauth:"), nil, testToken); code != 200 {
		t.Fatal("delete", code)
	}
	if _, err := launch(); err == nil || !strings.Contains(err.Error(), "已删除") {
		t.Fatal("a deleted entry must fail the launch loudly:", err)
	}
	if code, _ = request(t, ts, "DELETE", "/api/profiles/oauth/missing", nil, testToken); code != 404 {
		t.Fatal("missing entry", code)
	}
	if code, _ = request(t, ts, "POST", "/api/profiles/oauth", map[string]string{"name": "x", "token": "sk-ant-oat01-x"}, ""); code != 401 {
		t.Fatal("unauthenticated write", code)
	}
}

func TestOAuthListSurvivesBrokenCatalogAndTokenChangeRekeysLaunch(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	s.cfg.CCSwitchDBPath = s.cfg.DataDir // a directory: the CC-Switch read fails
	code, b := request(t, ts, "POST", "/api/profiles/oauth", map[string]string{"name": "Team", "token": "sk-ant-oat01-first"}, testToken)
	if code != 201 {
		t.Fatal(code, string(b))
	}
	if code, b = request(t, ts, "GET", "/api/profiles/oauth", nil, testToken); code != 200 || !strings.Contains(string(b), "Team") || strings.Contains(string(b), "first") {
		t.Fatalf("oauth list %d %s", code, b)
	}
	if tokenFingerprint("a", nil) == tokenFingerprint("b", nil) || tokenFingerprint("", nil) != "" || tokenFingerprint("a", os.ErrNotExist) == tokenFingerprint("a", nil) {
		t.Fatal("fingerprint must follow the token and the launch error")
	}
}

// An OAuth session shows its own account's usage, apart from the host's login.
func TestOAuthUsageIsReadPerAccount(t *testing.T) {
	clearProviderEnv(t)
	var hostCalls, accountCalls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer sk-ant-oat01-account" {
			accountCalls.Add(1)
			_, _ = w.Write([]byte(`{"limits":[{"kind":"session","percent":42,"resets_at":"2026-11-07T10:00:00Z"},{"kind":"weekly_all","percent":7,"resets_at":"2026-11-10T10:00:00Z"}]}`))
			return
		}
		hostCalls.Add(1)
		_, _ = w.Write([]byte(`{"limits":[{"kind":"session","percent":1,"resets_at":"2026-11-07T10:00:00Z"}]}`))
	}))
	defer api.Close()
	old := claude.UsageURL
	claude.UsageURL = api.URL
	defer func() { claude.UsageURL = old }()

	s, ts, st := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	writeLogin(t, s.cfg.ClaudeConfigDir, time.Now().Add(time.Hour))
	_, b := request(t, ts, "POST", "/api/profiles/oauth", map[string]string{"name": "Team", "token": "sk-ant-oat01-account"}, testToken)
	var created struct{ ID string }
	_ = json.Unmarshal(b, &created)
	code, b := request(t, ts, "POST", "/api/sessions", map[string]string{"cwd": t.TempDir(), "profileId": created.ID}, testToken)
	var x store.Session
	_ = json.Unmarshal(b, &x)
	if code != 201 {
		t.Fatal(code, string(b))
	}

	var r usageReply
	code, b = request(t, ts, "GET", "/api/usage?sessionId="+x.ID, nil, testToken)
	if code != 200 || json.Unmarshal(b, &r) != nil || !r.Available || r.Usage.FiveHour.Percent != 42 || r.Account != "Team" || r.Profile != created.ID || strings.Contains(string(b), "sk-ant") {
		t.Fatalf("session usage %d %s", code, b)
	}
	if got := getUsage(t, ts); got.Usage.FiveHour.Percent != 1 {
		t.Fatalf("the host login must keep its own reading: %+v", got.Usage)
	}
	if hostCalls.Load() != 1 || accountCalls.Load() != 1 {
		t.Fatalf("calls host=%d account=%d", hostCalls.Load(), accountCalls.Load())
	}
	// Explicit profile query, and a history of its own.
	if code, b = request(t, ts, "GET", "/api/usage?profile="+created.ID, nil, testToken); code != 200 || !strings.Contains(string(b), `"percent":42`) {
		t.Fatalf("profile usage %d %s", code, b)
	}
	if samples, _ := st.AccountUsageSamples(created.ID, time.Now().Add(-time.Hour)); len(samples) != 1 {
		t.Fatalf("%d account samples", len(samples))
	}
	code, b = request(t, ts, "GET", "/api/usage/history?hours=1&profile="+created.ID, nil, testToken)
	if code != 200 || !strings.Contains(string(b), `"fiveHour":42`) {
		t.Fatalf("history %d %s", code, b)
	}
	if code, b = request(t, ts, "GET", "/api/usage/history?hours=1", nil, testToken); strings.Contains(string(b), `"fiveHour":42`) {
		t.Fatalf("the host history must not include the account %d %s", code, b)
	}

	// Removing the profile forgets its readings.
	request(t, ts, "DELETE", "/api/profiles/oauth/"+strings.TrimPrefix(created.ID, "oauth:"), nil, testToken)
	if samples, _ := st.AccountUsageSamples(created.ID, time.Now().Add(-time.Hour)); len(samples) != 0 {
		t.Fatal("history of a removed account must go")
	}
	if r = s.readOAuthUsage(created.ID, 0); r.Available || r.Reason != "no-login" {
		t.Fatalf("a removed profile has no usage: %+v", r)
	}
}

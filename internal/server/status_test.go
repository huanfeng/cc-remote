package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatusShapeAndAuth(t *testing.T) {
	s, ts, st := setup(t, nil)
	dir := t.TempDir()
	s.cfg.DataDir, s.cfg.Version, s.cfg.StartedAt = dir, "1.2.3", time.Now().Add(-time.Minute)
	s.listen = "127.0.0.1:8787, 192.168.1.20:8787"
	s.cfg.ClaudeExecutable = "cc-remote-missing-claude-binary"
	s.cfg.LogFile = filepath.Join(dir, "host.log")
	if err := os.WriteFile(s.cfg.LogFile, []byte("start\nAuthorization: Bearer abc.def\ntoken "+testToken+"\nlast\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CC_REMOTE_SERVICE_MANAGER", "schtasks")
	x := createSession(t, ts)
	payload := json.RawMessage(`{"type":"result","total_cost_usd":0.5,"duration_ms":900,"modelUsage":{"claude-sonnet-4-6":{"inputTokens":10,"outputTokens":5}}}`)
	_, _ = st.Append(x.ID, "user", "hi", nil)
	_, _ = st.Append(x.ID, "result", "done", payload)

	if code, _ := request(t, ts, "GET", "/api/status", nil, ""); code != 401 {
		t.Fatalf("unauthenticated status: %d", code)
	}
	code, b := request(t, ts, "GET", "/api/status", nil, testToken)
	if code != 200 {
		t.Fatalf("status: %d %s", code, b)
	}
	if strings.Contains(string(b), testToken) || strings.Contains(string(b), "abc.def") {
		t.Fatalf("status leaks secret: %s", b)
	}
	var v struct {
		Version         string   `json:"version"`
		Pid             int      `json:"pid"`
		UptimeSeconds   int64    `json:"uptimeSeconds"`
		ListenAddresses []string `json:"listenAddresses"`
		Listeners       []struct {
			Address  string `json:"address"`
			Loopback bool   `json:"loopback"`
			Insecure bool   `json:"insecure"`
		} `json:"listeners"`
		Claude struct {
			Executable, Error string
		} `json:"claude"`
		Service struct {
			Managed bool   `json:"managed"`
			Manager string `json:"manager"`
		} `json:"service"`
		Sessions map[string]int `json:"sessions"`
		Storage  map[string]int64
		Totals   struct {
			Turns     int     `json:"turns"`
			CostUSD   float64 `json:"costUSD"`
			CostKnown bool    `json:"costKnown"`
		} `json:"totals"`
		Daily        []json.RawMessage `json:"daily"`
		SessionStats []struct {
			ID          string `json:"id"`
			Mode        string `json:"mode"`
			Turns       int    `json:"turns"`
			InputTokens int64  `json:"inputTokens"`
		} `json:"sessionStats"`
		LogTail []string `json:"logTail"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v.Version != "1.2.3" || v.Pid != os.Getpid() || v.UptimeSeconds < 59 || len(v.ListenAddresses) != 2 || v.ListenAddresses[1] != "192.168.1.20:8787" ||
		!v.Listeners[0].Loopback || v.Listeners[1].Loopback || v.Listeners[0].Insecure || !v.Listeners[1].Insecure || v.Claude.Error == "" || !v.Service.Managed || v.Service.Manager != "schtasks" {
		t.Fatalf("status host fields: %s", b)
	}
	if v.Sessions["total"] != 1 || v.Sessions["managed"] != 1 || v.Sessions["idle"] != 1 || v.Totals.Turns != 1 || v.Totals.CostUSD != 0.5 || !v.Totals.CostKnown || len(v.Daily) != 14 {
		t.Fatalf("status stats: %s", b)
	}
	if len(v.SessionStats) != 1 || v.SessionStats[0].ID != x.ID || v.SessionStats[0].Mode != "managed" || v.SessionStats[0].InputTokens != 10 {
		t.Fatalf("session stats: %s", b)
	}
	if v.Storage["databaseBytes"] != 0 || v.Storage["logBytes"] == 0 || len(v.LogTail) != 4 || v.LogTail[0] != "start" || v.LogTail[1] != "Authorization: Bearer [redacted]" || v.LogTail[2] != "token [redacted]" {
		t.Fatalf("storage/log: %s", b)
	}

	code, b = request(t, ts, "GET", "/api/sessions/"+x.ID+"/stats", nil, testToken)
	if code != 200 || !strings.Contains(string(b), `"turns":1`) {
		t.Fatalf("session stats: %d %s", code, b)
	}
	if code, _ = request(t, ts, "GET", "/api/sessions/missing/stats", nil, testToken); code != 404 {
		t.Fatalf("missing session stats: %d", code)
	}
}

func TestInfoVersion(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.ClaudeExecutable = "cc-remote-missing-claude-binary"
	for _, want := range []string{"0.2.0-dev.4", ""} {
		s.cfg.Version = want
		code, b := request(t, ts, "GET", "/api/info", nil, testToken)
		var v struct {
			Version         *string `json:"version"`
			ClaudeAvailable bool    `json:"claudeAvailable"`
		}
		if code != 200 || json.Unmarshal(b, &v) != nil || v.Version == nil || *v.Version != want || v.ClaudeAvailable {
			t.Fatalf("info %q: %d %s", want, code, b)
		}
	}
	if code, _ := request(t, ts, "GET", "/api/info", nil, ""); code != 401 {
		t.Fatalf("unauthenticated info: %d", code)
	}
}

func TestLogTailBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.log")
	if got := logTail(path, "x", 80); len(got) != 0 {
		t.Fatalf("missing log: %v", got)
	}
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		b.WriteString("line with a hex token 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\r\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	got := logTail(path, "", 80)
	if len(got) != 80 || got[79] != "line with a hex token [redacted]" {
		t.Fatalf("tail: %d %q", len(got), got[len(got)-1])
	}
}

func TestShutdown(t *testing.T) {
	s, ts, _ := setup(t, nil)
	if code, _ := request(t, ts, "POST", "/api/admin/shutdown", nil, ""); code != 401 {
		t.Fatalf("unauthenticated shutdown: %d", code)
	}
	if code, _ := request(t, ts, "POST", "/api/admin/shutdown", nil, testToken); code != 501 {
		t.Fatalf("nil shutdown callback: %d", code)
	}
	called := make(chan struct{})
	s.cfg.Shutdown = func() { close(called) }
	// A non-loopback client is refused even with a valid token.
	req := httptest.NewRequest("POST", "/api/admin/shutdown", nil)
	req.RemoteAddr = "192.168.1.30:50000"
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("remote shutdown: %d", rec.Code)
	}
	// A loopback peer relaying for someone else (local reverse proxy) is refused too.
	for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-IP", "X-Forwarded-Proto", "Via"} {
		req = httptest.NewRequest("POST", "/api/admin/shutdown", nil)
		req.RemoteAddr = "127.0.0.1:50000"
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set(h, "203.0.113.9")
		rec = httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("proxied shutdown via %s: %d", h, rec.Code)
		}
	}
	if code, b := request(t, ts, "POST", "/api/admin/shutdown", nil, testToken); code != 202 {
		t.Fatalf("loopback shutdown: %d %s", code, b)
	}
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown callback not called")
	}
}

package server

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A restart after an update waits for busy sessions, then runs once.
func TestRestartWaitsForIdleSessions(t *testing.T) {
	old := restartPoll
	restartPoll = 50 * time.Millisecond
	t.Cleanup(func() { restartPoll = old })
	s, ts, st := setup(t, noRun)
	if code, _ := request(t, ts, "POST", "/api/admin/restart", map[string]string{"when": "idle"}, testToken); code != 501 {
		t.Fatalf("restart without a service manager: %d", code)
	}
	called := make(chan struct{}, 2)
	s.cfg.Restart = func() { called <- struct{}{} }
	x, err := st.Create("busy work", t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.SetStatus(x.ID, "running")
	code, b := request(t, ts, "POST", "/api/admin/restart", map[string]string{"when": "idle"}, testToken)
	if code != 202 || !strings.Contains(string(b), `"waiting"`) || !strings.Contains(string(b), "busy work") {
		t.Fatalf("restart while busy: %d %s", code, b)
	}
	if _, b := request(t, ts, "GET", "/api/info", nil, testToken); !strings.Contains(string(b), `"restartPending":true`) {
		t.Fatalf("info does not show the pending restart: %s", b)
	}
	select {
	case <-called:
		t.Fatal("restarted while a session was running")
	case <-time.After(300 * time.Millisecond):
	}
	_ = st.SetStatus(x.ID, "completed")
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("no restart after the session became idle")
	}
	// A second request while one is pending does not start another watcher.
	if code, _ := request(t, ts, "POST", "/api/admin/restart", map[string]string{"when": "idle"}, testToken); code != 202 {
		t.Fatalf("repeated restart: %d", code)
	}
}

// A reload reports what still needs a restart and updates the bound addresses.
func TestReloadAppliesListen(t *testing.T) {
	s, ts, _ := setup(t, noRun)
	if code, _ := request(t, ts, "POST", "/api/admin/reload", nil, testToken); code != 501 {
		t.Fatalf("reload without a service manager: %d", code)
	}
	s.cfg.Reload = func() (ReloadResult, error) {
		return ReloadResult{Listen: []string{"127.0.0.1:8787", "192.168.1.5:8787"}, Restart: []string{"env"}}, nil
	}
	code, b := request(t, ts, "POST", "/api/admin/reload", nil, testToken)
	if code != 200 || !strings.Contains(string(b), `"restart":["env"]`) {
		t.Fatalf("reload: %d %s", code, b)
	}
	if _, b = request(t, ts, "GET", "/api/status", nil, testToken); !strings.Contains(string(b), `"listen":"127.0.0.1:8787,192.168.1.5:8787"`) {
		t.Fatalf("status listen after reload: %s", b)
	}
	s.cfg.Reload = func() (ReloadResult, error) { return ReloadResult{}, errors.New("listen on x: in use") }
	if code, b = request(t, ts, "POST", "/api/admin/reload", nil, testToken); code != 400 || !strings.Contains(string(b), "in use") {
		t.Fatalf("failed reload: %d %s", code, b)
	}
}

// A reload replaces the allowed web frontends at once; one that names none
// (nil) keeps them.
func TestReloadAppliesAllowOrigins(t *testing.T) {
	s, ts, _ := setup(t, noRun)
	s.cfg.AllowOrigins = []string{"https://old.example"}
	preflight := func(origin string) string {
		req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/api/info", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "GET")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("Access-Control-Allow-Origin")
	}
	if preflight("https://old.example") == "" || preflight("https://huanfeng.github.io") != "" {
		t.Fatal("before the reload")
	}
	s.cfg.Reload = func() (ReloadResult, error) {
		return ReloadResult{Listen: []string{"127.0.0.1:8787"}, AllowOrigins: []string{"https://huanfeng.github.io"}}, nil
	}
	if code, b := request(t, ts, "POST", "/api/admin/reload", nil, testToken); code != 200 {
		t.Fatalf("reload: %d %s", code, b)
	}
	if preflight("https://huanfeng.github.io") != "https://huanfeng.github.io" || preflight("https://old.example") != "" {
		t.Fatal("after the reload")
	}
	s.cfg.Reload = func() (ReloadResult, error) { return ReloadResult{Listen: []string{"127.0.0.1:8787"}}, nil }
	if code, _ := request(t, ts, "POST", "/api/admin/reload", nil, testToken); code != 200 || preflight("https://huanfeng.github.io") == "" {
		t.Fatal("a reload without origins dropped them")
	}
}

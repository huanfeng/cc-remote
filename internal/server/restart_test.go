package server

import (
	"errors"
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

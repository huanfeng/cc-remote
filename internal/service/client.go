package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cc-remote/internal/hostauth"
	"cc-remote/internal/i18n"
	"cc-remote/internal/listen"
)

// errNotService is the answer of a host that runs without a service manager
// (started by hand), which can neither reload nor restart itself.
var errNotService = errors.New("the host is not running as a service")

// managerDisabled, when set, refuses every service manager command (schtasks,
// systemctl, launchctl). Tests set it: they must never change the real service.
var managerDisabled error

func readToken(dataDir string) string {
	if t := strings.TrimSpace(os.Getenv("CC_REMOTE_TOKEN")); t != "" {
		return t
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "access-token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

var httpClient = &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}

func request(method, url, token string, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := *httpClient
	client.Timeout = timeout
	return client.Do(req)
}

// findHost returns the first local URL where this data dir's host answers,
// having it prove it holds the token before the token is sent there.
// answering reports whether anything listened at all.
func findHost(cfg *Config) (url string, answering bool) {
	token := readToken(cfg.DataDir)
	client := *httpClient
	client.Timeout = time.Second
	for _, u := range listen.ProbeURLs(cfg.Listen) {
		err := hostauth.Verify(&client, u, token, cfg.DataDir)
		if err == nil {
			return u, true
		}
		answering = answering || errors.Is(err, hostauth.ErrWrongToken) || errors.Is(err, hostauth.ErrNotHost)
	}
	return "", answering
}

func waitReady(cfg *Config, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for {
		if u, _ := findHost(cfg); u != "" {
			return u, true
		}
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func waitStopped(cfg *Config, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if _, answering := findHost(cfg); !answering {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// gracefulShutdown asks the host to exit with code 0 so no supervisor restarts it.
func gracefulShutdown(url, token string) error {
	resp, err := request("POST", url+"/api/admin/shutdown", token, 5*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return i18n.Errorf("shutdown endpoint returned %s %s", "停止接口返回 %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}

// requestRestart asks the running host to restart once no session is busy,
// so it runs the newly installed binary. It reports the busy sessions, or
// supported=false for a host too old to restart itself.
func requestRestart(url, token string) (busy []string, supported bool, err error) {
	req, err := http.NewRequest("POST", url+"/api/admin/restart", strings.NewReader(`{"when":"idle"}`))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := *httpClient
	client.Timeout = 5 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotImplemented {
		return nil, true, errNotService
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, false, nil
	}
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, true, i18n.Errorf("restart endpoint returned %s %s", "重启接口返回 %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var out struct {
		Busy []string `json:"busy"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
	return out.Busy, true, nil
}

// requestReload asks the running host to apply the saved service.json. It
// returns the addresses bound now and the settings that need a restart, or
// supported=false for a host too old to reload.
func requestReload(url, token string) (bound, restart []string, supported bool, err error) {
	resp, err := request("POST", url+"/api/admin/reload", token, 10*time.Second)
	if err != nil {
		return nil, nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotImplemented {
		return nil, nil, true, errNotService
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, nil, false, nil
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if json.Unmarshal(b, &e) != nil || e.Error == "" {
			e.Error = resp.Status + " " + strings.TrimSpace(string(b))
		}
		return nil, nil, true, errors.New(e.Error)
	}
	var out struct {
		Listen  []string `json:"listen"`
		Restart []string `json:"restart"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, nil, true, err
	}
	return out.Listen, out.Restart, true, nil
}

// fetchStatus reads /api/status, falling back to /api/info on older hosts.
func fetchStatus(url, token string) (map[string]any, string, error) {
	for _, path := range []string{"/api/status", "/api/info"} {
		resp, err := request("GET", url+path, token, 10*time.Second)
		if err != nil {
			return nil, "", err
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			continue
		}
		var v map[string]any
		err = json.NewDecoder(resp.Body).Decode(&v)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, path, i18n.Errorf("%s returned %s", "%s 返回 %s", path, resp.Status)
		}
		return v, path, err
	}
	return nil, "", i18n.Errorf("host has neither /api/status nor /api/info", "宿主既没有 /api/status 也没有 /api/info")
}

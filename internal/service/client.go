package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cc-remote/internal/listen"
)

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

// findHost returns the first local URL where a host accepts this data dir's token.
// answering reports whether anything listened at all.
func findHost(cfg *Config) (url string, answering bool) {
	token := readToken(cfg.DataDir)
	for _, u := range listen.ProbeURLs(cfg.Listen) {
		resp, err := request("GET", u+"/api/info", token, time.Second)
		if err != nil {
			continue
		}
		resp.Body.Close()
		answering = true
		if resp.StatusCode == http.StatusOK {
			return u, true
		}
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
		return fmt.Errorf("shutdown endpoint returned %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
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
			return nil, path, fmt.Errorf("%s returned %s", path, resp.Status)
		}
		return v, path, err
	}
	return nil, "", fmt.Errorf("host has neither /api/status nor /api/info")
}

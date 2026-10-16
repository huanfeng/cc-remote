package channel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ensureHost never starts a replacement when another service answers the port.
// Authenticating with this directory's token prevents binding to the wrong host.
func ensureHost(dataDir, host, claudeBin, configDir string) error {
	probe := func() (bool, error) {
		token, tokenErr := tokenFrom(dataDir, "")
		request, err := http.NewRequest("GET", host+"/api/sessions", nil)
		if err != nil {
			return false, err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		client := localClient()
		client.Timeout = time.Second
		response, err := client.Do(request)
		if err != nil {
			return false, nil
		}
		defer response.Body.Close()
		if tokenErr != nil || token == "" || response.StatusCode != http.StatusOK {
			return false, fmt.Errorf("a service is already listening at %s but does not accept this data directory's token; use the matching --data-dir or a different --host", host)
		}
		var sessions []json.RawMessage
		if err := json.NewDecoder(response.Body).Decode(&sessions); err != nil {
			return false, fmt.Errorf("service at %s did not return a CC Remote session list: %w", host, err)
		}
		return true, nil
	}
	if ok, err := probe(); ok || err != nil {
		return err
	}
	u, _ := url.Parse(host)
	if u.Scheme != "http" {
		return fmt.Errorf("host %s is unavailable; start the HTTPS host separately", host)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"--listen", u.Host, "--data-dir", dataDir, "--claude", claudeBin}
	if configDir != "" {
		args = append(args, "--claude-config-dir", configDir)
	}
	// The host writes and rotates host.log itself; its stdio is discarded.
	args = append(args, "--log-file", filepath.Join(dataDir, "host.log"))
	cmd := exec.Command(exe, args...)
	detachHost(cmd)
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("start host: %w", err)
	}
	_ = cmd.Process.Release()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if ok, err := probe(); ok || err != nil {
			return err
		}
	}
	return fmt.Errorf("host did not become ready at %s; see %s", host, filepath.Join(dataDir, "host.log"))
}

// EnsureHost makes sure a host for dataDir answers at host, starting one in
// the background when none runs (as ccr does for shared sessions).
func EnsureHost(dataDir, host string) error {
	return ensureHost(dataDir, defaultHost(host), "claude", "")
}

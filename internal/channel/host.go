package channel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"cc-remote/internal/i18n"
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
			return false, i18n.Errorf("a service is already listening at %s but does not accept this data directory's token; use the matching --data-dir or a different --host", "%s 上已有服务在监听，但它不接受此数据目录的令牌；请使用对应的 --data-dir 或换一个 --host", host)
		}
		var sessions []json.RawMessage
		if err := json.NewDecoder(response.Body).Decode(&sessions); err != nil {
			return false, i18n.Errorf("service at %s did not return a CC Remote session list: %w", "%s 上的服务没有返回 CC Remote 会话列表：%w", host, err)
		}
		return true, nil
	}
	if ok, err := probe(); ok || err != nil {
		return err
	}
	u, _ := url.Parse(host)
	if u.Scheme != "http" {
		return i18n.Errorf("host %s is unavailable; start the HTTPS host separately", "宿主 %s 不可用；请另行启动 HTTPS 宿主", host)
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
		return i18n.Errorf("start host: %w", "启动宿主失败：%w", err)
	}
	_ = cmd.Process.Release()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if ok, err := probe(); ok || err != nil {
			return err
		}
	}
	return i18n.Errorf("host did not become ready at %s; see %s", "宿主没有在 %s 就绪；请查看 %s", host, filepath.Join(dataDir, "host.log"))
}

// EnsureHost makes sure a host for dataDir answers at host, starting one in
// the background when none runs (as ccr does before attaching).
func EnsureHost(dataDir, host string) error {
	return ensureHost(dataDir, defaultHost(host), "claude", "")
}

func defaultHost(host string) string {
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return "http://127.0.0.1:8787"
	}
	return host
}

func tokenFrom(dataDir, token string) (string, error) {
	if token == "" {
		token = os.Getenv("CC_REMOTE_TOKEN")
	}
	token = strings.TrimSpace(token)
	if token != "" {
		return token, nil
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "access-token"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

type hostError int

func (e hostError) Error() string { return fmt.Sprintf("host returned HTTP %d", int(e)) }
func validateHost(host string) error {
	u, err := url.Parse(host)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return errors.New("channel host must be a loopback HTTP(S) origin")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("channel host must use localhost or a loopback IP")
	}
	return nil
}
func localClient() *http.Client {
	return &http.Client{Timeout: 35 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("channel HTTP redirects are not allowed")
	}}
}

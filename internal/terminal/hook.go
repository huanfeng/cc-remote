package terminal

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"
)

// HookConfig is the file a terminal session's hooks read (CC_REMOTE_TERMINAL_FILE).
type HookConfig struct {
	Host    string `json:"host"`
	Token   string `json:"token"`
	Session string `json:"session"`
}

// RunHook is `cc-remote terminal-hook`: it forwards a Claude Code hook event
// (SessionStart, UserPromptSubmit, Stop, Notification, SubagentStop) to the
// host. It never prints and always succeeds, so Claude Code is never blocked
// or changed by it.
func RunHook() error {
	input, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil || !json.Valid(input) {
		return nil
	}
	raw, err := os.ReadFile(os.Getenv("CC_REMOTE_TERMINAL_FILE"))
	if err != nil {
		return nil
	}
	var cfg HookConfig
	if json.Unmarshal(raw, &cfg) != nil || cfg.Host == "" || cfg.Session == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodPost, cfg.Host+"/api/terminals/"+cfg.Session+"/hook", bytes.NewReader(input))
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
	return nil
}

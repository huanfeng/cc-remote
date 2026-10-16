package terminal

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// HookConfig is the file a terminal session's hooks read (CC_REMOTE_TERMINAL_FILE).
type HookConfig struct {
	Host    string `json:"host"`
	Token   string `json:"token"`
	Session string `json:"session"`
	// StatusCommand is the user's own status line, which terminal-status runs.
	StatusCommand string `json:"statusCommand,omitempty"`
}

func readHookConfig() (HookConfig, bool) {
	var cfg HookConfig
	raw, err := os.ReadFile(os.Getenv("CC_REMOTE_TERMINAL_FILE"))
	if err != nil || json.Unmarshal(raw, &cfg) != nil || cfg.Host == "" || cfg.Session == "" {
		return cfg, false
	}
	return cfg, true
}

func post(cfg HookConfig, path string, body []byte, timeout time.Duration) {
	req, err := http.NewRequest(http.MethodPost, cfg.Host+"/api/terminals/"+cfg.Session+path, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil}}
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
}

// RunStatus is `cc-remote terminal-status`, a terminal session's status line:
// it hands Claude Code's status data (model, context window, cost, duration)
// to the host and prints the user's own status line, so the terminal looks
// the same as without cc-remote.
func RunStatus() error {
	input, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	cfg, ok := readHookConfig()
	if !ok {
		return nil
	}
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		if json.Valid(input) {
			post(cfg, "/status", input, time.Second)
		}
	}()
	if cfg.StatusCommand != "" {
		c := shellCommand(cfg.StatusCommand)
		c.Stdin = bytes.NewReader(input)
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		_ = c.Run()
	}
	<-sent
	return nil
}

// shellCommand runs a status line command the way Claude Code does: with
// bash (Git Bash on Windows) when there is one.
func shellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		bash := os.Getenv("CLAUDE_CODE_GIT_BASH_PATH")
		if bash == "" {
			bash, _ = exec.LookPath("bash")
		}
		if bash != "" {
			return exec.Command(bash, "-c", command)
		}
		return exec.Command(os.Getenv("ComSpec"), "/d", "/c", command)
	}
	return exec.Command("/bin/sh", "-c", command)
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
	if cfg, ok := readHookConfig(); ok {
		post(cfg, "/hook", input, 5*time.Second)
	}
	return nil
}

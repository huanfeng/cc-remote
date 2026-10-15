package channel

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"cc-remote/internal/claude"
	"cc-remote/internal/config"
	"cc-remote/internal/hostlock"
	"cc-remote/internal/prefs"
	"cc-remote/internal/store"
)

var launchUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func Launch(args []string) error {
	fs := flag.NewFlagSet("terminal", flag.ContinueOnError)
	host := fs.String("host", "http://127.0.0.1:8787", "cc-remote host URL")
	dataDir := fs.String("data-dir", config.DefaultDataDir(), "cc-remote data directory (or CC_REMOTE_DATA_DIR)")
	claudeBin := fs.String("claude", "claude", "Claude Code executable path")
	configDir := fs.String("claude-config-dir", "", "Claude config directory")
	resume := fs.String("resume", "", "Claude session UUID to resume")
	if err := fs.Parse(args); err != nil {
		return err
	}
	nativeArgs, sessionID, err := prepareNativeArgs(fs.Args(), *resume)
	if err != nil {
		return err
	}
	if err := validateHost(defaultHost(*host)); err != nil {
		return err
	}
	absData, err := filepath.Abs(*dataDir)
	if err != nil {
		return err
	}
	// Like managed runs, a corrupt preferences file is reported and ignored.
	p, _, err := prefs.Load(absData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告：无法读取 CC Remote 默认设置，本次不应用：%v\n", err)
		p = prefs.Preferences{}
	}
	if warning := inheritedDangerWarning(nativeArgs, p); warning != "" {
		fmt.Fprintf(os.Stderr, "\n!!! %s\n\n", warning)
	}
	nativeArgs = applyPreferences(nativeArgs, p)
	if err = ensureHost(absData, defaultHost(*host), *claudeBin, *configDir); err != nil {
		return err
	}
	token, err := tokenFrom(absData, "")
	if err != nil {
		return fmt.Errorf("read access token: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	lockDir := filepath.Join(absData, "channel-locks", sessionID)
	if err = os.MkdirAll(lockDir, 0700); err != nil {
		return err
	}
	release, err := hostlock.Acquire(lockDir)
	if err != nil {
		return fmt.Errorf("this shared session already has a terminal launcher: %w", err)
	}
	defer release()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	configPath, err := writeConfig(absData, exe, defaultHost(*host), token, sessionID, cwd)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(configPath))
	settingsPath := filepath.Join(filepath.Dir(configPath), "settings.json")
	settings, _ := json.Marshal(map[string]any{"hooks": map[string]any{"SessionStart": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": exe, "args": []string{"channel-session-hook"}}}}}}})
	if err = os.WriteFile(settingsPath, settings, 0600); err != nil {
		return err
	}
	claudeArgs := append(nativeArgs, "--settings", settingsPath, "--mcp-config", configPath, "--dangerously-load-development-channels", "server:cc-remote")
	cmd := exec.Command(*claudeBin, claudeArgs...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	cmd.Env = replaceEnv(cmd.Env, "CC_REMOTE_SESSION_ID", sessionID, runtime.GOOS == "windows")
	cmd.Env = replaceEnv(cmd.Env, "CC_REMOTE_BINDING_FILE", filepath.Join(filepath.Dir(configPath), "session-changed"), runtime.GOOS == "windows")
	if strings.TrimSpace(*configDir) != "" {
		cmd.Env = replaceEnv(cmd.Env, "CLAUDE_CONFIG_DIR", strings.TrimSpace(*configDir), runtime.GOOS == "windows")
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeConfig(dataDir, hostExe, host, token, sessionID, cwd string) (string, error) {
	base := filepath.Join(dataDir, "channel-config")
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(base, "session-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, sessionID+".json")
	cfg := map[string]any{"mcpServers": map[string]any{"cc-remote": map[string]any{
		"type":    "stdio",
		"command": hostExe,
		"args":    []string{"channel"},
		"env": map[string]string{
			"CC_REMOTE_HOST":         host,
			"CC_REMOTE_TOKEN":        token,
			"CC_REMOTE_SESSION_ID":   sessionID,
			"CC_REMOTE_CWD":          cwd,
			"CC_REMOTE_BINDING_FILE": filepath.Join(dir, "session-changed"),
		},
	}}}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}

func replaceEnv(env []string, key, value string, caseInsensitive bool) []string {
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if name == key || (caseInsensitive && strings.EqualFold(name, key)) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, key+"="+value)
}

func prepareNativeArgs(args []string, resume string) ([]string, string, error) {
	result := append([]string{}, args...)
	id := strings.ToLower(strings.TrimSpace(resume))
	hasSession := false
	for i := 0; i < len(args); i++ {
		name, value, equals := strings.Cut(args[i], "=")
		switch name {
		case "-p", "--print", "--continue", "-c", "--fork-session", "--remote-control", "--settings", "--mcp-config", "--dangerously-load-development-channels", "--channels":
			return nil, "", fmt.Errorf("%s cannot be combined with shared terminal mode", name)
		case "--resume", "-r", "--session-id":
			if hasSession || id != "" {
				return nil, "", fmt.Errorf("specify only one Claude session UUID")
			}
			if !equals {
				i++
				if i >= len(args) {
					return nil, "", fmt.Errorf("%s requires a session UUID", name)
				}
				value = args[i]
			}
			id = strings.ToLower(strings.TrimSpace(value))
			if !launchUUID.MatchString(id) {
				return nil, "", fmt.Errorf("%s requires an explicit session UUID", name)
			}
			hasSession = true
		}
	}
	if id != "" && !launchUUID.MatchString(id) {
		return nil, "", fmt.Errorf("invalid Claude session UUID")
	}
	if !hasSession {
		if id == "" {
			id = store.ID()
			result = append(result, "--session-id", id)
		} else {
			result = append(result, "--resume", id)
		}
	}
	return result, id, nil
}

func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		name, _, _ := strings.Cut(a, "=")
		for _, n := range names {
			if name == n {
				return true
			}
		}
	}
	return false
}

var permissionFlags = []string{"--permission-mode", "--dangerously-skip-permissions", "--allow-dangerously-skip-permissions"}

// applyPreferences adds cc-remote defaults only for options the user did not pass.
func applyPreferences(args []string, p prefs.Preferences) []string {
	if hasFlag(args, permissionFlags...) {
		p.PermissionMode = ""
	}
	if hasFlag(args, "--model") {
		p.Model = ""
	}
	if hasFlag(args, "--effort") {
		p.Effort = ""
	}
	return append(append([]string{}, args...), claude.LaunchFlags(p.PermissionMode, p.Model, p.Effort)...)
}

// inheritedDangerWarning explains, before launch, that a dangerous permission
// mode comes from the web preferences rather than this command line.
func inheritedDangerWarning(args []string, p prefs.Preferences) string {
	if !prefs.Dangerous(p.PermissionMode) || hasFlag(args, permissionFlags...) {
		return ""
	}
	return fmt.Sprintf("警告：CC Remote 默认权限模式为 %s（来自网页设置），本终端会话将跳过工具审批；传 --permission-mode default 可覆盖。", p.PermissionMode)
}

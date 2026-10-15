// Package service installs the host as a per-user background service
// (Task Scheduler on Windows, systemd --user on Linux, launchd on macOS).
package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cc-remote/internal/config"
	"cc-remote/internal/listen"
)

// ExitError carries a process exit code without an error message.
type ExitError struct{ Code int }

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// ExitConflict is the host's exit code when its data directory lock or a
// listen port is held by another process (EX_TEMPFAIL). The Windows
// supervisor stops restarting after a few such exits.
const ExitConflict = 75

type schedState struct {
	Installed, Enabled bool
	Detail             string
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

const serviceUsage = `usage: cc-remote service <command> [flags]

commands:
  install    copy the executable, register and start the background service
  config     show or change listen/claude/env settings and restart the service
  uninstall  stop and unregister the service (--purge-binary removes the copy)
  start      start the service and wait until it answers
  stop       stop the service gracefully (falls back to the service manager)
  restart    stop, then start
  status     show service and host state (--json; exit 3 when not running)
  logs       print host.log (-n lines, -f follow)

Run "cc-remote service <command> -h" for flags.
`

// Main runs "cc-remote service ...".
func Main(args []string, version string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(serviceUsage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "install":
		return installCmd(rest, version)
	case "config":
		return configCmd(rest)
	case "uninstall":
		return uninstallCmd(rest)
	case "start", "stop", "restart":
		fs := flag.NewFlagSet("service "+cmd, flag.ContinueOnError)
		dataDir := fs.String("data-dir", "", "data directory of the installed service")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		cfg, err := mustConfig(*dataDir)
		if err != nil {
			return err
		}
		switch cmd {
		case "start":
			return start(cfg, os.Stdout)
		case "stop":
			return stop(cfg, os.Stdout)
		}
		if err = stop(cfg, os.Stdout); err != nil {
			return err
		}
		return start(cfg, os.Stdout)
	case "status":
		return statusCmd(rest)
	case "logs":
		return logsCmd(rest)
	case "supervise":
		return Supervise(rest)
	}
	return fmt.Errorf("unknown service command %q\n\n%s", cmd, serviceUsage)
}

func mustConfig(dataDir string) (*Config, error) {
	cfg, err := LoadConfig(dataDir)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, errors.New("service is not installed (no service.json); run: cc-remote service install")
	}
	return cfg, nil
}

func validateListen(specs []string) error {
	if len(specs) == 0 {
		return errors.New("at least one --listen address is required")
	}
	for _, s := range specs {
		if _, port, err := net.SplitHostPort(s); err != nil || port == "" {
			return fmt.Errorf("invalid --listen %q: expected host:port, lan:PORT, all:PORT or localhost:PORT", s)
		}
	}
	return nil
}

func resolveClaude(bin string, out io.Writer) string {
	if filepath.IsAbs(bin) {
		return bin
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		fmt.Fprintf(out, "warning: %q not found on PATH; the service will try it as is\n", bin)
		return bin
	}
	if abs, e := filepath.Abs(path); e == nil {
		return abs
	}
	return path
}

// applySettings updates cfg from explicitly set flags and fills missing defaults.
func applySettings(cfg *Config, set map[string]bool, listenVals []string, claudeBin, configDir string, allowInsecure bool, out io.Writer) error {
	if set["allow-insecure-remote"] {
		cfg.AllowInsecureRemote = allowInsecure
	}
	if set["listen"] || len(cfg.Listen) == 0 {
		vals := listen.Split(listenVals)
		if len(vals) == 0 {
			vals = []string{"127.0.0.1:8787"}
		}
		if err := validateListen(vals); err != nil {
			return err
		}
		cfg.Listen = vals
	}
	if bad := listen.Insecure(cfg.Listen); len(bad) > 0 && !cfg.AllowInsecureRemote {
		return fmt.Errorf("%s binds every interface (plain HTTP, reachable beyond the LAN); use lan:PORT or a specific address, or pass --allow-insecure-remote if you really mean it", strings.Join(bad, ", "))
	}
	if set["claude"] || cfg.Claude == "" {
		cfg.Claude = resolveClaude(claudeBin, out)
	}
	if set["claude-config-dir"] {
		cfg.ClaudeConfigDir = ""
		if configDir != "" {
			abs, err := filepath.Abs(configDir)
			if err != nil {
				return err
			}
			cfg.ClaudeConfigDir = abs
		}
	}
	return nil
}

type envFlags struct {
	vals               multiFlag
	capture, noCapture *bool
}

func addEnvFlags(fs *flag.FlagSet, captureHelp string) *envFlags {
	e := &envFlags{}
	fs.Var(&e.vals, "env", "service environment KEY=VALUE, repeatable; KEY= removes KEY (values are stored in service.json)")
	e.capture = fs.Bool("capture-env", false, captureHelp)
	e.noCapture = fs.Bool("no-capture-env", false, "do not capture proxy variables from the current environment")
	return e
}

// shouldCapture resolves --capture-env/--no-capture-env against a default.
func (e *envFlags) shouldCapture(set map[string]bool, def bool) (bool, error) {
	if set["capture-env"] && set["no-capture-env"] && *e.capture && *e.noCapture {
		return false, errors.New("--capture-env and --no-capture-env are mutually exclusive")
	}
	if set["no-capture-env"] && *e.noCapture {
		return false, nil
	}
	if set["capture-env"] {
		return *e.capture, nil
	}
	return def, nil
}

// redacted returns a copy of cfg for printing, with non-proxy environment
// values masked.
func redacted(cfg *Config) *Config {
	if cfg == nil || cfg.Env == nil {
		return cfg
	}
	c := *cfg
	c.Env = make(map[string]string, len(cfg.Env))
	for k, v := range cfg.Env {
		_, c.Env[k], _ = strings.Cut(envDisplay(k, v), "=")
	}
	return &c
}

func redactedJSON(cfg *Config) []byte {
	b, _ := json.MarshalIndent(redacted(cfg), "", "  ")
	return b
}

func installCmd(args []string, version string) error {
	fs := flag.NewFlagSet("service install", flag.ContinueOnError)
	var listenVals multiFlag
	fs.Var(&listenVals, "listen", "listen address, repeatable or comma separated (default 127.0.0.1:8787; also lan:PORT, all:PORT, localhost:PORT)")
	dataDir := fs.String("data-dir", "", "host data directory (default: the existing installation's, else "+config.DefaultDataDir()+")")
	claudeBin := fs.String("claude", "claude", "Claude Code executable (resolved to an absolute path)")
	configDir := fs.String("claude-config-dir", "", "Claude config directory")
	allowInsecure := fs.Bool("allow-insecure-remote", false, "allow all:/0.0.0.0/:: listen addresses (plain HTTP on every interface)")
	noStart := fs.Bool("no-start", false, "register without starting")
	withAlias := fs.Bool("alias", false, "also install the ccr alias")
	dryRun := fs.Bool("dry-run", false, "print what would be done without changing anything")
	envF := addEnvFlags(fs, "capture HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY from the current environment (default when the installation has no environment configured yet)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	out := os.Stdout
	existing, absData, err := existingInstall(*dataDir)
	if err != nil {
		return err
	}
	cfg := &Config{DataDir: absData}
	if existing != nil {
		*cfg = *existing
		cfg.DataDir = absData
		fmt.Fprintf(out, "Existing installation found (data dir %s); updating it.\n", absData)
		// Installs from before --allow-insecure-remote existed may already
		// bind every interface; keep them working through "cc-remote update"
		// (the old binary cannot set the flag) but record the opt-in.
		if bad := listen.Insecure(cfg.Listen); len(bad) > 0 && !cfg.AllowInsecureRemote && !set["listen"] && !set["allow-insecure-remote"] {
			cfg.AllowInsecureRemote = true
			fmt.Fprintf(out, "warning: the existing installation listens on %s (every interface); keeping it and recording --allow-insecure-remote. Prefer: cc-remote service config --listen lan:PORT --allow-insecure-remote=false\n", strings.Join(bad, ", "))
		}
	}
	if err = applySettings(cfg, set, listenVals, *claudeBin, *configDir, *allowInsecure, out); err != nil {
		return err
	}
	capture, err := envF.shouldCapture(set, cfg.Env == nil)
	if err != nil {
		return err
	}
	if err = applyEnv(cfg, capture, os.Environ(), envF.vals, foldEnvCase(), out); err != nil {
		return err
	}
	if cfg.Exe, err = InstalledExe(); err != nil {
		return err
	}
	cfg.LogFile = filepath.Join(absData, "host.log")
	cfg.InstalledAt = time.Now().UTC().Truncate(time.Second)
	cfg.Version = version
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Fprintln(out, "[dry-run] no changes will be made")
		fmt.Fprintf(out, "[dry-run] stop any host answering at %s\n", strings.Join(listen.ProbeURLs(cfg.Listen), ", "))
	} else if existing != nil || hostAnswering(cfg) {
		if err = stop(cfg, out); err != nil {
			return err
		}
	}
	if err = installBinaries(self, *dryRun, out); err != nil {
		return fmt.Errorf("install executable: %w", err)
	}
	if *dryRun {
		fmt.Fprintf(out, "[dry-run] write %s and %s:\n%s\n", filepath.Join(absData, configName), filepath.Join(filepath.Dir(filepath.Dir(cfg.Exe)), configName), redactedJSON(cfg))
	} else {
		if err = SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "Wrote %s\n", filepath.Join(absData, configName))
	}
	if err = register(cfg, *dryRun, out); err != nil {
		return err
	}
	printExposure(cfg, out)
	if *withAlias {
		if err = installAlias(aliasOptions{dryRun: *dryRun}, out); err != nil {
			return err
		}
	}
	if *noStart {
		fmt.Fprintln(out, "Service registered; start it with: cc-remote service start")
		return nil
	}
	if *dryRun {
		fmt.Fprintln(out, "[dry-run] start the service and wait for /api/info")
		return nil
	}
	return start(cfg, out)
}

// existingInstall locates the installation to update. An explicit --data-dir
// wins; otherwise the mirrored service.json in InstallRoot (then the default
// data dir) names the data dir, so "cc-remote update" and the install scripts
// keep a non-default data dir, listen addresses and Claude settings.
func existingInstall(dataDir string) (*Config, string, error) {
	if dataDir != "" {
		abs, err := filepath.Abs(dataDir)
		if err != nil {
			return nil, "", err
		}
		cfg, err := LoadConfig(abs)
		if err != nil {
			return nil, "", fmt.Errorf("read existing service.json: %w", err)
		}
		return cfg, abs, nil
	}
	cfg, err := LoadConfig("")
	if err != nil {
		return nil, "", fmt.Errorf("read existing service.json: %w", err)
	}
	dir := config.DefaultDataDir()
	if cfg != nil && cfg.DataDir != "" {
		dir = cfg.DataDir
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", err
	}
	return cfg, abs, nil
}

func hostAnswering(cfg *Config) bool {
	_, answering := findHost(cfg)
	return answering
}

func configCmd(args []string) error {
	fs := flag.NewFlagSet("service config", flag.ContinueOnError)
	var listenVals multiFlag
	fs.Var(&listenVals, "listen", "listen address, repeatable or comma separated")
	dataDir := fs.String("data-dir", "", "data directory of the installed service")
	claudeBin := fs.String("claude", "claude", "Claude Code executable")
	configDir := fs.String("claude-config-dir", "", "Claude config directory (empty to clear)")
	allowInsecure := fs.Bool("allow-insecure-remote", false, "allow all:/0.0.0.0/:: listen addresses (=false to revoke)")
	noRestart := fs.Bool("no-restart", false, "save without restarting the service")
	dryRun := fs.Bool("dry-run", false, "print what would be done without changing anything")
	envF := addEnvFlags(fs, "capture HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY from the current environment")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	cfg, err := mustConfig(*dataDir)
	if err != nil {
		return err
	}
	out := os.Stdout
	envChange := set["env"] || set["capture-env"] || set["no-capture-env"]
	if !set["listen"] && !set["claude"] && !set["claude-config-dir"] && !set["allow-insecure-remote"] && !envChange {
		printConfig(cfg, out)
		return nil
	}
	if err = applySettings(cfg, set, listenVals, *claudeBin, *configDir, *allowInsecure, out); err != nil {
		return err
	}
	if envChange {
		capture, err := envF.shouldCapture(set, false)
		if err != nil {
			return err
		}
		if err = applyEnv(cfg, capture, os.Environ(), envF.vals, foldEnvCase(), out); err != nil {
			return err
		}
	}
	if *dryRun {
		fmt.Fprintf(out, "[dry-run] no changes will be made\n[dry-run] write service.json:\n%s\n", redactedJSON(cfg))
	} else if err = SaveConfig(cfg); err != nil {
		return err
	}
	if err = register(cfg, *dryRun, out); err != nil {
		return err
	}
	printExposure(cfg, out)
	if *dryRun {
		fmt.Fprintln(out, "[dry-run] restart the service")
		return nil
	}
	if *noRestart {
		fmt.Fprintln(out, "Saved; restart with: cc-remote service restart")
		return nil
	}
	if err = stop(cfg, out); err != nil {
		return err
	}
	return start(cfg, out)
}

func printConfig(cfg *Config, out io.Writer) {
	fmt.Fprintf(out, "Listen:        %s\n", strings.Join(cfg.Listen, ", "))
	fmt.Fprintf(out, "Data dir:      %s\n", cfg.DataDir)
	fmt.Fprintf(out, "Claude:        %s\n", cfg.Claude)
	if cfg.ClaudeConfigDir != "" {
		fmt.Fprintf(out, "Claude config: %s\n", cfg.ClaudeConfigDir)
	}
	if cfg.AllowInsecureRemote {
		fmt.Fprintln(out, "Insecure bind: allowed (--allow-insecure-remote)")
	}
	if len(cfg.Env) > 0 {
		fmt.Fprintf(out, "Env:           %s\n", envSummary(cfg.Env))
	}
	fmt.Fprintf(out, "Executable:    %s\n", cfg.Exe)
	fmt.Fprintf(out, "Log file:      %s\n", cfg.LogFile)
	fmt.Fprintf(out, "Installed:     %s (version %s)\n", cfg.InstalledAt.Local().Format(time.DateTime), cfg.Version)
}

// printExposure warns about non-loopback listen addresses.
func printExposure(cfg *Config, out io.Writer) {
	var exposed []string
	for _, l := range cfg.Listen {
		if !listen.IsLoopback(l) {
			exposed = append(exposed, l)
		}
	}
	if len(exposed) == 0 {
		return
	}
	fmt.Fprintf(out, "WARNING: %s is reachable from other machines over plain HTTP. Anyone holding the access token can run code as you; use it only on a trusted LAN or behind HTTPS / a tunnel.\n", strings.Join(exposed, ", "))
	fmt.Fprintln(out, "警告：以上地址可被其它机器以明文 HTTP 访问；持有访问令牌即可以你的身份执行代码，仅在可信局域网使用，或置于 HTTPS 反代 / 隧道之后。")
	firewallHint(exposed, out)
}

func start(cfg *Config, out io.Writer) error {
	if u, _ := findHost(cfg); u != "" {
		fmt.Fprintf(out, "Host already running at %s\n", u)
		return nil
	}
	if err := schedulerStart(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	u, ok := waitReady(cfg, 10*time.Second)
	if !ok {
		return fmt.Errorf("service started but the host did not answer within 10s; see: cc-remote service logs")
	}
	fmt.Fprintf(out, "Host running at %s\n", u)
	return nil
}

func stop(cfg *Config, out io.Writer) error {
	u, answering := findHost(cfg)
	if u != "" {
		if err := gracefulShutdown(u, readToken(cfg.DataDir)); err != nil {
			fmt.Fprintf(out, "Graceful shutdown failed (%v); using the service manager\n", err)
		} else if waitStopped(cfg, 15*time.Second) {
			_ = schedulerStop()
			fmt.Fprintln(out, "Host stopped")
			return nil
		} else {
			fmt.Fprintln(out, "Host did not exit within 15s; using the service manager")
		}
	} else if answering {
		fmt.Fprintln(out, "warning: a service answers the listen address but rejects this data directory's token")
	}
	if err := schedulerStop(); err != nil && schedulerState().Installed {
		return fmt.Errorf("stop service: %w", err)
	}
	if !waitStopped(cfg, 5*time.Second) {
		return errors.New("host is still answering; it may not be managed by the service (stop it manually)")
	}
	fmt.Fprintln(out, "Service stopped")
	return nil
}

func uninstallCmd(args []string) error {
	fs := flag.NewFlagSet("service uninstall", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "data directory of the installed service")
	purge := fs.Bool("purge-binary", false, "also remove the installed executable copy")
	dryRun := fs.Bool("dry-run", false, "print what would be done without changing anything")
	if err := fs.Parse(args); err != nil {
		return err
	}
	out := os.Stdout
	cfg, err := LoadConfig(*dataDir)
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Fprintln(out, "[dry-run] no changes will be made")
		if cfg != nil {
			fmt.Fprintf(out, "[dry-run] stop the host at %s\n", strings.Join(listen.ProbeURLs(cfg.Listen), ", "))
		}
	} else if cfg != nil {
		if err = stop(cfg, out); err != nil {
			fmt.Fprintf(out, "warning: %v\n", err)
		}
	}
	if err = unregister(*dryRun, out); err != nil {
		return err
	}
	root, err := InstallRoot()
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Fprintf(out, "[dry-run] remove %s (data directory is kept)\n", filepath.Join(root, configName))
	} else {
		removeConfigMirror()
	}
	if *purge {
		bin := filepath.Join(root, "bin")
		if *dryRun {
			fmt.Fprintf(out, "[dry-run] remove %s\n", bin)
		} else {
			if err = os.RemoveAll(bin); err != nil {
				return err
			}
			fmt.Fprintf(out, "Removed %s\n", bin)
		}
		// On Windows the ccr launcher and its user PATH entry live in bin.
		if err = removeFromPath(bin, *dryRun, out); err != nil {
			fmt.Fprintf(out, "warning: remove %s from PATH: %v\n", bin, err)
		}
	}
	if !*dryRun {
		fmt.Fprintln(out, "Service uninstalled; data directory kept.")
	}
	return nil
}

func statusCmd(args []string) error {
	fs := flag.NewFlagSet("service status", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "data directory of the installed service")
	asJSON := fs.Bool("json", false, "print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := LoadConfig(*dataDir)
	if err != nil {
		return err
	}
	state := schedulerState()
	var url, endpoint string
	var status map[string]any
	var statusErr error
	if cfg != nil {
		url, _ = findHost(cfg)
		if url != "" {
			status, endpoint, statusErr = fetchStatus(url, readToken(cfg.DataDir))
		}
	}
	running := url != ""
	if *asJSON {
		v := map[string]any{"installed": state.Installed, "enabled": state.Enabled, "scheduler": state.Detail, "manager": managerName, "config": redacted(cfg), "running": running, "url": url, "status": status}
		if statusErr != nil {
			v["statusError"] = statusErr.Error()
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err = enc.Encode(v); err != nil {
			return err
		}
	} else {
		printStatus(os.Stdout, cfg, state, url, endpoint, status, statusErr)
	}
	if !running {
		return ExitError{Code: 3}
	}
	return nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func printStatus(out io.Writer, cfg *Config, state schedState, url, endpoint string, status map[string]any, statusErr error) {
	fmt.Fprintf(out, "Service (%s): installed=%s enabled=%s\n", managerName, yesNo(state.Installed), yesNo(state.Enabled))
	if state.Detail != "" {
		fmt.Fprintf(out, "Scheduler:     %s\n", state.Detail)
	}
	if cfg == nil {
		fmt.Fprintln(out, "service.json:  not found")
		fmt.Fprintln(out, "Host:          not running")
		return
	}
	printConfig(cfg, out)
	for _, l := range cfg.Listen {
		if !listen.IsLoopback(l) {
			fmt.Fprintf(out, "  %s: reachable from other machines over plain HTTP / 其它机器可明文访问，令牌即执行权限\n", l)
		}
	}
	if url == "" {
		fmt.Fprintln(out, "Host:          not running")
		return
	}
	fmt.Fprintf(out, "Host:          running at %s\n", url)
	if statusErr != nil {
		fmt.Fprintf(out, "Status error:  %v\n", statusErr)
		return
	}
	if endpoint == "/api/info" {
		fmt.Fprintf(out, "Claude:        %v (host has no /api/status)\n", status["claudeVersion"])
		return
	}
	field := func(label, key string) {
		if v, ok := status[key]; ok && v != nil && v != "" {
			fmt.Fprintf(out, "%-15s%v\n", label+":", v)
		}
	}
	field("Version", "version")
	if v, ok := status["pid"].(float64); ok {
		fmt.Fprintf(out, "%-15s%d\n", "PID:", int64(v))
	}
	if v, ok := status["uptimeSeconds"].(float64); ok {
		fmt.Fprintf(out, "%-15s%s\n", "Uptime:", (time.Duration(v) * time.Second).String())
	}
	field("Bound", "listen")
	if c, ok := status["claude"].(map[string]any); ok {
		if c["error"] != nil && c["error"] != "" {
			fmt.Fprintf(out, "%-15s%v (error: %v)\n", "Claude:", c["executable"], c["error"])
		} else {
			fmt.Fprintf(out, "%-15s%v %v\n", "Claude:", c["executable"], c["version"])
		}
	}
	if s, ok := status["service"].(map[string]any); ok {
		fmt.Fprintf(out, "%-15smanaged=%v manager=%v\n", "Managed:", s["managed"], s["manager"])
	}
	printMap(out, "Sessions", status["sessions"])
	if v, ok := status["pendingRequests"].(float64); ok {
		fmt.Fprintf(out, "%-15s%d\n", "Pending:", int64(v))
	}
	printMap(out, "Totals", status["totals"])
}

func printMap(out io.Writer, label string, v any) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		val := m[k]
		if val == nil || val == "" {
			continue
		}
		if f, ok := val.(float64); ok && f == float64(int64(f)) {
			val = int64(f)
		}
		parts = append(parts, fmt.Sprintf("%s=%v", k, val))
	}
	fmt.Fprintf(out, "%-15s%s\n", label+":", strings.Join(parts, " "))
}

func logsCmd(args []string) error {
	fs := flag.NewFlagSet("service logs", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "data directory (defaults to the installed service's)")
	lines := fs.Int("n", 100, "number of lines to print")
	follow := fs.Bool("f", false, "follow the log")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := LoadConfig(*dataDir)
	if err != nil {
		return err
	}
	var path string
	switch {
	case cfg != nil && cfg.LogFile != "":
		path = cfg.LogFile
	case *dataDir != "":
		path = filepath.Join(*dataDir, "host.log")
	default:
		path = filepath.Join(config.DefaultDataDir(), "host.log")
	}
	return tailLog(os.Stdout, path, *lines, *follow, nil)
}

// lastLines returns the final n lines of b, newline terminated.
func lastLines(b []byte, n int) []byte {
	if n <= 0 || len(b) == 0 {
		return nil
	}
	b = bytes.TrimRight(b, "\n")
	idx := len(b)
	for i := 0; i < n; i++ {
		j := bytes.LastIndexByte(b[:idx], '\n')
		if j < 0 {
			return append(b, '\n')
		}
		idx = j
	}
	return append(b[idx+1:], '\n')
}

// tailLog prints the last n lines and optionally follows the file, starting
// over after rotation. Closing stop ends following (used by tests).
func tailLog(out io.Writer, path string, n int, follow bool, stop <-chan struct{}) error {
	b, err := os.ReadFile(path)
	if err != nil && !(follow && errors.Is(err, os.ErrNotExist)) {
		return err
	}
	out.Write(lastLines(b, n))
	if !follow {
		return nil
	}
	offset := int64(len(b))
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-ticker.C:
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.Size() < offset {
			offset = 0 // rotated or truncated
		}
		if info.Size() == offset {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		if _, err = f.Seek(offset, io.SeekStart); err == nil {
			n, _ := io.Copy(out, bufio.NewReader(f))
			offset += n
		}
		f.Close()
	}
}

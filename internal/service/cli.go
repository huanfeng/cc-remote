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
	"cc-remote/internal/i18n"
	"cc-remote/internal/identity"
	"cc-remote/internal/listen"
)

// ExitError carries a process exit code without an error message.
type ExitError struct{ Code int }

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// ExitConflict is the host's exit code when its data directory lock or a
// listen port is held by another process (EX_TEMPFAIL). The Windows
// supervisor stops restarting after a few such exits.
const ExitConflict = 75

// ExitRestart is the host's exit code when it stops so that the service
// manager starts it again, e.g. to run an update installed with --when-idle.
const ExitRestart = 76

type schedState struct {
	Installed, Enabled bool
	Detail             string
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

const serviceUsageZH = `用法：cc-remote service <命令> [参数]

命令：
  install    复制可执行文件，注册并启动后台服务
  config     查看或修改 listen/claude/env 等设置，并重启服务
  uninstall  停止并注销服务（--purge-binary 同时删除复制的可执行文件）
  start      启动服务并等待它响应
  stop       正常停止服务（失败时交给服务管理器）
  restart    先停止，再启动
  status     显示服务和宿主状态（--json；未运行时退出码为 3）
  logs       打印 host.log（-n 行数，-f 持续跟随）

运行 "cc-remote service <命令> -h" 查看参数。
`

func serviceUsage() string { return i18n.T(serviceUsageEN, serviceUsageZH) }

const serviceUsageEN = `usage: cc-remote service <command> [flags]

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
		fmt.Print(serviceUsage())
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
		dataDir := fs.String("data-dir", "", dataDirHelp())
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
	return i18n.Errorf("unknown service command %q\n\n%s", "未知的 service 命令 %q\n\n%s", cmd, serviceUsage())
}

// dataDirHelp is the --data-dir help of commands acting on an installation.
func dataDirHelp() string {
	return i18n.T("data directory of the installed service", "已安装服务的数据目录")
}

// dryRunHelp is the --dry-run help.
func dryRunHelp() string {
	return i18n.T("print what would be done without changing anything", "只打印将要执行的操作，不做任何改动")
}

func dryRunNoChanges() string {
	return i18n.T("[dry-run] no changes will be made", "[dry-run] 不会做任何改动")
}

// dryRunRemove, wrote and removed are the lines of a file written or removed.
func dryRunRemove(path string) string {
	return i18n.F("[dry-run] remove %s\n", "[dry-run] 删除 %s\n", path)
}

func wrote(path string) string { return i18n.F("Wrote %s\n", "已写入 %s\n", path) }

func removed(path string) string { return i18n.F("Removed %s\n", "已删除 %s\n", path) }

func mustConfig(dataDir string) (*Config, error) {
	cfg, err := LoadConfig(dataDir)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, errors.New(i18n.T("service is not installed (no service.json); run: cc-remote service install", "服务未安装（没有 service.json）；请运行：cc-remote service install"))
	}
	return cfg, nil
}

func validateListen(specs []string) error {
	if len(specs) == 0 {
		return errors.New(i18n.T("at least one --listen address is required", "至少需要一个 --listen 地址"))
	}
	for _, s := range specs {
		if _, port, err := net.SplitHostPort(s); err != nil || port == "" {
			return i18n.Errorf("invalid --listen %q: expected host:port, lan:PORT, all:PORT or localhost:PORT", "无效的 --listen %q：应为 host:port、lan:PORT、all:PORT 或 localhost:PORT", s)
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
		fmt.Fprint(out, i18n.F("warning: %q not found on PATH; the service will try it as is\n", "警告：PATH 中找不到 %q；服务将按原样尝试运行它\n", bin))
		return bin
	}
	if abs, e := filepath.Abs(path); e == nil {
		return abs
	}
	return path
}

// identityFlags are --name and --color: how the browser tells this host apart.
type identityFlags struct{ name, color *string }

func addIdentityFlags(fs *flag.FlagSet) identityFlags {
	return identityFlags{
		name:  fs.String("name", "", i18n.T("host name shown in the browser tab, sidebar and login page (empty: the machine name)", "显示在浏览器标签页、侧栏和登录页的主机名称（留空：机器名）")),
		color: fs.String("color", "", i18n.F("host color of the tab icon and logo: %s, #rrggbb, or auto", "标签页图标和标志的主机颜色：%s、#rrggbb 或 auto", identity.Keys())),
	}
}

// apply saves --name and --color in the data directory; it reports whether either was given.
func (f identityFlags) apply(dataDir string, set map[string]bool, dryRun bool, out io.Writer) (bool, error) {
	if !set["name"] && !set["color"] {
		return false, nil
	}
	id, err := identity.Load(dataDir)
	if err != nil {
		return true, err
	}
	if set["name"] {
		id.Name = *f.name
	}
	if set["color"] {
		id.Color = *f.color
		if strings.EqualFold(strings.TrimSpace(id.Color), "auto") {
			id.Color = ""
		}
	}
	if err = id.Validate(); err != nil {
		return true, err
	}
	if dryRun {
		fmt.Fprint(out, i18n.F("[dry-run] write identity.json: name %q, color %q\n", "[dry-run] 写入 identity.json：名称 %q，颜色 %q\n", id.Name, id.Color))
		return true, nil
	}
	if err = identity.Save(dataDir, id); err != nil {
		return true, err
	}
	name, hex := id.Resolved()
	fmt.Fprint(out, i18n.F("Host name %s, color %s; open pages pick it up when reloaded (no restart needed).\n", "主机名称 %s，颜色 %s；已打开的页面刷新后生效（无需重启）。\n", name, hex))
	return true, nil
}

// identityValue shows a setting, or its default marked as automatic.
func identityValue(setting, shown string) string {
	if setting != "" {
		return shown
	}
	return shown + i18n.T(" (auto)", "（自动）")
}

// applyLanguage stores --lang: zh or en, or empty for auto (the system language).
func applyLanguage(cfg *Config, set map[string]bool, value string) error {
	if !set["lang"] {
		return nil
	}
	l, ok := i18n.Parse(value)
	if !ok {
		return i18n.Errorf("--lang must be auto, zh or en (got %q)", "--lang 只能是 auto、zh 或 en（收到 %q）", value)
	}
	cfg.Language = ""
	if l != i18n.Auto {
		cfg.Language = string(l)
	}
	i18n.Set(l)
	return nil
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
		return i18n.Errorf("%s binds every interface (plain HTTP, reachable beyond the LAN); use lan:PORT or a specific address, or pass --allow-insecure-remote if you really mean it", "%s 会监听所有网卡（明文 HTTP，局域网之外也可能访问到）；请改用 lan:PORT 或具体地址，确有需要时加 --allow-insecure-remote", strings.Join(bad, ", "))
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
	fs.Var(&e.vals, "env", i18n.T("service environment KEY=VALUE, repeatable; KEY= removes KEY (values are stored in service.json)", "服务环境变量 KEY=VALUE，可重复；KEY= 表示删除 KEY（值保存在 service.json 中）"))
	e.capture = fs.Bool("capture-env", false, captureHelp)
	e.noCapture = fs.Bool("no-capture-env", false, i18n.T("do not capture proxy variables from the current environment", "不从当前环境捕获代理变量"))
	return e
}

// shouldCapture resolves --capture-env/--no-capture-env against a default.
func (e *envFlags) shouldCapture(set map[string]bool, def bool) (bool, error) {
	if set["capture-env"] && set["no-capture-env"] && *e.capture && *e.noCapture {
		return false, errors.New(i18n.T("--capture-env and --no-capture-env are mutually exclusive", "--capture-env 与 --no-capture-env 不能同时使用"))
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
	fs.Var(&listenVals, "listen", i18n.T("listen address, repeatable or comma separated (default 127.0.0.1:8787; also lan:PORT, all:PORT, localhost:PORT)", "监听地址，可重复或用逗号分隔（默认 127.0.0.1:8787；也可用 lan:PORT、all:PORT、localhost:PORT）"))
	dataDir := fs.String("data-dir", "", i18n.F("host data directory (default: the existing installation's, else %s)", "宿主数据目录（默认沿用现有安装的，否则为 %s）", config.DefaultDataDir()))
	claudeBin := fs.String("claude", "claude", i18n.T("Claude Code executable (resolved to an absolute path)", "Claude Code 可执行文件（解析为绝对路径）"))
	configDir := fs.String("claude-config-dir", "", i18n.T("Claude config directory", "Claude 配置目录"))
	allowInsecure := fs.Bool("allow-insecure-remote", false, i18n.T("allow all:/0.0.0.0/:: listen addresses (plain HTTP on every interface)", "允许 all:/0.0.0.0/:: 监听地址（在所有网卡上提供明文 HTTP）"))
	lang := fs.String("lang", "", i18n.T("language of messages and the web UI: auto (system language, default), zh or en", "提示信息和 Web 界面的语言：auto（跟随系统，默认）、zh 或 en"))
	idF := addIdentityFlags(fs)
	noStart := fs.Bool("no-start", false, i18n.T("register without starting", "只注册，不启动"))
	whenIdle := fs.Bool("when-idle", false, i18n.T("replace the executable without stopping a running host; it restarts itself once no session is busy", "替换可执行文件但不停止运行中的宿主；等没有忙碌的会话时它会自行重启"))
	withAlias := fs.Bool("alias", false, i18n.T("also install the ccr alias", "同时安装 ccr 别名"))
	dryRun := fs.Bool("dry-run", false, dryRunHelp())
	envF := addEnvFlags(fs, i18n.T("capture HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY from the current environment (default when the installation has no environment configured yet)", "从当前环境捕获 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY（安装尚未配置环境变量时默认捕获）"))
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
		fmt.Fprint(out, i18n.F("Existing installation found (data dir %s); updating it.\n", "发现已有安装（数据目录 %s），将更新它。\n", absData))
		// Installs from before --allow-insecure-remote existed may already
		// bind every interface; keep them working through "cc-remote update"
		// (the old binary cannot set the flag) but record the opt-in.
		if bad := listen.Insecure(cfg.Listen); len(bad) > 0 && !cfg.AllowInsecureRemote && !set["listen"] && !set["allow-insecure-remote"] {
			cfg.AllowInsecureRemote = true
			fmt.Fprint(out, i18n.F("warning: the existing installation listens on %s (every interface); keeping it and recording --allow-insecure-remote. Prefer: cc-remote service config --listen lan:PORT --allow-insecure-remote=false\n", "警告：现有安装监听 %s（所有网卡）；保持不变并记录 --allow-insecure-remote。建议改为：cc-remote service config --listen lan:PORT --allow-insecure-remote=false\n", strings.Join(bad, ", ")))
		}
	}
	if err = applySettings(cfg, set, listenVals, *claudeBin, *configDir, *allowInsecure, out); err != nil {
		return err
	}
	if err = applyLanguage(cfg, set, *lang); err != nil {
		return err
	}
	if _, err = idF.apply(absData, set, *dryRun, out); err != nil {
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
	// --when-idle leaves a running service alone: the executable is swapped
	// (a running one is moved aside) and the host restarts itself when idle.
	hostURL, _ := findHost(cfg)
	deferred := *whenIdle && existing != nil && hostURL != "" && !*dryRun
	if *dryRun {
		fmt.Fprintln(out, dryRunNoChanges())
		fmt.Fprint(out, i18n.F("[dry-run] stop any host answering at %s\n", "[dry-run] 停止在 %s 响应的宿主\n", strings.Join(listen.ProbeURLs(cfg.Listen), ", ")))
	} else if deferred {
		fmt.Fprint(out, i18n.F("Host running at %s; installing without stopping it (--when-idle)\n", "宿主正在 %s 运行；安装时不停止它（--when-idle）\n", hostURL))
	} else if existing != nil || hostAnswering(cfg) {
		if err = stop(cfg, out); err != nil {
			return err
		}
	}
	if err = installBinaries(self, *dryRun, out); err != nil {
		return i18n.Errorf("install executable: %w", "安装可执行文件失败：%w", err)
	}
	if *dryRun {
		fmt.Fprint(out, i18n.F("[dry-run] write %s and %s:\n%s\n", "[dry-run] 写入 %s 和 %s：\n%s\n", filepath.Join(absData, configName), filepath.Join(filepath.Dir(filepath.Dir(cfg.Exe)), configName), redactedJSON(cfg)))
	} else {
		if err = SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprint(out, wrote(filepath.Join(absData, configName)))
	}
	if deferred {
		// Re-registering could restart the service; its definition reads
		// the settings from service.json, written above.
		if *withAlias || aliasInstalled() {
			if err = installAlias(aliasOptions{}, out); err != nil {
				return err
			}
		}
		busy, supported, err := requestRestart(hostURL, readToken(cfg.DataDir))
		switch {
		case err != nil:
			return i18n.Errorf("new version installed, but asking the host to restart failed: %w (run: cc-remote service restart)", "新版本已安装，但请求宿主重启失败：%w（请运行：cc-remote service restart）", err)
		case !supported:
			fmt.Fprintln(out, i18n.T("New version installed. The running host is too old to restart itself; run cc-remote service restart when it is idle.", "新版本已安装。运行中的宿主版本较旧，不能自动重启；空闲时运行 cc-remote service restart 生效。"))
		case len(busy) == 0:
			fmt.Fprintln(out, i18n.T("New version installed; no session is busy, the host is restarting.", "新版本已安装，没有忙碌的会话，宿主正在重启。"))
		default:
			fmt.Fprint(out, i18n.F("New version installed. The host restarts by itself once these sessions are idle: %s\n", "新版本已安装。宿主会在这些会话空闲后自动重启：%s\n", strings.Join(busy, i18n.T(", ", "、"))))
		}
		return nil
	}
	if err = register(cfg, *dryRun, out); err != nil {
		return err
	}
	printExposure(cfg, out)
	// Reinstalls (and "cc-remote update") refresh an existing ccr alias too, so
	// its target and the Unix cc-remote link follow the new installation.
	if *withAlias || aliasInstalled() {
		if err = installAlias(aliasOptions{dryRun: *dryRun}, out); err != nil {
			return err
		}
	}
	if *noStart {
		fmt.Fprintln(out, i18n.T("Service registered; start it with: cc-remote service start", "服务已注册；用此命令启动：cc-remote service start"))
		return nil
	}
	if *dryRun {
		fmt.Fprintln(out, i18n.T("[dry-run] start the service and wait for /api/info", "[dry-run] 启动服务并等待 /api/info 响应"))
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
			return nil, "", i18n.Errorf("read existing service.json: %w", "读取现有 service.json 失败：%w", err)
		}
		return cfg, abs, nil
	}
	cfg, err := LoadConfig("")
	if err != nil {
		return nil, "", i18n.Errorf("read existing service.json: %w", "读取现有 service.json 失败：%w", err)
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
	fs.Var(&listenVals, "listen", i18n.T("listen address, repeatable or comma separated", "监听地址，可重复或用逗号分隔"))
	dataDir := fs.String("data-dir", "", dataDirHelp())
	claudeBin := fs.String("claude", "claude", i18n.T("Claude Code executable", "Claude Code 可执行文件"))
	configDir := fs.String("claude-config-dir", "", i18n.T("Claude config directory (empty to clear)", "Claude 配置目录（留空表示清除）"))
	allowInsecure := fs.Bool("allow-insecure-remote", false, i18n.T("allow all:/0.0.0.0/:: listen addresses (=false to revoke)", "允许 all:/0.0.0.0/:: 监听地址（=false 撤销）"))
	lang := fs.String("lang", "", i18n.T("language of messages and the web UI: auto (system language), zh or en", "提示信息和 Web 界面的语言：auto（跟随系统）、zh 或 en"))
	idF := addIdentityFlags(fs)
	noRestart := fs.Bool("no-restart", false, i18n.T("save without restarting the service", "只保存，不重启服务"))
	dryRun := fs.Bool("dry-run", false, dryRunHelp())
	envF := addEnvFlags(fs, i18n.T("capture HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY from the current environment", "从当前环境捕获 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY"))
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
	// The host reads its name and colour on every page load: no restart.
	changed, err := idF.apply(cfg.DataDir, set, *dryRun, out)
	if err != nil {
		return err
	}
	if !set["listen"] && !set["claude"] && !set["claude-config-dir"] && !set["allow-insecure-remote"] && !set["lang"] && !envChange {
		if !changed {
			printConfig(cfg, out)
		}
		return nil
	}
	if err = applySettings(cfg, set, listenVals, *claudeBin, *configDir, *allowInsecure, out); err != nil {
		return err
	}
	if err = applyLanguage(cfg, set, *lang); err != nil {
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
		fmt.Fprintln(out, dryRunNoChanges())
		fmt.Fprint(out, i18n.F("[dry-run] write service.json:\n%s\n", "[dry-run] 写入 service.json：\n%s\n", redactedJSON(cfg)))
	} else if err = SaveConfig(cfg); err != nil {
		return err
	}
	if err = register(cfg, *dryRun, out); err != nil {
		return err
	}
	printExposure(cfg, out)
	if *dryRun {
		fmt.Fprintln(out, i18n.T("[dry-run] restart the service", "[dry-run] 重启服务"))
		return nil
	}
	if *noRestart {
		fmt.Fprintln(out, i18n.T("Saved; restart with: cc-remote service restart", "已保存；用此命令重启生效：cc-remote service restart"))
		return nil
	}
	if err = stop(cfg, out); err != nil {
		return err
	}
	return start(cfg, out)
}

// field prints "Label:   value" with the value in column 16; East Asian
// characters count as two columns so Chinese labels line up too.
func field(out io.Writer, en, zh string, value any) {
	label := i18n.T(en+":", zh+"：")
	width := 0
	for _, r := range label {
		width++
		if r >= 0x1100 && (r <= 0x115f || r >= 0x2e80) {
			width++
		}
	}
	fmt.Fprintf(out, "%s%s%v\n", label, strings.Repeat(" ", max(15-width, 1)), value)
}

func printConfig(cfg *Config, out io.Writer) {
	if id, err := identity.Load(cfg.DataDir); err == nil {
		name, hex := id.Resolved()
		field(out, "Host name", "主机名称", identityValue(id.Name, name))
		field(out, "Host color", "主机颜色", identityValue(id.Color, hex))
	}
	field(out, "Listen", "监听地址", strings.Join(cfg.Listen, ", "))
	field(out, "Data dir", "数据目录", cfg.DataDir)
	field(out, "Claude", "Claude", cfg.Claude)
	if cfg.ClaudeConfigDir != "" {
		field(out, "Claude config", "Claude 配置", cfg.ClaudeConfigDir)
	}
	if cfg.AllowInsecureRemote {
		field(out, "Insecure bind", "不安全监听", i18n.T("allowed (--allow-insecure-remote)", "已允许（--allow-insecure-remote）"))
	}
	if cfg.Language != "" {
		field(out, "Language", "语言", cfg.Language)
	}
	if len(cfg.Env) > 0 {
		field(out, "Env", "环境变量", envSummary(cfg.Env))
	}
	field(out, "Executable", "可执行文件", cfg.Exe)
	field(out, "Log file", "日志文件", cfg.LogFile)
	field(out, "Installed", "安装时间", i18n.F("%s (version %s)", "%s（版本 %s）", cfg.InstalledAt.Local().Format(time.DateTime), cfg.Version))
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
	fmt.Fprint(out, i18n.F("WARNING: %s is reachable from other machines over plain HTTP. Anyone holding the access token can run code as you; use it only on a trusted LAN or behind HTTPS / a tunnel.\n", "警告：%s 可被其它机器以明文 HTTP 访问；持有访问令牌即可以你的身份执行代码，仅在可信局域网使用，或置于 HTTPS 反代 / 隧道之后。\n", strings.Join(exposed, ", ")))
	firewallHint(exposed, out)
}

// warning formats a "warning: ..." line.
func warning(v any) string { return i18n.F("warning: %v\n", "警告：%v\n", v) }

func start(cfg *Config, out io.Writer) error {
	if u, _ := findHost(cfg); u != "" {
		fmt.Fprint(out, i18n.F("Host already running at %s\n", "宿主已在 %s 运行\n", u))
		return nil
	}
	if err := schedulerStart(); err != nil {
		return i18n.Errorf("start service: %w", "启动服务失败：%w", err)
	}
	u, ok := waitReady(cfg, 10*time.Second)
	if !ok {
		return errors.New(i18n.T("service started but the host did not answer within 10s; see: cc-remote service logs", "服务已启动，但宿主 10 秒内没有响应；请查看：cc-remote service logs"))
	}
	fmt.Fprint(out, i18n.F("Host running at %s\n", "宿主正在 %s 运行\n", u))
	return nil
}

func stop(cfg *Config, out io.Writer) error {
	u, answering := findHost(cfg)
	if u != "" {
		if err := gracefulShutdown(u, readToken(cfg.DataDir)); err != nil {
			fmt.Fprint(out, i18n.F("Graceful shutdown failed (%v); using the service manager\n", "正常停止失败（%v）；改用服务管理器\n", err))
		} else if waitStopped(cfg, 15*time.Second) {
			_ = schedulerStop()
			fmt.Fprintln(out, i18n.T("Host stopped", "宿主已停止"))
			return nil
		} else {
			fmt.Fprintln(out, i18n.T("Host did not exit within 15s; using the service manager", "宿主 15 秒内没有退出；改用服务管理器"))
		}
	} else if answering {
		fmt.Fprint(out, warning(i18n.T("a service answers the listen address but rejects this data directory's token", "监听地址上有服务响应，但它不接受此数据目录的令牌")))
	}
	if err := schedulerStop(); err != nil && schedulerState().Installed {
		return i18n.Errorf("stop service: %w", "停止服务失败：%w", err)
	}
	if !waitStopped(cfg, 5*time.Second) {
		return errors.New(i18n.T("host is still answering; it may not be managed by the service (stop it manually)", "宿主仍在响应；它可能不受服务管理（请手动停止）"))
	}
	fmt.Fprintln(out, i18n.T("Service stopped", "服务已停止"))
	return nil
}

func uninstallCmd(args []string) error {
	fs := flag.NewFlagSet("service uninstall", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", dataDirHelp())
	purge := fs.Bool("purge-binary", false, i18n.T("also remove the installed executable copy", "同时删除安装时复制的可执行文件"))
	dryRun := fs.Bool("dry-run", false, dryRunHelp())
	if err := fs.Parse(args); err != nil {
		return err
	}
	out := os.Stdout
	cfg, err := LoadConfig(*dataDir)
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Fprintln(out, dryRunNoChanges())
		if cfg != nil {
			fmt.Fprint(out, i18n.F("[dry-run] stop the host at %s\n", "[dry-run] 停止 %s 上的宿主\n", strings.Join(listen.ProbeURLs(cfg.Listen), ", ")))
		}
	} else if cfg != nil {
		if err = stop(cfg, out); err != nil {
			fmt.Fprint(out, warning(err))
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
		fmt.Fprint(out, i18n.F("[dry-run] remove %s (data directory is kept)\n", "[dry-run] 删除 %s（保留数据目录）\n", filepath.Join(root, configName)))
	} else {
		removeConfigMirror()
	}
	if *purge {
		bin := filepath.Join(root, "bin")
		if *dryRun {
			fmt.Fprint(out, dryRunRemove(bin))
		} else {
			if err = os.RemoveAll(bin); err != nil {
				return err
			}
			fmt.Fprint(out, removed(bin))
		}
		// On Windows the ccr launcher and its user PATH entry live in bin.
		if err = removeFromPath(bin, *dryRun, out); err != nil {
			fmt.Fprint(out, warning(i18n.F("remove %s from PATH: %v", "从 PATH 中移除 %s 失败：%v", bin, err)))
		}
	}
	if !*dryRun {
		fmt.Fprintln(out, i18n.T("Service uninstalled; data directory kept.", "服务已卸载，数据目录已保留。"))
	}
	return nil
}

func statusCmd(args []string) error {
	fs := flag.NewFlagSet("service status", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", dataDirHelp())
	asJSON := fs.Bool("json", false, i18n.T("print machine-readable JSON", "输出机器可读的 JSON"))
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
		return i18n.T("yes", "是")
	}
	return i18n.T("no", "否")
}

func printStatus(out io.Writer, cfg *Config, state schedState, url, endpoint string, status map[string]any, statusErr error) {
	fmt.Fprint(out, i18n.F("Service (%s): installed=%s enabled=%s\n", "服务（%s）：已安装=%s 已启用=%s\n", managerName, yesNo(state.Installed), yesNo(state.Enabled)))
	if state.Detail != "" {
		field(out, "Scheduler", "计划任务", state.Detail)
	}
	notRunning := i18n.T("not running", "未运行")
	if cfg == nil {
		field(out, "service.json", "service.json", i18n.T("not found", "未找到"))
		field(out, "Host", "宿主", notRunning)
		return
	}
	printConfig(cfg, out)
	for _, l := range cfg.Listen {
		if !listen.IsLoopback(l) {
			fmt.Fprint(out, i18n.F("  %s: reachable from other machines over plain HTTP; the token grants code execution\n", "  %s：其它机器可明文访问，令牌即执行权限\n", l))
		}
	}
	if url == "" {
		field(out, "Host", "宿主", notRunning)
		return
	}
	field(out, "Host", "宿主", i18n.F("running at %s", "正在 %s 运行", url))
	if statusErr != nil {
		field(out, "Status error", "状态错误", statusErr)
		return
	}
	if endpoint == "/api/info" {
		field(out, "Claude", "Claude", i18n.F("%v (host has no /api/status)", "%v（宿主没有 /api/status）", status["claudeVersion"]))
		return
	}
	value := func(en, zh, key string) {
		if v, ok := status[key]; ok && v != nil && v != "" {
			field(out, en, zh, v)
		}
	}
	value("Version", "版本", "version")
	if v, ok := status["pid"].(float64); ok {
		field(out, "PID", "PID", int64(v))
	}
	if v, ok := status["uptimeSeconds"].(float64); ok {
		field(out, "Uptime", "运行时长", (time.Duration(v) * time.Second).String())
	}
	value("Bound", "已监听", "listen")
	if c, ok := status["claude"].(map[string]any); ok {
		if c["error"] != nil && c["error"] != "" {
			field(out, "Claude", "Claude", i18n.F("%v (error: %v)", "%v（错误：%v）", c["executable"], c["error"]))
		} else {
			field(out, "Claude", "Claude", fmt.Sprintf("%v %v", c["executable"], c["version"]))
		}
	}
	if s, ok := status["service"].(map[string]any); ok {
		field(out, "Managed", "托管", fmt.Sprintf("managed=%v manager=%v", s["managed"], s["manager"]))
	}
	printMap(out, "Sessions", "会话", status["sessions"])
	if v, ok := status["pendingRequests"].(float64); ok {
		field(out, "Pending", "待处理请求", int64(v))
	}
	printMap(out, "Totals", "累计", status["totals"])
}

func printMap(out io.Writer, en, zh string, v any) {
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
	field(out, en, zh, strings.Join(parts, " "))
}

func logsCmd(args []string) error {
	fs := flag.NewFlagSet("service logs", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", i18n.T("data directory (defaults to the installed service's)", "数据目录（默认为已安装服务的）"))
	lines := fs.Int("n", 100, i18n.T("number of lines to print", "打印的行数"))
	follow := fs.Bool("f", false, i18n.T("follow the log", "持续跟随日志"))
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

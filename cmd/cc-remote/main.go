package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"cc-remote/internal/channel"
	"cc-remote/internal/claude"
	"cc-remote/internal/config"
	"cc-remote/internal/hostlock"
	"cc-remote/internal/i18n"
	"cc-remote/internal/listen"
	"cc-remote/internal/logfile"
	"cc-remote/internal/server"
	"cc-remote/internal/service"
	"cc-remote/internal/store"
	"cc-remote/internal/terminal"
	"cc-remote/internal/update"
	webui "cc-remote/web"
)

// version is replaced at build time: go build -ldflags "-X main.version=1.2.3".
var version = "dev"

const usageEN = `CC Remote: remote web UI for Claude Code.

usage:
  cc-remote [host flags]                  run the host (web UI and API)
  ccr [--new] [ls | session]              attach to this directory's terminal session (or start one)
  ccr [claude options] [prompt]           start a terminal session with Claude Code options
  ccr -r [id] | -c                        continue a Claude Code session as a terminal session
  cc-remote [--data-dir D] -- claude ...  same as ccr -- [claude options]

commands:
  service    install/config/uninstall/start/stop/restart/status/logs the background service
  alias      install/uninstall/status the ccr launcher
  attach, a  connect this terminal to a terminal session the host holds (--new starts one here)
  ccr        the ccr launcher: see "ccr --help"
  update     update cc-remote to the latest release
  version    print version information
  help       show this help

host flags:
`

const usageZH = `CC Remote：Claude Code 的远程 Web 界面。

用法：
  cc-remote [宿主参数]                    运行宿主（Web 界面和 API）
  ccr [--new] [ls | 会话]                 接入当前目录的终端会话（没有则新建）
  ccr [claude 参数] [提示词]              带 Claude Code 参数新建终端会话
  ccr -r [ID] | -c                        以终端会话继续 Claude Code 会话
  cc-remote [--data-dir D] -- claude ...  等同于 ccr -- [claude 参数]

命令：
  service    安装/配置/卸载/启动/停止/重启/查看状态/查看日志（后台服务）
  alias      安装/卸载/查看 ccr 启动器
  attach, a  把当前终端接入宿主上的终端会话（--new 在此新建一个）
  ccr        ccr 启动器：见 "ccr --help"
  update     把 cc-remote 更新到最新版本
  version    显示版本信息
  help       显示本帮助

宿主参数：
`

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func main() {
	err := run()
	var exit service.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.Code)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// runCCR is ccr: a terminal session for this directory, a new one started
// with Claude Code options, or a Claude Code session to continue.
func runCCR(args []string) error {
	return terminal.CCR(args, config.DefaultDataDir(), channel.EnsureHost)
}

// isCCR reports whether the executable was invoked under the ccr alias name.
func isCCR(argv0 string) bool {
	base := filepath.Base(strings.ReplaceAll(argv0, `\`, "/"))
	return strings.EqualFold(strings.TrimSuffix(base, filepath.Ext(base)), "ccr")
}

// cliLanguage is the language of commands run by hand: CC_REMOTE_LANG, else
// the installed service's setting, else the system language.
func cliLanguage() i18n.Lang {
	if l, ok := i18n.Parse(os.Getenv("CC_REMOTE_LANG")); ok && l != i18n.Auto {
		return l
	}
	if cfg, err := service.LoadConfig(""); err == nil && cfg != nil {
		return i18n.Resolve(cfg.Language)
	}
	return i18n.Detect()
}

func run() error {
	// Hooks and internal subcommands run often and print nothing for a
	// person to read: dispatch them before cliLanguage reads service.json.
	if len(os.Args) > 1 && !isCCR(os.Args[0]) {
		switch os.Args[1] {
		case "hook":
			return claude.RunHook()
		case "permission-mcp":
			return claude.RunPermissionMCP()
		case "channel", "channel-session-hook":
			// Retired shared mode: a Claude Code process launched before the
			// upgrade may still call these; do nothing rather than start a host.
			return nil
		case "channel-ask-hook":
			return channel.AskHook()
		case "terminal-hook":
			return terminal.RunHook()
		case "terminal-status":
			return terminal.RunStatus()
		}
	}
	i18n.Set(cliLanguage())
	if isCCR(os.Args[0]) {
		return runCCR(os.Args[1:])
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version":
			fmt.Printf("cc-remote %s\n%s %s/%s\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		case "help", "-h", "--help", "-help":
			fs, _ := hostFlags()
			fs.SetOutput(os.Stdout)
			fs.Usage()
			return nil
		case "service":
			return service.Main(os.Args[2:], version)
		case "alias":
			return service.AliasMain(os.Args[2:])
		case "update":
			return update.Run(version, os.Args[2:])
		case "attach", "a":
			return terminal.Attach(os.Args[2:], config.DefaultDataDir())
		case "ccr":
			return runCCR(os.Args[2:])
		}
	}
	for i, arg := range os.Args[1:] {
		if arg == "--" {
			pos := i + 1
			if pos+1 >= len(os.Args) {
				return i18n.Errorf("usage: cc-remote [--data-dir directory] -- claude [arguments]", "用法：cc-remote [--data-dir 目录] -- claude [参数]")
			}
			// cc-remote [--host H] [--data-dir D] -- claude [options]: like
			// ccr, Claude Code starts with the options in a terminal session
			// (the host runs its own configured Claude Code executable).
			args := append([]string{}, os.Args[1:pos]...)
			args = append(args, "--")
			return runCCR(append(args, os.Args[pos+2:]...))
		}
	}
	return runHost(os.Args[1:])
}

type hostOptions struct {
	listen, allowOrigin                                    stringList
	dataDir, claudeBin, configDir, logFile, serviceManager *string
	allowInsecureRemote                                    *bool
	lang                                                   *string
}

func hostFlags() (*flag.FlagSet, *hostOptions) {
	fs := flag.NewFlagSet("cc-remote", flag.ExitOnError)
	o := &hostOptions{}
	fs.Var(&o.listen, "listen", i18n.T("HTTP listen address, repeatable or comma separated (default 127.0.0.1:8787; lan:PORT = all private IPv4 addresses, all:PORT = 0.0.0.0)", "HTTP 监听地址，可重复或用逗号分隔（默认 127.0.0.1:8787；lan:PORT = 所有私有 IPv4 地址，all:PORT = 0.0.0.0）"))
	o.dataDir = fs.String("data-dir", config.DefaultDataDir(), i18n.T("SQLite and access-token directory (or CC_REMOTE_DATA_DIR)", "SQLite 数据库和访问令牌所在目录（或 CC_REMOTE_DATA_DIR）"))
	o.claudeBin = fs.String("claude", "claude", i18n.T("Claude Code executable path", "Claude Code 可执行文件路径"))
	o.configDir = fs.String("claude-config-dir", "", i18n.T("Claude config directory (defaults to CLAUDE_CONFIG_DIR or ~/.claude)", "Claude 配置目录（默认取 CLAUDE_CONFIG_DIR 或 ~/.claude）"))
	o.logFile = fs.String("log-file", "", i18n.T("also write logs to this file (rotated at 10 MiB, one backup)", "同时把日志写入此文件（满 10 MiB 轮转，保留一份备份）"))
	o.allowInsecureRemote = fs.Bool("allow-insecure-remote", false, i18n.T("allow all:/0.0.0.0/:: listen addresses (plain HTTP on every interface)", "允许 all:/0.0.0.0/:: 监听地址（在所有网卡上提供明文 HTTP）"))
	fs.Var(&o.allowOrigin, "allow-origin", i18n.T("origin of a standalone web frontend allowed to use this host, such as https://ccr.example.com; repeatable or comma separated (or CC_REMOTE_ALLOW_ORIGINS)", "允许使用此宿主的独立网页前端来源，例如 https://ccr.example.com；可重复或用逗号分隔（或 CC_REMOTE_ALLOW_ORIGINS）"))
	o.lang = fs.String("lang", "", i18n.T("language of messages and the web UI: auto (system language), zh or en (or CC_REMOTE_LANG)", "提示信息和 Web 界面的语言：auto（跟随系统）、zh 或 en（或 CC_REMOTE_LANG）"))
	o.serviceManager = fs.String("service-manager", "", i18n.T("internal: set by the background service (schtasks, systemd, launchd)", "内部使用：由后台服务设置（schtasks、systemd、launchd）"))
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), i18n.T(usageEN, usageZH))
		fs.PrintDefaults()
	}
	return fs, o
}

// conflict logs err and exits with service.ExitConflict so the Windows
// supervisor can tell "another host owns this data dir or port" apart from a
// crash and stop retrying.
func conflict(err error) error {
	log.Printf("%v", err)
	return service.ExitError{Code: service.ExitConflict}
}

// addrInUse reports EADDRINUSE (WSAEADDRINUSE = 10048 on Windows).
func addrInUse(err error) bool {
	var errno syscall.Errno
	return errors.Is(err, syscall.EADDRINUSE) || (errors.As(err, &errno) && errno == 10048)
}

// warnExposure logs a prominent warning for every bind reachable from other
// machines: the API is plain HTTP and the token grants code execution.
func warnExposure(bound []string) {
	for _, addr := range bound {
		if listen.IsLoopback(addr) {
			continue
		}
		log.Printf("WARNING: %s is reachable from other machines over plain HTTP. Anyone with the access token can run code as this user; use only on a trusted LAN or behind HTTPS / a tunnel.", addr)
	}
}

// langArg finds --lang before flags are parsed, so the help text and parse
// errors already use it.
func langArg(argv []string) string {
	for i, arg := range argv {
		if arg == "--" {
			break
		}
		name, value, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if name != "lang" || !strings.HasPrefix(arg, "-") {
			continue
		}
		if inline {
			return value
		}
		if i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}

func runHost(argv []string) error {
	// Without --lang the host speaks like the other commands (run set it).
	if l, ok := i18n.Parse(langArg(argv)); ok && l != i18n.Auto {
		i18n.Set(l)
	}
	fs, opts := hostFlags()
	_ = fs.Parse(argv)
	// Under a service manager service.json is the source of truth, so a
	// restart applies "service config" changes without re-registering.
	var started *service.Config
	var startedErr error
	if *opts.serviceManager != "" {
		if started, startedErr = service.LoadConfig(*opts.dataDir); started != nil {
			applyServiceConfig(opts, started)
		}
	}
	if *opts.lang != "" {
		l, ok := i18n.Parse(*opts.lang)
		if !ok {
			return i18n.Errorf("--lang must be auto, zh or en (got %q)", "--lang 只能是 auto、zh 或 en（收到 %q）", *opts.lang)
		}
		i18n.Set(i18n.Resolve(string(l)))
	}
	if *opts.logFile != "" {
		path, err := filepath.Abs(*opts.logFile)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		w, err := logfile.Open(path, logfile.DefaultMaxSize)
		if err != nil {
			return err
		}
		defer w.Close()
		if *opts.serviceManager != "" {
			// Under a service manager stderr goes to launchd.log, the journal
			// or the supervisor; copying every line there would duplicate
			// host.log without its size limit.
			log.SetOutput(w)
		} else {
			// File first: MultiWriter stops at the first failing writer.
			log.SetOutput(io.MultiWriter(w, os.Stderr))
		}
		// Fatal runtime errors bypass the log package; keep them in host.log.
		if err = w.SetCrashOutput(); err != nil {
			log.Printf("crash output: %v", err)
		}
		*opts.logFile = path
	}
	if startedErr != nil {
		log.Printf("read service.json: %v; using the command line settings", startedErr)
	} else if started != nil && len(started.Env) > 0 {
		log.Printf("service environment: %s", service.EnvSummary(started.Env))
	}
	// Read by the status endpoint; set explicitly so a host spawned from a
	// managed host's children does not inherit the marker.
	if *opts.serviceManager != "" {
		os.Setenv("CC_REMOTE_SERVICE_MANAGER", *opts.serviceManager)
	} else {
		os.Unsetenv("CC_REMOTE_SERVICE_MANAGER")
	}
	specs := listen.Split(opts.listen)
	if len(specs) == 0 {
		specs = []string{"127.0.0.1:8787"}
	}
	if bad := listen.Insecure(specs); len(bad) > 0 && !*opts.allowInsecureRemote {
		return i18n.Errorf("--listen %s binds every interface over plain HTTP; use lan:PORT or a specific address, or pass --allow-insecure-remote if you really mean it", "--listen %s 会在所有网卡上提供明文 HTTP；请改用 lan:PORT 或具体地址，确有需要时加 --allow-insecure-remote", strings.Join(bad, ","))
	}
	addrs, added, err := listen.Resolve(specs, nil)
	if err != nil {
		return err
	}
	if added != "" {
		log.Printf("No loopback listen address configured; also listening on %s for local tools", added)
	}
	origins, err := listen.Origins(append([]string{os.Getenv("CC_REMOTE_ALLOW_ORIGINS")}, opts.allowOrigin...))
	if err != nil {
		return err
	}
	absData, err := filepath.Abs(*opts.dataDir)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(absData, 0700); err != nil {
		return err
	}
	release, err := hostlock.Acquire(absData)
	if err != nil {
		return conflict(err)
	}
	defer release()
	// Leftovers of the retired shared mode's launcher (best effort).
	for _, dir := range []string{"channel-config", "channel-locks"} {
		_ = os.RemoveAll(filepath.Join(absData, dir))
	}
	token := strings.TrimSpace(os.Getenv("CC_REMOTE_TOKEN"))
	tokenFile := filepath.Join(absData, "access-token")
	if token == "" {
		b, e := os.ReadFile(tokenFile)
		if e == nil {
			token = strings.TrimSpace(string(b))
		} else if !os.IsNotExist(e) {
			return e
		}
		if token == "" {
			var b [32]byte
			if _, e = rand.Read(b[:]); e != nil {
				return e
			}
			token = hex.EncodeToString(b[:])
			if e = os.WriteFile(tokenFile, []byte(token+"\n"), 0600); e != nil {
				return e
			}
		}
		log.Printf("Access token file: %s", tokenFile)
	}
	if len(token) < 16 {
		return i18n.Errorf("CC_REMOTE_TOKEN must be at least 16 characters", "CC_REMOTE_TOKEN 至少需要 16 个字符")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	hostExe, err := os.Executable()
	if err != nil {
		return err
	}
	claudeBin, configDir := *opts.claudeBin, *opts.configDir
	if configDir == "" {
		configDir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	// Claude Code reads .claude.json from CLAUDE_CONFIG_DIR when it is set and
	// from the home directory otherwise: only a chosen directory is passed on.
	explicitConfig := configDir != ""
	if configDir == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		configDir = filepath.Join(home, ".claude")
	}
	configDir, err = filepath.Abs(configDir)
	if err != nil {
		return err
	}
	configEnv := ""
	if explicitConfig {
		configEnv = configDir
	}
	st, err := store.Open(filepath.Join(absData, "cc-remote.sqlite"))
	if err != nil {
		return err
	}
	defer st.Close()
	public := &http.Server{ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errs := make(chan error, 1)
	ls := newListenerSet(public, errs)
	defer ls.closeAll()
	for _, addr := range addrs {
		l, e := listenOn(addr)
		if e != nil {
			if addrInUse(e) {
				return conflict(e)
			}
			return e
		}
		ls.add(addr, l)
	}
	// The public bind may be a LAN-only IP. A separate loopback listener keeps
	// process-local hooks available without exposing run credentials remotely.
	bridgeListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer bridgeListener.Close()
	channelURL := "http://" + bridgeListener.Addr().String()
	wildcardPort := 0
	bound := ls.bound()
	loopback, wildcard := false, ""
	for i, l := range ls.listeners() {
		addr, ok := l.Addr().(*net.TCPAddr)
		if !ok {
			continue
		}
		if addr.IP.IsLoopback() && !loopback {
			channelURL, loopback = "http://"+addr.String(), true
			ls.keep = addrs[i]
		} else if addr.IP.IsUnspecified() && wildcardPort == 0 {
			wildcardPort, wildcard = addr.Port, addrs[i]
		}
	}
	if !loopback && wildcardPort != 0 {
		channelURL = fmt.Sprintf("http://127.0.0.1:%d", wildcardPort)
		ls.keep = wildcard
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	prefix := ""
	if runtime.GOOS == "windows" {
		prefix = "& "
		quote = func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
	}
	attachCommand := prefix + quote(hostExe) + " attach --host " + quote(channelURL) + " --data-dir " + quote(absData)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Under a service manager the host can restart itself: it exits with
	// ExitRestart and the manager starts it again (with an updated binary).
	var restartRequested atomic.Bool
	var restart func()
	var reload func() (server.ReloadResult, error)
	if *opts.serviceManager != "" {
		restart = func() { restartRequested.Store(true); stop() }
		reload = func() (server.ReloadResult, error) { return reloadSettings(absData, started, i18n.Current(), ls) }
	}
	app := server.New(st, server.Config{UploadDir: filepath.Join(absData, "uploads"), Token: token, ClaudeExecutable: claudeBin, HostExecutable: hostExe, Cwd: cwd, ClaudeConfigDir: configDir, ClaudeConfigEnv: configEnv, Language: i18n.Current(), AllowOrigins: origins, BridgeURL: "http://" + bridgeListener.Addr().String() + "/api/bridge/request", AttachCommand: attachCommand, Assets: webui.Files(), DataDir: absData, Listen: strings.Join(bound, ","), Version: version, LogFile: *opts.logFile, StartedAt: time.Now(), Shutdown: stop, Restart: restart, Reload: reload})
	public.Handler = app.Handler()
	bridge := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ls.serve()
	go func() {
		err := bridge.Serve(bridgeListener)
		select {
		case errs <- err:
		default:
		}
	}()
	for _, addr := range bound {
		log.Printf("CC Remote listening at http://%s", addr)
	}
	warnExposure(bound)
	if len(origins) > 0 {
		log.Printf("Standalone web frontends allowed: %s", strings.Join(origins, ", "))
	}
	log.Printf("Claude config: %s", configDir)
	go app.RestoreTerminals() // the sessions running when the host last stopped
	select {
	case <-ctx.Done():
	case e := <-errs:
		if e != nil && e != http.ErrServerClosed {
			log.Printf("HTTP server: %v", e)
		}
	}
	log.Printf("Shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	appErr := app.Close(shutdown)
	publicErr := public.Shutdown(shutdown)
	bridgeErr := bridge.Shutdown(shutdown)
	if restartRequested.Load() {
		log.Printf("Exiting for a restart by the service manager")
		return service.ExitError{Code: service.ExitRestart}
	}
	if appErr != nil {
		return appErr
	}
	if publicErr != nil {
		return publicErr
	}
	return bridgeErr
}

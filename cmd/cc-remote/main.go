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
	"syscall"
	"time"

	"cc-remote/internal/channel"
	"cc-remote/internal/claude"
	"cc-remote/internal/config"
	"cc-remote/internal/hostlock"
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

const usage = `CC Remote: remote web UI for Claude Code.

usage:
  cc-remote [host flags]                  run the host (web UI and API)
  cc-remote [--data-dir D] -- claude ...  start native Claude Code shared with the web UI
  ccr [claude args]                       same as "cc-remote -- claude [claude args]"

commands:
  service    install/config/uninstall/start/stop/restart/status/logs the background service
  alias      install/uninstall/status the ccr launcher
  terminal   start a shared terminal session (legacy form of "--")
  attach     connect this terminal to a terminal session the host holds (--new starts one here)
  update     update cc-remote to the latest release
  version    print version information
  help       show this help

host flags:
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

// isCCR reports whether the executable was invoked under the ccr alias name.
func isCCR(argv0 string) bool {
	base := filepath.Base(strings.ReplaceAll(argv0, `\`, "/"))
	return strings.EqualFold(strings.TrimSuffix(base, filepath.Ext(base)), "ccr")
}

func run() error {
	if isCCR(os.Args[0]) {
		return channel.Launch(append([]string{"--claude", "claude", "--"}, os.Args[1:]...))
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
		case "terminal":
			return channel.Launch(os.Args[2:])
		case "attach":
			return terminal.Attach(os.Args[2:], config.DefaultDataDir())
		}
	}
	for i, arg := range os.Args[1:] {
		if arg == "--" {
			pos := i + 1
			if pos+1 >= len(os.Args) {
				return fmt.Errorf("usage: cc-remote [--data-dir directory] -- claude [arguments]")
			}
			args := append([]string{}, os.Args[1:pos]...)
			args = append(args, "--claude", os.Args[pos+1], "--")
			args = append(args, os.Args[pos+2:]...)
			return channel.Launch(args)
		}
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "hook":
			return claude.RunHook()
		case "permission-mcp":
			return claude.RunPermissionMCP()
		case "channel":
			return channel.Run()
		case "channel-session-hook":
			return channel.SessionHook()
		case "channel-ask-hook":
			return channel.AskHook()
		case "terminal-hook":
			return terminal.RunHook()
		}
	}
	return runHost(os.Args[1:])
}

type hostOptions struct {
	listen                                                 stringList
	dataDir, claudeBin, configDir, logFile, serviceManager *string
	allowInsecureRemote                                    *bool
}

func hostFlags() (*flag.FlagSet, *hostOptions) {
	fs := flag.NewFlagSet("cc-remote", flag.ExitOnError)
	o := &hostOptions{}
	fs.Var(&o.listen, "listen", "HTTP listen address, repeatable or comma separated (default 127.0.0.1:8787; lan:PORT = all private IPv4 addresses, all:PORT = 0.0.0.0)")
	o.dataDir = fs.String("data-dir", config.DefaultDataDir(), "SQLite and access-token directory (or CC_REMOTE_DATA_DIR)")
	o.claudeBin = fs.String("claude", "claude", "Claude Code executable path")
	o.configDir = fs.String("claude-config-dir", "", "Claude config directory (defaults to CLAUDE_CONFIG_DIR or ~/.claude)")
	o.logFile = fs.String("log-file", "", "also write logs to this file (rotated at 10 MiB, one backup)")
	o.allowInsecureRemote = fs.Bool("allow-insecure-remote", false, "allow all:/0.0.0.0/:: listen addresses (plain HTTP on every interface)")
	o.serviceManager = fs.String("service-manager", "", "internal: set by the background service (schtasks, systemd, launchd)")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
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
		log.Printf("警告：%s 可被其它机器以明文 HTTP 访问。持有访问令牌即可以当前用户身份执行代码；仅在可信局域网使用，或置于 HTTPS 反代 / 隧道之后。", addr)
	}
}

func runHost(argv []string) error {
	fs, opts := hostFlags()
	_ = fs.Parse(argv)
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
		return fmt.Errorf("--listen %s binds every interface over plain HTTP; use lan:PORT or a specific address, or pass --allow-insecure-remote if you really mean it", strings.Join(bad, ","))
	}
	addrs, added, err := listen.Resolve(specs, nil)
	if err != nil {
		return err
	}
	if added != "" {
		log.Printf("No loopback listen address configured; also listening on %s for local tools", added)
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
		return fmt.Errorf("CC_REMOTE_TOKEN must be at least 16 characters")
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
	st, err := store.Open(filepath.Join(absData, "cc-remote.sqlite"))
	if err != nil {
		return err
	}
	defer st.Close()
	var listeners []net.Listener
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()
	for _, addr := range addrs {
		l, e := net.Listen("tcp", addr)
		if e != nil {
			e = fmt.Errorf("listen on %s: %w", addr, e)
			if addrInUse(e) {
				return conflict(e)
			}
			return e
		}
		listeners = append(listeners, l)
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
	bound := make([]string, 0, len(listeners))
	loopback := false
	for _, l := range listeners {
		bound = append(bound, l.Addr().String())
		addr, ok := l.Addr().(*net.TCPAddr)
		if !ok {
			continue
		}
		if addr.IP.IsLoopback() && !loopback {
			channelURL, loopback = "http://"+addr.String(), true
		} else if addr.IP.IsUnspecified() && wildcardPort == 0 {
			wildcardPort = addr.Port
		}
	}
	if !loopback && wildcardPort != 0 {
		channelURL = fmt.Sprintf("http://127.0.0.1:%d", wildcardPort)
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	prefix := ""
	if runtime.GOOS == "windows" {
		prefix = "& "
		quote = func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
	}
	terminalCommand := prefix + quote(hostExe) + " --host " + quote(channelURL) + " --data-dir " + quote(absData) + " --claude-config-dir " + quote(configDir) + " -- " + quote(claudeBin)
	attachCommand := prefix + quote(hostExe) + " attach --host " + quote(channelURL) + " --data-dir " + quote(absData)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app := server.New(st, server.Config{UploadDir: filepath.Join(absData, "uploads"), Token: token, ClaudeExecutable: claudeBin, HostExecutable: hostExe, Cwd: cwd, ClaudeConfigDir: configDir, BridgeURL: "http://" + bridgeListener.Addr().String() + "/api/bridge/request", TerminalCommand: terminalCommand, AttachCommand: attachCommand, Assets: webui.Files(), DataDir: absData, Listen: strings.Join(bound, ","), Version: version, LogFile: *opts.logFile, StartedAt: time.Now(), Shutdown: stop})
	public := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	bridge := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errs := make(chan error, len(listeners)+1)
	for _, l := range listeners {
		go func(l net.Listener) { errs <- public.Serve(l) }(l)
	}
	go func() { errs <- bridge.Serve(bridgeListener) }()
	for _, addr := range bound {
		log.Printf("CC Remote listening at http://%s", addr)
	}
	warnExposure(bound)
	log.Printf("Claude config: %s", configDir)
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
	if appErr != nil {
		return appErr
	}
	if publicErr != nil {
		return publicErr
	}
	return bridgeErr
}

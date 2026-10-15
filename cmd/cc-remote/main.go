package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
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
	"cc-remote/internal/server"
	"cc-remote/internal/store"
	webui "cc-remote/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "terminal" {
		return channel.Launch(os.Args[2:])
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
		case "terminal":
			return channel.Launch(os.Args[2:])
		}
	}
	listen := flag.String("listen", "127.0.0.1:8787", "HTTP listen address (use 0.0.0.0:8787 for remote access)")
	dataDir := flag.String("data-dir", config.DefaultDataDir(), "SQLite and access-token directory (or CC_REMOTE_DATA_DIR)")
	claudeBin := flag.String("claude", "claude", "Claude Code executable path")
	configDir := flag.String("claude-config-dir", "", "Claude config directory (defaults to CLAUDE_CONFIG_DIR or ~/.claude)")
	flag.Parse()
	absData, err := filepath.Abs(*dataDir)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(absData, 0700); err != nil {
		return err
	}
	release, err := hostlock.Acquire(absData)
	if err != nil {
		return err
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
	if *configDir == "" {
		*configDir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if *configDir == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		*configDir = filepath.Join(home, ".claude")
	}
	*configDir, err = filepath.Abs(*configDir)
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(absData, "cc-remote.sqlite"))
	if err != nil {
		return err
	}
	defer st.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	// The public bind may be a LAN-only IP. A separate loopback listener keeps
	// process-local hooks available without exposing run credentials remotely.
	bridgeListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer bridgeListener.Close()
	channelURL := "http://" + bridgeListener.Addr().String()
	if addr, ok := listener.Addr().(*net.TCPAddr); ok && (addr.IP.IsLoopback() || addr.IP.IsUnspecified()) {
		channelURL = fmt.Sprintf("http://127.0.0.1:%d", addr.Port)
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	prefix := ""
	if runtime.GOOS == "windows" {
		prefix = "& "
		quote = func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
	}
	terminalCommand := prefix + quote(hostExe) + " --host " + quote(channelURL) + " --data-dir " + quote(absData) + " --claude-config-dir " + quote(*configDir) + " -- " + quote(*claudeBin)
	app := server.New(st, server.Config{UploadDir: filepath.Join(absData, "uploads"), Token: token, ClaudeExecutable: *claudeBin, HostExecutable: hostExe, Cwd: cwd, ClaudeConfigDir: *configDir, BridgeURL: "http://" + bridgeListener.Addr().String() + "/api/bridge/request", TerminalCommand: terminalCommand, Assets: webui.Files()})
	public := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	bridge := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errs := make(chan error, 2)
	go func() { errs <- public.Serve(listener) }()
	go func() { errs <- bridge.Serve(bridgeListener) }()
	log.Printf("CC Remote listening at http://%s", listener.Addr())
	log.Printf("Claude config: %s", *configDir)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case e := <-errs:
		if e != nil && e != http.ErrServerClosed {
			log.Printf("HTTP server: %v", e)
		}
	}
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

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"cc-remote/internal/channel"
	"cc-remote/internal/claude"
	"cc-remote/internal/config"
	"cc-remote/internal/i18n"
	"cc-remote/internal/listen"
	"cc-remote/internal/service"
	"cc-remote/internal/statusline"
	"cc-remote/internal/terminal"
	"cc-remote/internal/update"
)

type cliRoute struct {
	command string
	args    []string
}

// routeCLI does not inspect the executable name. Only leading context options
// are consumed before selecting a command; Claude arguments are never scanned.
func routeCLI(args []string) cliRoute {
	var context []string
	for len(args) > 0 {
		name, _, inline := strings.Cut(strings.TrimLeft(args[0], "-"), "=")
		if strings.HasPrefix(args[0], "-") && (name == "host" || name == "data-dir") {
			n := 1
			if !inline && len(args) > 1 {
				n = 2
			}
			context = append(context, args[:n]...)
			args = args[n:]
			continue
		}
		if args[0] == "ccr" {
			args = args[1:]
			continue
		} // old installed launcher
		break
	}
	command := "session"
	// Retain the old `cc-remote [context] -- claude [options]` spelling
	// as a managed terminal launch, identically under either executable name.
	if len(args) > 1 && args[0] == "--" && args[1] == "claude" {
		args = append([]string{"--"}, args[2:]...)
	}
	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-h", "-help":
			command = "help"
			args = args[1:]
		case "version", "--version":
			command = "version"
			args = args[1:]
		case "session", "serve", "service", "status", "start", "stop", "restart", "logs", "config", "alias", "update", "attach", "a", "claude", "oauth", "profile", "hook", "permission-mcp", "channel", "channel-session-hook", "channel-ask-hook", "terminal-hook", "terminal-status":
			command = args[0]
			args = args[1:]
		default:
			name, _, _ := strings.Cut(strings.TrimLeft(args[0], "-"), "=")
			if strings.HasPrefix(args[0], "-") && legacyHostFlag(name) {
				command = "serve"
			}
		}
	}
	// --host is connection context, not a remote service-management option.
	if command != "session" && command != "claude" && command != "oauth" && command != "profile" && command != "attach" && command != "a" {
		var local []string
		for i := 0; i < len(context); i++ {
			name, _, inline := strings.Cut(strings.TrimLeft(context[i], "-"), "=")
			n := 1
			if !inline && i+1 < len(context) {
				n = 2
			}
			if name == "data-dir" {
				local = append(local, context[i:i+n]...)
			}
			i += n - 1
		}
		context = local
	}
	if command == "help" || command == "version" || command == "alias" || command == "update" || internalCommand(command) {
		context = nil
	}
	if hasSubcommand(command) {
		if len(args) == 0 {
			return cliRoute{command: command}
		}
		return cliRoute{command, append(append([]string{args[0]}, context...), args[1:]...)}
	}
	return cliRoute{command, append(context, args...)}
}

func hasSubcommand(command string) bool {
	return command == "service" || command == "oauth" || command == "profile"
}

func internalCommand(command string) bool {
	switch command {
	case "hook", "permission-mcp", "channel", "channel-session-hook", "channel-ask-hook", "terminal-hook", "terminal-status":
		return true
	}
	return false
}

func legacyHostFlag(name string) bool {
	switch name {
	case "listen", "claude", "codex", "codex-home", "cc-switch-db", "claude-config-dir", "log-file", "allow-insecure-remote", "allow-origin", "lang", "service-manager":
		return true
	}
	return false
}

// connectionDefaults makes direct invocation and installed launchers agree,
// including installations with a custom directory or loopback port.
func connectionDefaults(args []string) []string {
	return connectionDefaultsFor("session", args)
}

func connectionDefaultsFor(command string, args []string) []string {
	dir := os.Getenv("CC_REMOTE_DATA_DIR")
	flags := args
	if hasSubcommand(command) {
		if len(args) == 0 {
			return args
		}
		flags = args[1:]
	}
	dir = connectionDataDir(command, flags, dir)
	cfg, _ := service.LoadConfig(dir)
	if cfg == nil {
		return args
	}
	var defaults []string
	if dir == "" && cfg.DataDir != "" {
		defaults = append(defaults, "--data-dir", cfg.DataDir)
	}
	if urls := listen.ProbeURLs(cfg.Listen); len(urls) > 0 {
		defaults = append(defaults, "--host", urls[0])
	}
	if hasSubcommand(command) {
		return append(append([]string{args[0]}, defaults...), args[1:]...)
	}
	return append(defaults, args...)
}

// Follow each target parser's option/value boundaries. In particular a Claude
// option can take the literal "--data-dir" as its value; it is not our context.
func connectionDataDir(command string, args []string, dir string) string {
	afterDash := false
	for i := 0; i < len(args); {
		a := args[i]
		if a == "--" {
			if command != "session" {
				break
			}
			afterDash = true
			i++
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			if command == "claude" || command == "attach" || command == "a" {
				break
			}
			i++
			continue
		}
		name, value, inline := strings.Cut(strings.TrimLeft(a, "-"), "=")
		// These are the value-taking options of the existing command parsers.
		own := name == "host" || name == "data-dir"
		switch command {
		case "session":
			own = (strings.HasPrefix(a, "--") || !afterDash) && (own || name == "title" || name == "model" || name == "effort" || name == "permission-mode" || name == "profile")
		case "claude":
			own = own || name == "profile" || name == "claude"
		case "oauth":
			own = own || name == "token"
		case "profile":
			own = own || name == "app"
		case "attach", "a":
			own = own || name == "title"
		}
		if own {
			if !inline && i+1 < len(args) {
				i++
				value = args[i]
				inline = true
			}
			if name == "data-dir" && inline {
				dir = value
			}
			i++
			continue
		}
		if command == "session" {
			i += len(claude.ParseArgs(args[i:])[0].Words())
		} else {
			i++
		}
	}
	return dir
}

func runCLI(args []string) error {
	if len(args) > 0 && args[0] == "statusline" {
		style := "compact"
		if len(args) > 1 {
			style = args[1]
		}
		var options *statusline.Options
		if len(args) > 2 {
			options = statusline.Decode(args[2])
		}
		fmt.Fprintln(os.Stdout, statusline.RenderOptions(os.Stdin, style, statusline.Provider(), options))
		return nil
	}
	route := routeCLI(args)
	switch route.command {
	case "help":
		fmt.Fprint(os.Stdout, i18n.T(usageEN, usageZH))
		return nil
	case "version":
		fmt.Printf("ccr %s\n%s %s/%s\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return nil
	case "serve":
		return runHost(route.args)
	case "service":
		return service.Main(route.args, version)
	case "status", "start", "stop", "restart", "logs", "config":
		return service.Main(append([]string{route.command}, route.args...), version)
	case "alias":
		return service.AliasMain(route.args)
	case "update":
		return update.Run(version, route.args)
	}
	args = connectionDefaultsFor(route.command, route.args)
	switch route.command {
	case "attach", "a":
		return terminal.Attach(args, config.DefaultDataDir())
	case "claude":
		return terminal.RunClaudeDirect(args, "", config.DefaultDataDir(), channel.EnsureHost)
	case "oauth":
		return terminal.RunOAuth(args, config.DefaultDataDir(), channel.EnsureHost)
	case "profile":
		return terminal.RunProfile(args, config.DefaultDataDir(), channel.EnsureHost)
	default:
		return runCCR(args)
	}
}

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"cc-remote/internal/channel"
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
// are consumed before selecting a command; native arguments are never scanned.
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
		break
	}
	command := "open"
	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-h", "-help":
			command = "help"
			args = args[1:]
		case "version", "--version":
			command = "version"
			args = args[1:]
		case "--new", "--resume":
			// Keep the shortcut and its value for the Agent option parser.
		case "list", "ls":
			command = "ls"
			args = args[1:]
		case "session", "shell", "run", "serve", "service", "status", "start", "stop", "restart", "logs", "config", "alias", "update", "claude", "codex", "local", "oauth", "profile", "hook", "permission-mcp", "channel", "channel-session-hook", "channel-ask-hook", "terminal-hook", "terminal-status":
			command = args[0]
			args = args[1:]
		default:
			if strings.HasPrefix(args[0], "--resume=") {
				break
			}
			name, _, _ := strings.Cut(strings.TrimLeft(args[0], "-"), "=")
			if strings.HasPrefix(args[0], "-") && legacyHostFlag(name) {
				command = "serve"
			} else {
				return cliRoute{"unknown", args}
			}
		}
	}
	// --host is connection context, not a remote service-management option.
	if !usesConnection(command) {
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

func usesConnection(command string) bool {
	switch command {
	case "open", "ls", "session", "claude", "codex", "local", "shell", "run", "profile", "oauth":
		return true
	}
	return false
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
	case "listen", "claude", "codex", "codex-home", "cc-switch-db", "claude-config-dir", "log-file", "allow-insecure-remote", "allow-origin", "project-shell", "lang", "service-manager":
		return true
	}
	return false
}

// connectionDefaults makes direct invocation and installed launchers agree,
// including installations with a custom directory or loopback port.
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

// Follow each target parser's option/value boundaries. Native flags and values
// after -- belong entirely to the command running in the terminal.
func connectionDataDir(command string, args []string, dir string) string {
	for i := 0; i < len(args); {
		a := args[i]
		if a == "--" {
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			if command == "run" {
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
			own = own || name == "agent"
		case "claude", "codex":
			own = own || name == "cwd" || name == "title" || name == "prompt" || name == "model" || name == "supplier" || name == "effort" || name == "permission-mode"
		case "local":
			own = own || name == "supplier" || name == "claude"
		case "shell", "run":
			own = own || name == "cwd"
		case "oauth":
			own = own || name == "token"
		case "profile":
			own = own || name == "app"
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
		i++
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
	case "unknown":
		return i18n.Errorf("unknown command %q; see ccr --help", "未知命令 %q，请查看 ccr --help", route.args[0])
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
	case "session":
		return terminal.SessionCLI(args, config.DefaultDataDir())
	case "shell":
		return terminal.ProjectShellCLI(args, config.DefaultDataDir(), channel.EnsureHost)
	case "run":
		return terminal.ProjectCommandCLI(args, config.DefaultDataDir(), channel.EnsureHost)
	case "local":
		return terminal.RunClaudeDirect(args, config.DefaultDataDir(), channel.EnsureHost)
	case "claude", "codex":
		return terminal.AgentCLI(args, route.command, false, config.DefaultDataDir(), channel.EnsureHost)
	case "open", "ls":
		return terminal.AgentCLI(args, "", route.command == "ls", config.DefaultDataDir(), channel.EnsureHost)
	case "oauth":
		return terminal.RunOAuth(args, config.DefaultDataDir(), channel.EnsureHost)
	case "profile":
		return terminal.RunProfile(args, config.DefaultDataDir(), channel.EnsureHost)
	default:
		return i18n.Errorf("unknown command %q", "未知命令 %q", route.command)
	}
}

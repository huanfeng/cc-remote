package claude

import "strings"

// Claude Code's command-line options by how they take values, as its own
// parser (commander) reads them: a value, an optional value (the next word
// unless it is an option), or every following word up to the next option.
var (
	valueFlags = flagSet("--agent", "--agents", "--append-system-prompt", "--append-system-prompt-file",
		"--autocompact", "--debug-file", "--effort", "--environment", "--fallback-model", "--input-format",
		"--json-schema", "--max-budget-usd", "--model", "-n", "--name", "--output-format", "--permission-mode",
		"--permission-prompts", "--plugin-dir", "--plugin-url", "--remote-control-session-name-prefix",
		"--session-id", "--setting-sources", "--settings", "--system-prompt", "--system-prompt-file",
		"--system-prompt-snapshot")
	optionalFlags = flagSet("-d", "--debug", "--cloud", "--from-pr", "--prompt-suggestions", "--remote-control",
		"--teleport", "-w", "--worktree", "-r", "--resume")
	listFlags = flagSet("--add-dir", "--allowedTools", "--allowed-tools", "--betas", "--disallowedTools",
		"--disallowed-tools", "--file", "--mcp-config", "--tools")
	// hostFlags conflict with how the host runs Claude Code in a terminal
	// session (it owns the settings, session ID and transcript) or do not run
	// an interactive session there.
	hostFlags = flagSet("-p", "--print", "--output-format", "--input-format", "--json-schema",
		"--include-partial-messages", "--include-hook-events", "--replay-user-messages", "--session-id",
		"--settings", "--bare", "--safe-mode", "--no-session-persistence", "--fork-session", "--from-pr",
		"--bg", "--background", "--desktop", "--cloud", "--teleport", "--tmux", "-h", "--help", "-v", "--version",
		"-r", "--resume", "-c", "--continue", "--model", "--effort", "--permission-mode")
)

func flagSet(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// Arg is one option of a Claude Code command line with its values.
type Arg struct {
	Name   string   // "--add-dir", or "" for a positional word (the prompt)
	Values []string // the values as given (one for --name=value)
	Inline bool     // written as --name=value
}

// Words is the option as command-line words.
func (a Arg) Words() []string {
	if a.Name == "" {
		return a.Values
	}
	if a.Inline {
		return []string{a.Name + "=" + a.Values[0]}
	}
	return append([]string{a.Name}, a.Values...)
}

// ParseArgs splits a Claude Code command line into options and positional
// words; after "--" every word is positional.
func ParseArgs(args []string) []Arg {
	var out []Arg
	for i := 0; i < len(args); i++ {
		word := args[i]
		if word == "--" {
			for _, w := range args[i+1:] {
				out = append(out, Arg{Values: []string{w}})
			}
			break
		}
		if !isOption(word) {
			out = append(out, Arg{Values: []string{word}})
			continue
		}
		if name, value, ok := strings.Cut(word, "="); ok && strings.HasPrefix(word, "--") {
			out = append(out, Arg{Name: name, Values: []string{value}, Inline: true})
			continue
		}
		a := Arg{Name: word}
		switch {
		case valueFlags[word]:
			if i+1 < len(args) {
				i++
				a.Values = []string{args[i]}
			}
		case optionalFlags[word]:
			if i+1 < len(args) && !isOption(args[i+1]) {
				i++
				a.Values = []string{args[i]}
			}
		case listFlags[word]:
			for i+1 < len(args) && !isOption(args[i+1]) {
				i++
				a.Values = append(a.Values, args[i])
			}
		}
		out = append(out, a)
	}
	return out
}

func isOption(word string) bool { return len(word) > 1 && word[0] == '-' && word != "--" }

// HostFlag reports an option a terminal session cannot pass to Claude Code
// unchanged (the host sets it, or cc-remote has its own form of it).
func HostFlag(name string) bool { return hostFlags[name] }

// CheckTerminalArgs returns the first word of args a terminal session cannot
// pass to Claude Code: a host option or a positional word (prompts are sent
// as messages, so a resumed session does not get them again).
func CheckTerminalArgs(args []string) string {
	for _, a := range ParseArgs(args) {
		if a.Name == "" {
			return a.Values[0]
		}
		if HostFlag(a.Name) {
			return a.Name
		}
	}
	return ""
}

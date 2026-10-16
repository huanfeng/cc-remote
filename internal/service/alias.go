package service

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"cc-remote/internal/config"
	"cc-remote/internal/listen"
)

type aliasOptions struct {
	dir    string
	dryRun bool
}

const aliasUsage = `usage: cc-remote alias <install|uninstall|status> [--dir D]

"ccr [claude args]" runs "cc-remote -- claude [claude args]".
`

// AliasMain runs "cc-remote alias ...".
func AliasMain(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(aliasUsage)
		return nil
	}
	fs := flag.NewFlagSet("alias "+args[0], flag.ContinueOnError)
	def, err := defaultAliasDir()
	if err != nil {
		def = "unavailable: " + err.Error()
	}
	dir := fs.String("dir", "", "directory for the ccr launcher (default "+def+")")
	dryRun := fs.Bool("dry-run", false, "print what would be done without changing anything")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	opts := aliasOptions{dir: *dir, dryRun: *dryRun}
	switch args[0] {
	case "install":
		return installAlias(opts, os.Stdout)
	case "uninstall":
		return uninstallAlias(opts, os.Stdout)
	case "status":
		return aliasStatus(opts, os.Stdout)
	}
	return fmt.Errorf("unknown alias command %q\n\n%s", args[0], aliasUsage)
}

func aliasPath(dir string) (string, error) {
	if dir == "" {
		var err error
		if dir, err = defaultAliasDir(); err != nil {
			return "", err
		}
	}
	name := "ccr"
	if runtime.GOOS == "windows" {
		name = "ccr.cmd"
	}
	return filepath.Join(dir, name), nil
}

// aliasTarget returns the executable and the cc-remote flags ccr passes:
// the installed service copy with its data dir and host when present.
func aliasTarget(out io.Writer) (string, []string, error) {
	cfg, _ := LoadConfig("")
	if cfg != nil && cfg.Exe != "" {
		if _, err := os.Stat(cfg.Exe); err == nil {
			var flags []string
			if !samePath(cfg.DataDir, config.DefaultDataDir()) {
				flags = append(flags, "--data-dir", cfg.DataDir)
			}
			// ProbeURLs only yields loopback URLs; ccr refuses other hosts.
			if urls := listen.ProbeURLs(cfg.Listen); len(urls) > 0 && urls[0] != "http://127.0.0.1:8787" {
				flags = append(flags, "--host", urls[0])
			}
			return cfg.Exe, flags, nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	fmt.Fprintf(out, "warning: service is not installed; ccr will point to %s (rebuilding or moving it breaks the alias)\n", exe)
	return exe, nil, nil
}

// AliasScript renders the ccr launcher for goos.
func AliasScript(goos, exe string, flags []string) string {
	if goos == "windows" {
		parts := []string{WindowsCmdQuote(exe), "ccr"}
		for _, f := range flags {
			parts = append(parts, WindowsCmdQuote(f))
		}
		return "@echo off\r\n" + strings.Join(parts, " ") + " %*\r\n"
	}
	parts := []string{ShellQuote(exe), "ccr"}
	for _, f := range flags {
		parts = append(parts, ShellQuote(f))
	}
	return "#!/bin/sh\n# ccr: cc-remote terminal session launcher\nexec " + strings.Join(parts, " ") + " \"$@\"\n"
}

// aliasInstalled reports whether the default ccr launcher exists.
func aliasInstalled() bool {
	path, err := aliasPath("")
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func installAlias(opts aliasOptions, out io.Writer) error {
	exe, flags, err := aliasTarget(out)
	if err != nil {
		return err
	}
	path, err := aliasPath(opts.dir)
	if err != nil {
		return err
	}
	script := AliasScript(runtime.GOOS, exe, flags)
	if opts.dryRun {
		fmt.Fprintf(out, "[dry-run] write %s:\n%s\n", path, script)
	} else {
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err = os.WriteFile(path, []byte(script), 0755); err != nil {
			return err
		}
		if err = os.Chmod(path, 0755); err != nil {
			return err
		}
		fmt.Fprintf(out, "Wrote %s\n", path)
	}
	if err = linkCommand(filepath.Dir(path), exe, opts.dryRun, out); err != nil {
		return err
	}
	return ensureOnPath(filepath.Dir(path), opts.dryRun, out)
}

// commandLink is the "cc-remote" entry placed next to ccr on Unix. On Windows
// the installed exe already lives in the PATH directory next to ccr.cmd.
func commandLink(dir string) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return filepath.Join(dir, "cc-remote")
}

// linkCommand points dir/cc-remote at exe so the CLI is on PATH like ccr.
// It only replaces a symlink, never a regular file the user put there.
func linkCommand(dir, exe string, dryRun bool, out io.Writer) error {
	link := commandLink(dir)
	if link == "" || samePath(link, exe) {
		return nil
	}
	if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink == 0 {
		fmt.Fprintf(out, "warning: %s exists and is not a symlink; leaving it unchanged\n", link)
		return nil
	}
	if dryRun {
		fmt.Fprintf(out, "[dry-run] symlink %s -> %s\n", link, exe)
		return nil
	}
	_ = os.Remove(link)
	if err := os.Symlink(exe, link); err != nil {
		return fmt.Errorf("link %s: %w", link, err)
	}
	fmt.Fprintf(out, "Linked %s -> %s\n", link, exe)
	return nil
}

// unlinkCommand removes dir/cc-remote only when it is a symlink.
func unlinkCommand(dir string, dryRun bool, out io.Writer) {
	link := commandLink(dir)
	if link == "" {
		return
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return
	}
	if dryRun {
		fmt.Fprintf(out, "[dry-run] remove %s\n", link)
		return
	}
	if err := os.Remove(link); err == nil {
		fmt.Fprintf(out, "Removed %s\n", link)
	}
}

func uninstallAlias(opts aliasOptions, out io.Writer) error {
	path, err := aliasPath(opts.dir)
	if err != nil {
		return err
	}
	if opts.dryRun {
		fmt.Fprintf(out, "[dry-run] remove %s\n", path)
	} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	} else {
		fmt.Fprintf(out, "Removed %s\n", path)
	}
	unlinkCommand(filepath.Dir(path), opts.dryRun, out)
	return removeFromPath(filepath.Dir(path), opts.dryRun, out)
}

func aliasStatus(opts aliasOptions, out io.Writer) error {
	path, err := aliasPath(opts.dir)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(out, "ccr: not installed (%s)\n", path)
	} else {
		fmt.Fprintf(out, "ccr: %s\n%s", path, strings.TrimLeft(string(b), "\ufeff"))
	}
	dir := filepath.Dir(path)
	if link := commandLink(dir); link != "" {
		if target, err := os.Readlink(link); err == nil {
			fmt.Fprintf(out, "cc-remote: %s -> %s\n", link, target)
		} else {
			fmt.Fprintf(out, "cc-remote: not linked (%s)\n", link)
		}
	}
	fmt.Fprintf(out, "Directory on PATH (this shell): %s\n", yesNo(PathContains(os.Getenv("PATH"), dir, string(os.PathListSeparator))))
	pathStatus(dir, out)
	return nil
}

// normalizePathEntry expands environment references (%VAR% in the Windows
// REG_EXPAND_SZ user PATH, $VAR elsewhere) so "%LOCALAPPDATA%/cc-remote/bin"
// and its expanded form compare equal; Windows entries compare case-insensitively.
func normalizePathEntry(s string, sep string) string {
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), `"`))
	if sep == ";" {
		s = expandPercentEnv(s)
	} else {
		s = os.ExpandEnv(s)
	}
	if sep == ";" {
		s = strings.ToLower(strings.TrimRight(s, `\/`))
	} else if s != "/" {
		s = strings.TrimRight(s, "/")
	}
	return s
}

// expandPercentEnv replaces %NAME% with the variable's value, leaving unknown
// references untouched as cmd.exe and ExpandEnvironmentStrings do.
func expandPercentEnv(s string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '%')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			break
		}
		name := s[i+1 : i+1+j]
		if v, ok := os.LookupEnv(name); ok && name != "" {
			b.WriteString(s[:i] + v)
			s = s[i+2+j:]
			continue
		}
		b.WriteString(s[:i+1+j])
		s = s[i+1+j:]
	}
	return b.String() + s
}

// PathContains reports whether dir is an entry of the PATH-style list.
func PathContains(list, dir, sep string) bool {
	want := normalizePathEntry(dir, sep)
	for _, e := range strings.Split(list, sep) {
		if normalizePathEntry(e, sep) == want && want != "" {
			return true
		}
	}
	return false
}

// MergePath appends dir to a PATH-style list unless already present.
func MergePath(list, dir, sep string) (string, bool) {
	if PathContains(list, dir, sep) {
		return list, false
	}
	list = strings.TrimRight(list, sep)
	if list == "" {
		return dir, true
	}
	return list + sep + dir, true
}

// RemovePath removes every entry equal to dir, preserving the others.
func RemovePath(list, dir, sep string) (string, bool) {
	want := normalizePathEntry(dir, sep)
	var kept []string
	changed := false
	for _, e := range strings.Split(list, sep) {
		if normalizePathEntry(e, sep) == want {
			changed = true
			continue
		}
		if e != "" {
			kept = append(kept, e)
		}
	}
	if !changed {
		return list, false
	}
	return strings.Join(kept, sep), true
}

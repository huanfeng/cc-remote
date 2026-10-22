//go:build darwin

package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cc-remote/internal/i18n"
)

const managerName = "launchd"

func plistPath() string {
	return filepath.Join(homeDir("."), "Library", "LaunchAgents", LaunchdLabel+".plist")
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// installedPath is the PATH of the installed LaunchAgent, or "".
func installedPath() string {
	b, err := os.ReadFile(plistPath())
	if err != nil {
		return ""
	}
	return PlistEnv(string(b), "PATH")
}

func renderPlist(cfg *Config) string {
	// Under a service manager the host writes only to host.log (no stderr
	// copy) and routes Go crash output there via debug.SetCrashOutput, so
	// launchd.log only receives what is printed before host.log is opened
	// (bad flags, unwritable log dir) and stays small.
	return LaunchdPlist(cfg.Exe, HostArgs(*cfg, managerName), serviceEnvFor(serviceEnv(managerName), cfg.Env), filepath.Join(cfg.DataDir, "launchd.log"), homeDir(cfg.DataDir))
}

func register(cfg *Config, dryRun bool, out io.Writer) error {
	path, plist := plistPath(), renderPlist(cfg)
	if dryRun {
		plist = renderPlist(redacted(cfg)) // shown, often pasted elsewhere: no --env secrets
		fmt.Fprint(out, i18n.F("[dry-run] write %s:\n%s[dry-run] launchctl bootout %s/%s; launchctl bootstrap %s %s\n", "[dry-run] 写入 %s：\n%s[dry-run] launchctl bootout %s/%s；launchctl bootstrap %s %s\n", path, plist, domain(), LaunchdLabel, domain(), path))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := writeFileAtomic(path, []byte(plist), 0600); err != nil {
		return err
	}
	// Re-bootstrap so launchd reads the new plist; RunAtLoad starts the host.
	_, _ = runQuiet("launchctl", "bootout", domain()+"/"+LaunchdLabel)
	if _, err := runQuiet("launchctl", "bootstrap", domain(), path); err != nil {
		return err
	}
	fmt.Fprint(out, i18n.F("Registered LaunchAgent %s\n", "已注册 LaunchAgent %s\n", path))
	return nil
}

// updateDefinition rewrites the plist without reloading it: a reload would
// stop the running host. The host reads its settings from service.json on
// every start, so the loaded definition only matters for the executable.
func updateDefinition(cfg *Config) error {
	return writeFileAtomic(plistPath(), []byte(renderPlist(cfg)), 0600)
}

func unregister(dryRun bool, out io.Writer) error {
	path := plistPath()
	if dryRun {
		fmt.Fprint(out, i18n.F("[dry-run] launchctl bootout %s/%s; remove %s\n", "[dry-run] launchctl bootout %s/%s；删除 %s\n", domain(), LaunchdLabel, path))
		return nil
	}
	_, _ = runQuiet("launchctl", "bootout", domain()+"/"+LaunchdLabel)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Fprint(out, i18n.F("Removed LaunchAgent %s\n", "已删除 LaunchAgent %s\n", path))
	return nil
}

func loaded() bool {
	_, err := runQuiet("launchctl", "print", domain()+"/"+LaunchdLabel)
	return err == nil
}

func schedulerStart() error {
	if !loaded() {
		if _, err := runQuiet("launchctl", "bootstrap", domain(), plistPath()); err != nil {
			return err
		}
	}
	_, err := runQuiet("launchctl", "kickstart", domain()+"/"+LaunchdLabel)
	return err
}

// schedulerStop unloads the agent so KeepAlive cannot restart a killed host.
// After a graceful exit (code 0) KeepAlive{SuccessfulExit=false} leaves it stopped.
func schedulerStop() error {
	if !loaded() {
		return nil
	}
	_, err := runQuiet("launchctl", "bootout", domain()+"/"+LaunchdLabel)
	if err != nil && !loaded() {
		return nil // already gone (e.g. exited and unloaded concurrently)
	}
	return err
}

func schedulerState() schedState {
	if _, err := os.Stat(plistPath()); err != nil {
		return schedState{}
	}
	out, err := runQuiet("launchctl", "print", domain()+"/"+LaunchdLabel)
	if err != nil {
		return schedState{Installed: true, Detail: "not loaded"}
	}
	detail := "loaded"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "state = ") || strings.HasPrefix(line, "pid = ") || strings.HasPrefix(line, "last exit code = ") {
			detail += " " + strings.ReplaceAll(line, " = ", "=")
		}
	}
	return schedState{Installed: true, Enabled: true, Detail: detail}
}

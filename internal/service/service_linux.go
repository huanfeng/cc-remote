//go:build linux

package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cc-remote/internal/i18n"
)

const (
	managerName = "systemd"
	unitName    = "cc-remote.service"
)

func unitPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" || !filepath.IsAbs(dir) {
		dir = filepath.Join(homeDir("."), ".config")
	}
	return filepath.Join(dir, "systemd", "user", unitName)
}

// installedPath is the PATH of the installed unit, or "".
func installedPath() string {
	b, err := os.ReadFile(unitPath())
	if err != nil {
		return ""
	}
	return UnitEnv(string(b), "PATH")
}

func renderUnit(cfg *Config) string {
	unit := SystemdUnit(cfg.Exe, HostArgs(*cfg, managerName), serviceEnvFor(serviceEnv(managerName), cfg.Env))
	return strings.Replace(unit, "Type=simple\n", "Type=simple\nWorkingDirectory="+SystemdPath(homeDir(cfg.DataDir))+"\n", 1)
}

func register(cfg *Config, dryRun bool, out io.Writer) error {
	path, unit := unitPath(), renderUnit(cfg)
	if dryRun {
		fmt.Fprint(out, i18n.F("[dry-run] write %s:\n%s[dry-run] systemctl --user daemon-reload && systemctl --user enable %s\n", "[dry-run] 写入 %s：\n%s[dry-run] systemctl --user daemon-reload && systemctl --user enable %s\n", path, unit, unitName))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := writeFileAtomic(path, []byte(unit), 0600); err != nil {
		return err
	}
	if _, err := runQuiet("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if _, err := runQuiet("systemctl", "--user", "enable", unitName); err != nil {
		return err
	}
	fmt.Fprint(out, i18n.F("Registered systemd user unit %s\n", "已注册 systemd 用户单元 %s\n", path))
	fmt.Fprint(out, i18n.F("Tip: to start at boot without logging in, run: loginctl enable-linger %s\n", "提示：要在开机后无需登录就启动，请运行：loginctl enable-linger %s\n", os.Getenv("USER")))
	return nil
}

// updateDefinition rewrites the unit for the next start without touching the
// running host (daemon-reload does not restart it).
func updateDefinition(cfg *Config) error {
	if err := writeFileAtomic(unitPath(), []byte(renderUnit(cfg)), 0600); err != nil {
		return err
	}
	_, err := runQuiet("systemctl", "--user", "daemon-reload")
	return err
}

func unregister(dryRun bool, out io.Writer) error {
	path := unitPath()
	if dryRun {
		fmt.Fprint(out, i18n.F("[dry-run] systemctl --user disable --now %s; remove %s; systemctl --user daemon-reload\n", "[dry-run] systemctl --user disable --now %s；删除 %s；systemctl --user daemon-reload\n", unitName, path))
		return nil
	}
	_, _ = runQuiet("systemctl", "--user", "disable", "--now", unitName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, _ = runQuiet("systemctl", "--user", "daemon-reload")
	fmt.Fprint(out, i18n.F("Removed systemd user unit %s\n", "已删除 systemd 用户单元 %s\n", path))
	return nil
}

func schedulerStart() error {
	_, err := runQuiet("systemctl", "--user", "start", unitName)
	return err
}

// schedulerStop is idempotent: stopping an inactive or unloaded unit is not
// an error once is-active confirms nothing runs.
func schedulerStop() error {
	_, err := runQuiet("systemctl", "--user", "stop", unitName)
	if err != nil {
		if active, _ := runQuiet("systemctl", "--user", "is-active", unitName); active != "active" && active != "activating" && active != "deactivating" {
			return nil
		}
	}
	return err
}

func schedulerState() schedState {
	if _, err := os.Stat(unitPath()); err != nil {
		return schedState{}
	}
	enabled, _ := runQuiet("systemctl", "--user", "is-enabled", unitName)
	active, _ := runQuiet("systemctl", "--user", "is-active", unitName)
	return schedState{Installed: true, Enabled: enabled == "enabled", Detail: "is-enabled=" + enabled + " is-active=" + active}
}

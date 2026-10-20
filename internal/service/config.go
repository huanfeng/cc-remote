package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"cc-remote/internal/config"
	"cc-remote/internal/i18n"
)

// Config is persisted as <dataDir>/service.json (and mirrored in InstallRoot
// so commands find a non-default data directory).
type Config struct {
	Listen          []string `json:"listen"`
	Claude          string   `json:"claude"`
	ClaudeConfigDir string   `json:"claudeConfigDir,omitempty"`
	// Language of the host's messages and web UI: "en", "zh" or empty (the system language).
	Language string `json:"language,omitempty"`
	// AllowInsecureRemote permits all:/0.0.0.0/:: binds (--allow-insecure-remote).
	AllowInsecureRemote bool `json:"allowInsecureRemote,omitempty"`
	// AllowOrigins are standalone web frontends allowed to use the host
	// (--allow-origin, normalized), besides CC_REMOTE_ALLOW_ORIGINS.
	AllowOrigins []string `json:"allowOrigins,omitempty"`
	// Env is added to the host's environment (proxy variables captured at
	// install, plus --env). It stays non-nil once configured so a reinstall
	// keeps it instead of capturing again (hence no omitempty: "{}" means
	// configured but empty); values may be secrets (the file is 0600).
	Env map[string]string `json:"env"`
	// EnvRemoved names variables removed with --env KEY=; the host unsets
	// them, since an older service definition may still pass them on.
	EnvRemoved []string `json:"envRemoved,omitempty"`
	// AutoLanguage is the language "auto" stood for when the settings were
	// last saved (a service manager starts the host without the user's LANG).
	AutoLanguage string    `json:"autoLanguage,omitempty"`
	DataDir      string    `json:"dataDir"`
	Exe          string    `json:"exe"`
	LogFile      string    `json:"logFile"`
	InstalledAt  time.Time `json:"installedAt"`
	Version      string    `json:"version"`
}

const configName = "service.json"

// InstallRoot holds the stable executable copy and the config mirror. It
// never falls back to a temporary directory: an executable or config placed
// there could be replaced by another user or wiped on reboot.
func InstallRoot() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if dir := os.Getenv("LOCALAPPDATA"); dir != "" && filepath.IsAbs(dir) {
			return filepath.Join(dir, "cc-remote"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", i18n.Errorf("cannot determine the install directory (LOCALAPPDATA and the home directory are unset): %w", "无法确定安装目录（LOCALAPPDATA 和用户主目录都未设置）：%w", err)
		}
		return filepath.Join(home, "AppData", "Local", "cc-remote"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", i18n.Errorf("cannot determine the install directory (HOME is unset): %w", "无法确定安装目录（HOME 未设置）：%w", err)
		}
		return filepath.Join(home, "Library", "Application Support", "cc-remote"), nil
	}
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" && filepath.IsAbs(dir) {
		return filepath.Join(dir, "cc-remote"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", i18n.Errorf("cannot determine the install directory (XDG_DATA_HOME and HOME are unset): %w", "无法确定安装目录（XDG_DATA_HOME 和 HOME 都未设置）：%w", err)
	}
	return filepath.Join(home, ".local", "share", "cc-remote"), nil
}

// BinDir is where install copies the executable.
func BinDir() (string, error) {
	root, err := InstallRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin"), nil
}

// InstalledExe is the stable executable path used by the service.
func InstalledExe() (string, error) {
	dir, err := BinDir()
	if err != nil {
		return "", err
	}
	name := "cc-remote"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name), nil
}

func readConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err = json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadConfig finds service.json: an explicit data dir wins, then the mirror in
// InstallRoot, then the default data dir. It returns nil, nil when none exists.
func LoadConfig(dataDir string) (*Config, error) {
	var candidates []string
	if dataDir != "" {
		candidates = []string{filepath.Join(dataDir, configName)}
	} else {
		if root, err := InstallRoot(); err == nil {
			candidates = append(candidates, filepath.Join(root, configName))
		}
		candidates = append(candidates, filepath.Join(config.DefaultDataDir(), configName))
	}
	for _, path := range candidates {
		cfg, err := readConfig(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		return cfg, err
	}
	return nil, nil
}

// SaveConfig writes service.json into the data dir and the install root.
func SaveConfig(cfg *Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	root, err := InstallRoot()
	if err != nil {
		return err
	}
	for _, dir := range []string{cfg.DataDir, root} {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err = writeFileAtomic(filepath.Join(dir, configName), b, 0600); err != nil {
			return err
		}
	}
	return nil
}

func removeConfigMirror() {
	if root, err := InstallRoot(); err == nil {
		_ = os.Remove(filepath.Join(root, configName))
	}
}

func writeFileAtomic(path string, b []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(b)
	if e := tmp.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), mode)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// copyExecutable copies src to dst via a temporary file. A running (locked)
// destination on Windows is renamed aside first, which Windows allows.
func copyExecutable(src, dst string, patch func([]byte) error) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if patch != nil {
		if err = patch(b); err != nil {
			return err
		}
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	old := AsidePath(dst)
	tmp := dst + ".new"
	if err = os.WriteFile(tmp, b, 0755); err != nil {
		return err
	}
	if err = os.Rename(tmp, dst); err == nil {
		return nil
	}
	if e := os.Rename(dst, old); e != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err = os.Rename(tmp, dst); err != nil {
		_ = os.Rename(old, dst)
		_ = os.Remove(tmp)
	}
	return err
}

// AsidePath returns where a running executable at path can be moved before it
// is replaced: path+".old" after removing a stale copy, or path+".old-<time>"
// when that stale copy is still locked (e.g. a Windows process still runs it).
func AsidePath(path string) string {
	old := path + ".old"
	if err := os.Remove(old); err == nil || errors.Is(err, os.ErrNotExist) {
		return old
	}
	return fmt.Sprintf("%s.old-%d", path, time.Now().UnixNano())
}

// CleanupAside removes path.old and path.old-* left behind by earlier
// replacements; files that are still locked are kept for the next run.
func CleanupAside(path string) {
	if path == "" || strings.Contains(filepath.Base(path), ".old") {
		return
	}
	matches, _ := filepath.Glob(globEscape(path) + ".old*")
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

func globEscape(s string) string {
	if runtime.GOOS == "windows" {
		// Backslash is the path separator, not an escape, on Windows.
		return strings.NewReplacer("*", "[*]", "?", "[?]", "[", "[[]").Replace(s)
	}
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`).Replace(s)
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

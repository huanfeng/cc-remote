package service

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"cc-remote/internal/i18n"
)

// isolateInstallRoot points InstallRoot and the default data dir at temp dirs.
func isolateInstallRoot(t *testing.T) (root, defaultData string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)  // windows
	t.Setenv("XDG_DATA_HOME", base) // linux
	t.Setenv("HOME", base)          // darwin
	defaultData = filepath.Join(base, "default-data")
	t.Setenv("CC_REMOTE_DATA_DIR", defaultData)
	root, err := InstallRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root, defaultData
}

func TestExistingInstallReusesMirroredConfig(t *testing.T) {
	_, defaultData := isolateInstallRoot(t)

	// Fresh machine: default data dir, no previous config.
	cfg, dir, err := existingInstall("")
	if err != nil || cfg != nil || dir != defaultData {
		t.Fatalf("fresh: cfg=%v dir=%q err=%v", cfg, dir, err)
	}

	custom := filepath.Join(t.TempDir(), "custom-data")
	saved := &Config{DataDir: custom, Listen: []string{"lan:9000"}, Claude: "/opt/claude", ClaudeConfigDir: "/cfg"}
	if err := SaveConfig(saved); err != nil {
		t.Fatal(err)
	}

	// No --data-dir: the mirror under InstallRoot names the custom data dir.
	cfg, dir, err = existingInstall("")
	if err != nil || cfg == nil || dir != custom {
		t.Fatalf("mirror: cfg=%v dir=%q err=%v", cfg, dir, err)
	}
	if !reflect.DeepEqual(cfg.Listen, saved.Listen) || cfg.Claude != saved.Claude || cfg.ClaudeConfigDir != saved.ClaudeConfigDir {
		t.Fatalf("settings not reused: %+v", cfg)
	}

	// Unset flags keep the saved settings; explicit flags override.
	keep := *cfg
	if err := applySettings(&keep, map[string]bool{}, nil, "claude", "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(keep.Listen, saved.Listen) || keep.Claude != saved.Claude || keep.ClaudeConfigDir != saved.ClaudeConfigDir {
		t.Fatalf("unset flags changed settings: %+v", keep)
	}
	override := *cfg
	if err := applySettings(&override, map[string]bool{"listen": true}, []string{"127.0.0.1:7000"}, "claude", "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(override.Listen, []string{"127.0.0.1:7000"}) || override.Claude != saved.Claude {
		t.Fatalf("override: %+v", override)
	}

	// An explicit --data-dir wins over the mirror.
	other := filepath.Join(t.TempDir(), "other")
	cfg, dir, err = existingInstall(other)
	if err != nil || cfg != nil || dir != other {
		t.Fatalf("explicit: cfg=%v dir=%q err=%v", cfg, dir, err)
	}

	// start/stop/status/config resolve the same installation without flags.
	if cfg, err := mustConfig(""); err != nil || cfg.DataDir != custom {
		t.Fatalf("mustConfig: %v %v", cfg, err)
	}
}

func TestInsecureListenNeedsOptIn(t *testing.T) {
	for _, spec := range []string{"all:8787", "0.0.0.0:8787", "[::]:8787", ":8787"} {
		cfg := &Config{}
		err := applySettings(cfg, map[string]bool{"listen": true}, []string{spec}, "claude", "", false, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "--allow-insecure-remote") {
			t.Errorf("%s accepted without opt-in: %v", spec, err)
		}
	}
	cfg := &Config{}
	set := map[string]bool{"listen": true, "allow-insecure-remote": true}
	if err := applySettings(cfg, set, []string{"all:8787"}, "claude", "", true, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !cfg.AllowInsecureRemote || !slices.Contains(HostArgs(*cfg, "systemd"), "--allow-insecure-remote") {
		t.Fatalf("opt-in not persisted: %+v", cfg)
	}
	// Revoking the opt-in while a wildcard is configured is refused.
	if err := applySettings(cfg, map[string]bool{"allow-insecure-remote": true}, nil, "claude", "", false, io.Discard); err == nil {
		t.Fatal("revoked opt-in kept a wildcard listen address")
	}
	// lan: and specific private addresses only warn.
	cfg = &Config{}
	if err := applySettings(cfg, map[string]bool{"listen": true}, []string{"lan:8787,192.168.1.2:8787"}, "claude", "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(HostArgs(*cfg, "systemd"), "--allow-insecure-remote") {
		t.Fatal("unexpected --allow-insecure-remote")
	}
}

func TestApplyLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.Set(i18n.EN) }) // applyLanguage switches the process language
	set := map[string]bool{"lang": true}
	cfg := &Config{Listen: []string{"127.0.0.1:8787"}, LogFile: "host.log"}
	if args := HostArgs(*cfg, "systemd"); !slices.Contains(args, "--lang") || args[slices.Index(args, "--lang")+1] != "en" {
		t.Fatalf("auto not resolved to the installing command's language: %v", args)
	}
	for _, c := range []struct{ value, want string }{{"zh", "zh"}, {"zh-CN", "zh"}, {"en", "en"}, {"auto", ""}, {"", ""}} {
		cfg.Language = "en"
		if c.value == "" {
			cfg.Language = "zh"
		}
		if err := applyLanguage(cfg, set, c.value); err != nil {
			t.Fatalf("%q: %v", c.value, err)
		}
		if cfg.Language != c.want {
			t.Fatalf("%q stored %q, want %q", c.value, cfg.Language, c.want)
		}
		want := c.want
		if want == "" {
			want = string(i18n.Current()) // auto: the language this command resolved
		}
		args := HostArgs(*cfg, "systemd")
		i := slices.Index(args, "--lang")
		if i < 0 || args[i+1] != want {
			t.Fatalf("%q: host args %v", c.value, args)
		}
	}
	cfg.Language = "zh"
	if err := applyLanguage(cfg, set, "fr"); err == nil || cfg.Language != "zh" {
		t.Fatalf("invalid language accepted: %v, stored %q", err, cfg.Language)
	}
	if err := applyLanguage(cfg, map[string]bool{}, "en"); err != nil || cfg.Language != "zh" {
		t.Fatalf("unset --lang changed the setting: %v, %q", err, cfg.Language)
	}
}

func TestPathEntriesExpandEnv(t *testing.T) {
	t.Setenv("CCR_TEST_BASE", `C:\Users\Me\AppData\Local`)
	list := `%CCR_TEST_BASE%\cc-remote\bin;C:\Tools`
	dir := `c:\users\me\appdata\local\CC-REMOTE\bin`
	if !PathContains(list, dir, ";") {
		t.Fatal("expanded entry not recognized")
	}
	if merged, changed := MergePath(list, dir, ";"); changed {
		t.Fatalf("duplicate added: %q", merged)
	}
	if removed, changed := RemovePath(list, dir, ";"); !changed || removed != `C:\Tools` {
		t.Fatalf("remove = %q %v", removed, changed)
	}
	if !PathContains(`%NO_SUCH_VAR_CCR%\bin`, `%no_such_var_ccr%\bin`, ";") {
		t.Fatal("unknown variables must stay literal")
	}
	t.Setenv("CCR_TEST_HOME", "/home/me")
	if !PathContains("/usr/bin:$CCR_TEST_HOME/.local/bin", "/home/me/.local/bin", ":") {
		t.Fatal("unix $VAR entry not expanded")
	}
}

func TestAsidePathFallsBackWhenOldIsLocked(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "cc-remote")
	if got := AsidePath(exe); got != exe+".old" {
		t.Fatalf("got %q", got)
	}
	// A non-empty directory cannot be removed by os.Remove, like a locked file.
	if err := os.MkdirAll(filepath.Join(exe+".old", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := AsidePath(exe); !strings.HasPrefix(got, exe+".old-") {
		t.Fatalf("got %q", got)
	}
}

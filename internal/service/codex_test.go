package service

import (
	"bytes"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestCodexFlags(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "Codex tools", "codex")
	t.Setenv("CC_REMOTE_CODEX", bin)
	parse := func(args ...string) (codexFlags, map[string]bool) {
		t.Helper()
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		f := addCodexFlags(fs)
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		return f, set
	}
	f, set := parse()
	cfg := &Config{}
	if err := f.apply(cfg, set, io.Discard); err != nil || cfg.Codex != bin || cfg.CodexHome != "" {
		t.Fatalf("environment default: %+v, %v", cfg, err)
	}
	saved := Config{Codex: "saved-codex", CodexHome: "saved-home"}
	cfg = &saved
	if err := f.apply(cfg, set, io.Discard); err != nil || cfg.Codex != "saved-codex" || cfg.CodexHome != "saved-home" {
		t.Fatalf("unset flags: %+v, %v", cfg, err)
	}
	f, set = parse("--codex", bin, "--codex-home", "relative-home")
	wantHome, err := filepath.Abs("relative-home")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.apply(cfg, set, io.Discard); err != nil || cfg.Codex != bin || cfg.CodexHome != wantHome {
		t.Fatalf("explicit flags: %+v, %v", cfg, err)
	}
	f, set = parse("--codex-home=")
	if err := f.apply(cfg, set, io.Discard); err != nil || cfg.CodexHome != "" || cfg.Codex != bin {
		t.Fatalf("clear home: %+v, %v", cfg, err)
	}
	t.Setenv("CC_REMOTE_CODEX", "")
	f, _ = parse()
	if *f.executable != "codex" {
		t.Fatalf("default executable: %q", *f.executable)
	}
}

func TestCodexConfigPersistenceAndRendering(t *testing.T) {
	isolateInstallRoot(t)
	dir := t.TempDir()
	old := []byte(`{"dataDir":"unused","claude":"claude","env":{}}`)
	if err := os.WriteFile(filepath.Join(dir, configName), old, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil || cfg == nil {
		t.Fatalf("old config: %v", err)
	}
	if cfg.Codex != "" || cfg.CodexHome != "" || slices.Contains(HostArgs(*cfg, "systemd"), "--codex-home") || slices.Contains(HostArgs(*cfg, "systemd"), "--codex") {
		t.Fatalf("old config no longer inherits host defaults: %+v", cfg)
	}
	cfg.DataDir = dir
	cfg.Codex = filepath.Join(dir, "Codex tools", "codex")
	cfg.CodexHome = filepath.Join(dir, "Codex home & config")
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil || !reflect.DeepEqual(cfg, loaded) {
		t.Fatalf("round trip: %+v, %v", loaded, err)
	}
	for _, manager := range []string{"systemd", "launchd", "schtasks"} {
		args := HostArgs(*loaded, manager)
		for name, want := range map[string]string{"--codex": cfg.Codex, "--codex-home": cfg.CodexHome} {
			i := slices.Index(args, name)
			if i < 0 || i+1 >= len(args) || args[i+1] != want {
				t.Fatalf("%s args: %v", manager, args)
			}
		}
	}
	args := HostArgs(*loaded, "systemd")
	if unit := SystemdUnit("cc-remote", args, nil); !strings.Contains(unit, SystemdQuote(cfg.Codex, true)) || !strings.Contains(unit, SystemdQuote(cfg.CodexHome, true)) {
		t.Fatalf("systemd: %s", unit)
	}
	if plist := LaunchdPlist("cc-remote", args, nil, "log", dir); !strings.Contains(plist, "<string>"+xmlText(cfg.CodexHome)+"</string>") {
		t.Fatalf("launchd: %s", plist)
	}
	if line := WindowsCommandLine(args); !strings.Contains(line, WindowsQuote(cfg.Codex)) || !strings.Contains(line, WindowsQuote(cfg.CodexHome)) {
		t.Fatalf("windows: %s", line)
	}
}

func TestConfigCodexReload(t *testing.T) {
	isolateInstallRoot(t)
	t.Setenv("CC_REMOTE_TOKEN", "")
	reloads, restarts := 0, 0
	cfg := fakeHost(t, func(w http.ResponseWriter) {
		reloads++
		w.Write([]byte(`{"listen":["127.0.0.1:8787"],"restart":["codex","codex-home"]}`))
	}, &restarts)
	cfg.Claude, cfg.Env = "claude", map[string]string{}
	cfg.Exe = filepath.Join(t.TempDir(), "cc-remote")
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	bin, home := filepath.Join(t.TempDir(), "codex"), filepath.Join(t.TempDir(), "codex-home")
	if err := configCmd([]string{"--data-dir", cfg.DataDir, "--codex", bin, "--codex-home", home}); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadConfig(cfg.DataDir)
	if err != nil || saved == nil || saved.Codex != bin || saved.CodexHome != home || saved.Claude != "claude" || reloads != 1 || restarts != 1 {
		t.Fatalf("saved %+v, err %v, reloads %d restarts %d", saved, err, reloads, restarts)
	}
	var out bytes.Buffer
	printConfig(saved, &out)
	if !strings.Contains(out.String(), bin) || !strings.Contains(out.String(), home) {
		t.Fatalf("printed config: %s", &out)
	}
	if err := configCmd([]string{"--data-dir", cfg.DataDir, "--codex-home="}); err != nil {
		t.Fatal(err)
	}
	saved, err = LoadConfig(cfg.DataDir)
	if err != nil || saved == nil || saved.CodexHome != "" || saved.Codex != bin || reloads != 2 {
		t.Fatalf("clear saved %+v, err %v, reloads %d", saved, err, reloads)
	}
}

func TestInstallCodexDryRun(t *testing.T) {
	isolateInstallRoot(t)
	dir := t.TempDir()
	if err := installCmd([]string{"--dry-run", "--no-start", "--data-dir", dir, "--codex", filepath.Join(dir, "codex"), "--codex-home", filepath.Join(dir, "home")}, "test"); err != nil {
		t.Fatal(err)
	}
	if cfg, err := LoadConfig(dir); err != nil || cfg != nil {
		t.Fatalf("dry run saved config: %+v, %v", cfg, err)
	}
}

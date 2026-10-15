package service

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCaptureProxyEnv(t *testing.T) {
	environ := []string{
		"=C:=C:\\work",
		"PATH=/usr/bin",
		"http_proxy=http://127.0.0.1:7890",
		"HTTP_PROXY=http://other:1",
		"https_proxy=",
		"HTTPS_PROXY=   ",
		"All_Proxy=socks5://127.0.0.1:7890",
		"NO_PROXY=localhost,127.0.0.1",
		"ANTHROPIC_BASE_URL=https://example.invalid",
		"ANTHROPIC_API_KEY=sk-secret",
	}
	// Unix: names are case-sensitive, both spellings kept; empty values skipped.
	got := captureProxyEnv(environ, false)
	want := map[string]string{
		"http_proxy": "http://127.0.0.1:7890",
		"HTTP_PROXY": "http://other:1",
		"All_Proxy":  "socks5://127.0.0.1:7890",
		"NO_PROXY":   "localhost,127.0.0.1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unix capture = %v", got)
	}
	// Windows: one entry per case-insensitive name, keeping the first spelling.
	got = captureProxyEnv(environ, true)
	delete(want, "HTTP_PROXY")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("windows capture = %v", got)
	}
}

func TestParseEnvFlag(t *testing.T) {
	if k, v, err := parseEnvFlag("HTTPS_PROXY=http://h:1/?a=b"); err != nil || k != "HTTPS_PROXY" || v != "http://h:1/?a=b" {
		t.Fatalf("got %q %q %v", k, v, err)
	}
	if k, v, err := parseEnvFlag("FOO="); err != nil || k != "FOO" || v != "" {
		t.Fatalf("removal: %q %q %v", k, v, err)
	}
	for _, bad := range []string{"FOO", "=x", "A B=1", "cc_remote_service_manager=x"} {
		if _, _, err := parseEnvFlag(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestApplyEnv(t *testing.T) {
	cfg := &Config{}
	var out bytes.Buffer
	environ := []string{"https_proxy=http://u:pw@127.0.0.1:7890", "no_proxy=localhost"}
	if err := applyEnv(cfg, true, environ, []string{"MY_TOKEN=abc", "OTHER=1"}, true, &out); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"https_proxy": "http://u:pw@127.0.0.1:7890", "no_proxy": "localhost", "MY_TOKEN": "abc", "OTHER": "1"}
	if !reflect.DeepEqual(cfg.Env, want) {
		t.Fatalf("env = %v", cfg.Env)
	}
	s := out.String()
	if strings.Contains(s, "abc") || strings.Contains(s, "pw@") || !strings.Contains(s, "MY_TOKEN=****") || !strings.Contains(s, "https_proxy=http://u:xxxxx@127.0.0.1:7890") {
		t.Fatalf("output leaks or misses values:\n%s", s)
	}
	// Windows: --env replaces any spelling; KEY= removes it.
	if err := applyEnv(cfg, false, nil, []string{"HTTPS_PROXY=http://new:1", "No_Proxy=", "OTHER="}, true, io.Discard); err != nil {
		t.Fatal(err)
	}
	want = map[string]string{"HTTPS_PROXY": "http://new:1", "MY_TOKEN": "abc"}
	if !reflect.DeepEqual(cfg.Env, want) {
		t.Fatalf("after update env = %v", cfg.Env)
	}
	// Unix: names are case-sensitive.
	if err := applyEnv(cfg, false, nil, []string{"https_proxy=http://lower:1"}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Env) != 3 {
		t.Fatalf("unix env = %v", cfg.Env)
	}
	// Nothing captured still marks the environment as configured.
	empty := &Config{}
	if err := applyEnv(empty, true, []string{"PATH=/bin"}, nil, false, io.Discard); err != nil || empty.Env == nil {
		t.Fatalf("env = %v err = %v", empty.Env, err)
	}
	if err := applyEnv(empty, false, nil, []string{"bad"}, false, io.Discard); err == nil {
		t.Fatal("invalid --env accepted")
	}
}

func TestEnvFlags(t *testing.T) {
	parse := func(args ...string) (*envFlags, map[string]bool) {
		fs := flag.NewFlagSet("x", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		e := addEnvFlags(fs, "")
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		return e, set
	}
	e, set := parse("--env", "A=1", "--env", "B=")
	if !reflect.DeepEqual([]string(e.vals), []string{"A=1", "B="}) {
		t.Fatalf("vals = %v", e.vals)
	}
	for _, c := range []struct {
		args []string
		def  bool
		want bool
	}{
		{nil, true, true}, {nil, false, false},
		{[]string{"--no-capture-env"}, true, false},
		{[]string{"--capture-env"}, false, true},
		{[]string{"--capture-env=false"}, true, false},
	} {
		e, set = parse(c.args...)
		if got, err := e.shouldCapture(set, c.def); err != nil || got != c.want {
			t.Errorf("%v def=%v: got %v %v", c.args, c.def, got, err)
		}
	}
	e, set = parse("--capture-env", "--no-capture-env")
	if _, err := e.shouldCapture(set, true); err == nil {
		t.Fatal("conflicting flags accepted")
	}
}

func TestEnvSurvivesReinstall(t *testing.T) {
	_, defaultData := isolateInstallRoot(t)
	// A config from before env support has no "env" key: install captures.
	old := filepath.Join(defaultData, configName)
	if err := os.MkdirAll(defaultData, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte(`{"listen":["127.0.0.1:8787"],"dataDir":"`+filepath.ToSlash(defaultData)+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := existingInstall("")
	if err != nil || cfg == nil || cfg.Env != nil {
		t.Fatalf("old config: %+v %v", cfg, err)
	}
	// Configured env (even empty) round-trips and is kept on reinstall.
	cfg.DataDir = defaultData
	cfg.Env = map[string]string{}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	again, _, err := existingInstall("")
	if err != nil || again.Env == nil || len(again.Env) != 0 {
		t.Fatalf("empty env lost: %+v %v", again, err)
	}
	cfg.Env = map[string]string{"HTTPS_PROXY": "http://127.0.0.1:7890"}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	again, _, _ = existingInstall("")
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	e := addEnvFlags(fs, "")
	if capture, _ := e.shouldCapture(map[string]bool{}, again.Env == nil); capture {
		t.Fatal("reinstall would recapture over the configured env")
	}
	if err := applyEnv(again, false, []string{"HTTPS_PROXY=http://changed:1"}, nil, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if again.Env["HTTPS_PROXY"] != "http://127.0.0.1:7890" {
		t.Fatalf("env changed without flags: %v", again.Env)
	}
}

func TestRedactedConfig(t *testing.T) {
	cfg := &Config{Env: map[string]string{"HTTPS_PROXY": "http://127.0.0.1:7890", "API_KEY": "sk-1"}}
	var got Config
	if err := json.Unmarshal(redactedJSON(cfg), &got); err != nil {
		t.Fatal(err)
	}
	if got.Env["API_KEY"] != "****" || got.Env["HTTPS_PROXY"] != "http://127.0.0.1:7890" || cfg.Env["API_KEY"] != "sk-1" {
		t.Fatalf("redacted = %v original = %v", got.Env, cfg.Env)
	}
	var out bytes.Buffer
	printConfig(cfg, &out)
	if !strings.Contains(out.String(), "Env:           API_KEY=**** HTTPS_PROXY=http://127.0.0.1:7890\n") {
		t.Fatalf("printConfig:\n%s", out.String())
	}
}

func TestMergeEnviron(t *testing.T) {
	base := []string{"=C:=C:\\", "Path=C:\\Windows", "http_proxy=old", "HOME=/h"}
	got := mergeEnviron(base, map[string]string{"HTTP_PROXY": "http://new:1", "NO_PROXY": "localhost"}, true)
	want := []string{"=C:=C:\\", "Path=C:\\Windows", "HOME=/h", "HTTP_PROXY=http://new:1", "NO_PROXY=localhost"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("windows merge = %q", got)
	}
	got = mergeEnviron(base, map[string]string{"HTTP_PROXY": "x"}, false)
	if len(got) != len(base)+1 || got[2] != "http_proxy=old" {
		t.Fatalf("unix merge = %q", got)
	}
}

func TestServiceEnvForAndRendering(t *testing.T) {
	base := map[string]string{"CC_REMOTE_SERVICE_MANAGER": "systemd", "PATH": "/usr/bin"}
	env := serviceEnvFor(base, map[string]string{
		"HTTPS_PROXY":               "http://127.0.0.1:7890",
		"WEIRD":                     `50% $HOME "q" a\b`,
		"CC_REMOTE_SERVICE_MANAGER": "nope",
	})
	if env["CC_REMOTE_SERVICE_MANAGER"] != "systemd" || env["PATH"] != "/usr/bin" || base["HTTPS_PROXY"] != "" {
		t.Fatalf("merge = %v base = %v", env, base)
	}
	unit := SystemdUnit("/bin/cc-remote", nil, env)
	for _, want := range []string{
		`Environment="HTTPS_PROXY=http://127.0.0.1:7890"`,
		`Environment="WEIRD=50%% $HOME \"q\" a\\b"`,
		`Environment="CC_REMOTE_SERVICE_MANAGER=systemd"`,
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %s\n%s", want, unit)
		}
	}
	plist := LaunchdPlist("/bin/cc-remote", nil, serviceEnvFor(map[string]string{"CC_REMOTE_SERVICE_MANAGER": "launchd", "PATH": "/usr/bin"}, map[string]string{"NO_PROXY": "a&b<c>", "HTTPS_PROXY": "http://h:1"}), "/l", "/w")
	for _, want := range []string{
		"<key>NO_PROXY</key>\n    <string>a&amp;b&lt;c&gt;</string>",
		"<key>HTTPS_PROXY</key>\n    <string>http://h:1</string>",
		"<key>PATH</key>\n    <string>/usr/bin</string>",
		"<key>CC_REMOTE_SERVICE_MANAGER</key>\n    <string>launchd</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q", want)
		}
	}
}

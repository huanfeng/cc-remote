package main

import (
	"cc-remote/internal/service"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCLIRoute(t *testing.T) {
	for _, tc := range []struct {
		in      []string
		command string
		args    []string
	}{
		{nil, "session", nil},
		{[]string{"--resume", "status"}, "session", []string{"--resume", "status"}},
		{[]string{"--model", "sonnet", "status"}, "session", []string{"--model", "sonnet", "status"}},
		{[]string{"--", "status"}, "session", []string{"--", "status"}},
		{[]string{"--data-dir", "d", "--", "claude", "--resume", "abc"}, "session", []string{"--data-dir", "d", "--", "--resume", "abc"}},
		{[]string{"session", "status"}, "session", []string{"status"}},
		{[]string{"status", "--json"}, "status", []string{"--json"}},
		{[]string{"claude", "--", "--resume", "abc"}, "claude", []string{"--", "--resume", "abc"}},
		{[]string{"ccr", "--host", "http://localhost:8999", "--data-dir", "status", "status", "--json"}, "status", []string{"--data-dir", "status", "--json"}},
		{[]string{"--data-dir", "config", "--resume", "foo"}, "session", []string{"--data-dir", "config", "--resume", "foo"}},
		{[]string{"--listen", "127.0.0.1:8999"}, "serve", []string{"--listen", "127.0.0.1:8999"}},
		{[]string{"--data-dir", "d", "--listen", "127.0.0.1:8999"}, "serve", []string{"--data-dir", "d", "--listen", "127.0.0.1:8999"}},
		{[]string{"--help"}, "help", nil},
		{[]string{"service", "typo"}, "service", []string{"typo"}},
		{[]string{"ccr", "--host", "http://localhost:8999", "--data-dir", "old", "service", "status", "--data-dir", "new"}, "service", []string{"status", "--data-dir", "old", "--data-dir", "new"}},
		{[]string{"ccr", "--host", "http://localhost:8999", "--data-dir", "old", "service"}, "service", nil},
		{[]string{"--data-dir", "d", "oauth", "ls"}, "oauth", []string{"ls", "--data-dir", "d"}},
		{[]string{"--host", "http://localhost:8999", "profile", "default", "work"}, "profile", []string{"default", "--host", "http://localhost:8999", "work"}},
		{[]string{"claude", "--", "--help"}, "claude", []string{"--", "--help"}},
		{[]string{"claude", "--", "-p", "status"}, "claude", []string{"--", "-p", "status"}},
		{[]string{"--host", "http://localhost:8999", "shell", "attach", "ABC"}, "shell", []string{"--host", "http://localhost:8999", "attach", "ABC"}},
		{[]string{"--data-dir", "d", "run", "--", "git", "--help"}, "run", []string{"--data-dir", "d", "--", "git", "--help"}},
		{[]string{"session", "shell"}, "session", []string{"shell"}},
		{[]string{"ccr", "--data-dir", "d", "terminal-hook"}, "terminal-hook", nil},
		{[]string{"permission-mcp"}, "permission-mcp", nil},
		{[]string{"service", "supervise", "--listen", "127.0.0.1:8999", "--service-manager", "schtasks"}, "service", []string{"supervise", "--listen", "127.0.0.1:8999", "--service-manager", "schtasks"}},
	} {
		got := routeCLI(tc.in)
		if got.command != tc.command || !reflect.DeepEqual(got.args, tc.args) {
			t.Errorf("%q: got %+v, want %s %q", tc.in, got, tc.command, tc.args)
		}
	}
}

func TestConnectionContextBoundaries(t *testing.T) {
	base, other := t.TempDir(), t.TempDir()
	t.Setenv("CC_REMOTE_DATA_DIR", base)
	for dir, port := range map[string]string{base: "8999", other: "9000"} {
		b, _ := json.Marshal(service.Config{DataDir: dir, Listen: []string{"127.0.0.1:" + port}})
		if err := os.WriteFile(filepath.Join(dir, "service.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		command string
		args    []string
		port    string
	}{
		{"session", []string{"--append-system-prompt", "--data-dir", other}, "8999"},
		{"session", []string{"--title", "--data-dir", other}, "8999"},
		{"session", []string{"--data-dir", base, "--resume", "abc", "--data-dir", other}, "9000"},
		{"session", []string{"--", "--data-dir", other}, "9000"},
		{"session", []string{"-data-dir=" + other}, "9000"},
		{"claude", []string{"--", "--data-dir", other}, "8999"},
		{"claude", []string{"prompt", "--data-dir", other}, "8999"},
		{"claude", []string{"--profile", "--data-dir", other}, "8999"},
		{"oauth", []string{"add", "--token", "--data-dir", "name"}, "8999"},
		{"profile", []string{"default", "work", "--data-dir", other}, "9000"},
		{"shell", []string{"attach", "--data-dir", other}, "9000"},
		{"run", []string{"--cwd", "--data-dir", "git", "--data-dir", other}, "8999"},
		{"run", []string{"--data-dir", other, "--", "git", "--data-dir", base}, "9000"},
	} {
		got := connectionDefaultsFor(tc.command, tc.args)
		want := append([]string{"--host", "http://127.0.0.1:" + tc.port}, tc.args...)
		if tc.command == "oauth" || tc.command == "profile" {
			want = append([]string{tc.args[0], "--host", "http://127.0.0.1:" + tc.port}, tc.args[1:]...)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s %q: got %q, want %q", tc.command, tc.args, got, want)
		}
	}
}

func TestServiceHostArgsRoute(t *testing.T) {
	for _, manager := range []string{"systemd", "launchd", "schtasks"} {
		args := service.HostArgs(service.Config{DataDir: "data", Listen: []string{"127.0.0.1:8999"}, Claude: "claude"}, manager)
		if got := routeCLI(args); got.command != "serve" {
			t.Errorf("%s: host self-spawn routed to %s", manager, got.command)
		}
	}
}

func TestInternalHookRoutes(t *testing.T) {
	for _, command := range []string{"hook", "permission-mcp", "channel", "channel-session-hook", "channel-ask-hook", "terminal-hook", "terminal-status"} {
		got := routeCLI([]string{"ccr", "--host", "http://localhost:8999", "--data-dir", "data", command})
		if got.command != command || len(got.args) != 0 {
			t.Errorf("internal %s: got %+v", command, got)
		}
	}
	previous := os.Args
	defer func() { os.Args = previous }()
	for _, command := range []string{"channel", "channel-session-hook"} {
		os.Args = []string{"ccr", "ccr", "--host", "http://localhost:8999", "--data-dir", t.TempDir(), command}
		if err := run(); err != nil {
			t.Errorf("retired hook %s: %v", command, err)
		}
	}
}

func TestEquivalentNamesOffline(t *testing.T) {
	t.Setenv("CC_REMOTE_LANG", "en")
	dir := t.TempDir()
	t.Setenv("CC_REMOTE_DATA_DIR", dir)
	b, _ := json.Marshal(service.Config{DataDir: dir, Listen: []string{"127.0.0.1:8999"}})
	if err := os.WriteFile(filepath.Join(dir, "service.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	previousArgs, previousOut := os.Args, os.Stdout
	defer func() { os.Args = previousArgs; os.Stdout = previousOut }()
	for _, args := range [][]string{{"--help"}, {"--version"}, {"serve", "--help"}, {"session", "--help"}, {"service", "--help"}, {"oauth", "--help"}, {"profile", "--help"}, {"profile", "ls", "--help"}, {"oauth", "ls", "--help"}, {"--host", "http://localhost:8999", "--data-dir", dir, "profile", "ls", "--help"}, {"--host", "http://localhost:8999", "--data-dir", dir, "oauth", "ls", "--help"}, {"ccr", "--host", "http://localhost:8999", "--data-dir", "old", "service", "--help"}} {
		var previous string
		for i, name := range []string{"ccr", "cc-remote", "CCR.exe", "cc-remote.exe"} {
			f, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			os.Args = append([]string{name}, args...)
			os.Stdout = f
			err = run()
			os.Stdout = previousOut
			f.Close()
			if err != nil {
				t.Fatalf("%s %v: %v", name, args, err)
			}
			b, err := os.ReadFile(f.Name())
			if err != nil {
				t.Fatal(err)
			}
			if i > 0 && previous != string(b) {
				t.Fatalf("output depends on executable name: %s %v", name, args)
			}
			previous = string(b)
		}
	}
}

func TestConnectionDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CC_REMOTE_DATA_DIR", dir)
	cfg := service.Config{DataDir: dir, Listen: []string{"127.0.0.1:8999"}}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "service.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	got := connectionDefaults([]string{"--resume", "abc"})
	if !reflect.DeepEqual(got, []string{"--host", "http://127.0.0.1:8999", "--resume", "abc"}) {
		t.Fatalf("defaults %q", got)
	}
	got = connectionDefaults([]string{"--host", "https://example.test", "--resume", "abc"})
	if strings.Join(got, " ") != "--host http://127.0.0.1:8999 --host https://example.test --resume abc" {
		t.Fatalf("explicit context must override defaults: %q", got)
	}
}

func TestAddrInUse(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, err = net.Listen("tcp", l.Addr().String())
	if err == nil {
		t.Skip("platform allowed a second bind")
	}
	if !addrInUse(fmt.Errorf("listen: %w", err)) {
		t.Fatalf("not recognized as in use: %v", err)
	}
	if addrInUse(errors.New("other")) {
		t.Fatal("false positive")
	}
}

func TestLangArg(t *testing.T) {
	for want, argv := range map[string][]string{
		"en": {"--listen", "x", "--lang", "en", "-h"},
		"zh": {"-lang=zh"},
		"":   {"--", "--lang", "en"},
	} {
		if got := langArg(argv); got != want {
			t.Errorf("langArg(%v) = %q, want %q", argv, got, want)
		}
	}
}

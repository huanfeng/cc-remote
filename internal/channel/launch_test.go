package channel

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"cc-remote/internal/prefs"
)

func TestNativeLaunchArguments(t *testing.T) {
	uuid := "abcd1234-1234-1234-1234-123456789abc"
	for _, args := range [][]string{{"--resume", uuid, "--model", "sonnet"}, {"--session-id=" + uuid}, {"-r", uuid}} {
		got, id, err := prepareNativeArgs(args, "")
		if err != nil || id != uuid || !slices.Equal(got, args) {
			t.Fatalf("args %v: %v %s %v", args, got, id, err)
		}
	}
	for _, args := range [][]string{{"-p", "hello"}, {"--print=json"}, {"--resume"}, {"--resume", "name"}, {"--continue"}, {"--fork-session"}, {"--settings", "x"}, {"--session-id", uuid, "--resume", uuid}} {
		if _, _, err := prepareNativeArgs(args, ""); err == nil {
			t.Fatalf("accepted incompatible arguments %v", args)
		}
	}
	got, id, err := prepareNativeArgs([]string{"--permission-mode", "plan", "--model", "sonnet"}, "")
	if err != nil || !launchUUID.MatchString(id) || !slices.Contains(got, "plan") || slices.Contains(got, "default") {
		t.Fatalf("overrode native permissions: %v %v", got, err)
	}
	if _, _, err = prepareNativeArgs([]string{"--resume", uuid}, uuid); err == nil {
		t.Fatal("accepted two resume sources")
	}
}

func TestEnsureHostAuthenticatesMatchingDataDirectory(t *testing.T) {
	t.Setenv("CC_REMOTE_TOKEN", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "access-token"), []byte("matching-test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer matching-test-token" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer server.Close()
	if err := ensureHost(dir, server.URL, "claude", ""); err != nil {
		t.Fatal(err)
	}
	if err := ensureHost(t.TempDir(), server.URL, "claude", ""); err == nil {
		t.Fatal("accepted another data directory")
	}
}

func TestApplyPreferencesRespectsNativeFlags(t *testing.T) {
	p := prefs.Preferences{PermissionMode: "bypassPermissions", Model: "opus", Effort: "high"}
	got := applyPreferences([]string{"--session-id", "x"}, p)
	want := []string{"--session-id", "x", "--permission-mode", "bypassPermissions", "--model", "opus", "--effort", "high"}
	if !slices.Equal(got, want) {
		t.Fatalf("injected %v", got)
	}
	for _, native := range [][]string{{"--permission-mode", "plan"}, {"--permission-mode=plan"}, {"--dangerously-skip-permissions"}, {"--allow-dangerously-skip-permissions"}} {
		if got = applyPreferences(native, p); slices.Contains(got, "bypassPermissions") {
			t.Fatalf("overrode native permission flag %v: %v", native, got)
		}
	}
	got = applyPreferences([]string{"--model=sonnet", "--effort", "low"}, p)
	if slices.Contains(got, "opus") || slices.Contains(got, "high") || !slices.Contains(got, "bypassPermissions") {
		t.Fatalf("overrode native model/effort: %v", got)
	}
	if got = applyPreferences([]string{"a"}, prefs.Preferences{}); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("empty preferences injected %v", got)
	}
}

func TestInheritedDangerWarning(t *testing.T) {
	for _, mode := range []string{"bypassPermissions", "dontAsk", "auto"} {
		w := inheritedDangerWarning([]string{"--session-id", "x"}, prefs.Preferences{PermissionMode: mode})
		if !strings.Contains(w, mode) || !strings.Contains(w, "--permission-mode default") {
			t.Fatalf("%s: %q", mode, w)
		}
	}
	if w := inheritedDangerWarning(nil, prefs.Preferences{PermissionMode: "acceptEdits"}); w != "" {
		t.Fatalf("safe mode warned %q", w)
	}
	for _, native := range [][]string{{"--permission-mode=default"}, {"--dangerously-skip-permissions"}} {
		if w := inheritedDangerWarning(native, prefs.Preferences{PermissionMode: "bypassPermissions"}); w != "" {
			t.Fatalf("explicit flag %v warned %q", native, w)
		}
	}
}

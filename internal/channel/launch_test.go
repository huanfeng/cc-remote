package channel

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
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

package channel

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

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

func TestValidateHostAcceptsOnlyLoopbackOrigins(t *testing.T) {
	for _, host := range []string{"https://example.com", "http://127.0.0.1:8787/path", "file:///tmp", "http://token@localhost", "http://localhost?secret=yes"} {
		if validateHost(host) == nil {
			t.Errorf("accepted %s", host)
		}
	}
	for _, host := range []string{"http://127.0.0.1:8787", "http://localhost:8787", "http://[::1]:8787"} {
		if err := validateHost(host); err != nil {
			t.Error(err)
		}
	}
}

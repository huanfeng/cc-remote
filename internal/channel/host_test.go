package channel

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cc-remote/internal/hostauth"
)

func TestEnsureHostAuthenticatesMatchingDataDirectory(t *testing.T) {
	t.Setenv("CC_REMOTE_TOKEN", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "access-token"), []byte("matching-test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == hostauth.Path {
			w.Write([]byte(`{"proof":"` + hostauth.Proof("matching-test-token", r.URL.Query().Get("nonce"), r.Context().Value(http.LocalAddrContextKey).(net.Addr).String()) + `"}`))
			return
		}
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

// Whatever listens on the port without proving it holds the token (another
// user's server while the host is down) never receives it.
func TestEnsureHostSendsNoTokenToAnImpostor(t *testing.T) {
	t.Setenv("CC_REMOTE_TOKEN", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "access-token"), []byte("matching-test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	leaked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = leaked || strings.Contains(r.Header.Get("Authorization"), "matching-test-token")
		w.WriteHeader(401)
	}))
	defer server.Close()
	if err := ensureHost(dir, server.URL, "claude", ""); err == nil {
		t.Fatal("accepted a service that cannot prove it is the host")
	}
	if leaked {
		t.Fatal("the token was sent to it")
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

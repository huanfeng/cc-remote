package hostauth

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"cc-remote/internal/hostlock"
)

// A host from before /api/hello answers 401 to it, as anything else could:
// it is trusted only while a host holds the data directory's lock.
func TestCheckOldHost(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) }))
	defer old.Close()
	dir := t.TempDir()
	if err := Check(old.Client(), old.URL, "token-0123456789abcdef", dir); !errors.Is(err, ErrNotHost) {
		t.Fatalf("no host holds the lock: %v", err)
	}
	release, err := hostlock.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := Check(old.Client(), old.URL, "token-0123456789abcdef", dir); err != nil {
		t.Fatalf("the running host holds the lock: %v", err)
	}
	if err := Check(old.Client(), old.URL, "token-0123456789abcdef", ""); !errors.Is(err, ErrNotHost) {
		t.Fatalf("no data directory to check: %v", err)
	}
	// Once a host that proves itself has run there, the lock is no evidence:
	// it does not say where that host listens.
	if err := MarkHost(dir); err != nil {
		t.Fatal(err)
	}
	if err := Check(old.Client(), old.URL, "token-0123456789abcdef", dir); !errors.Is(err, ErrNotHost) {
		t.Fatalf("after a newer host ran: %v", err)
	}
}

func TestCheckProof(t *testing.T) {
	const token = "token-0123456789abcdef"
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"proof":"` + Proof(token, r.URL.Query().Get("nonce"), localAddr(r)) + `"}`))
	}))
	defer host.Close()
	if err := Check(host.Client(), host.URL, token, ""); err != nil {
		t.Fatal(err)
	}
	if err := Check(host.Client(), host.URL, "other-0123456789abcdef", ""); !errors.Is(err, ErrWrongToken) {
		t.Fatalf("other token: %v", err)
	}
	// Another service relays the challenge to the real host: the proof names
	// the real host's address, not the one the client connected to.
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(host.URL + r.URL.RequestURI())
		if err != nil {
			t.Error(err)
			return
		}
		defer resp.Body.Close()
		io.Copy(w, resp.Body)
	}))
	defer relay.Close()
	if err := Check(relay.Client(), relay.URL, token, ""); !errors.Is(err, ErrWrongToken) {
		t.Fatalf("a relayed proof: %v", err)
	}
	// Or it redirects the client there: the proof then names where the client
	// was sent, but the token would go to the service.
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, host.URL+r.URL.RequestURI(), http.StatusFound)
	}))
	defer redirect.Close()
	if err := Check(http.DefaultClient, redirect.URL, token, ""); err == nil {
		t.Fatal("a redirect to the real host passed")
	}
}

func localAddr(r *http.Request) string {
	return r.Context().Value(http.LocalAddrContextKey).(net.Addr).String()
}

func TestVerifyRemoteHTTPS(t *testing.T) {
	if err := Verify(http.DefaultClient, "https://cc.example.invalid", "t", ""); err != nil {
		t.Fatalf("an HTTPS host elsewhere: %v", err)
	}
	for _, local := range []string{"http://127.0.0.1:8787", "https://localhost:8787", "http://[::1]:1"} {
		if !Local(local) {
			t.Fatalf("%s is local", local)
		}
	}
	if Local("http://192.168.1.5:8787") {
		t.Fatal("a LAN address is not local")
	}
}

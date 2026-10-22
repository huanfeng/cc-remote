package hostauth

import (
	"errors"
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
		w.Write([]byte(`{"proof":"` + Proof(token, r.URL.Query().Get("nonce")) + `"}`))
	}))
	defer host.Close()
	if err := Check(host.Client(), host.URL, token, ""); err != nil {
		t.Fatal(err)
	}
	if err := Check(host.Client(), host.URL, "other-0123456789abcdef", ""); !errors.Is(err, ErrWrongToken) {
		t.Fatalf("other token: %v", err)
	}
	replay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"proof":"` + Proof(token, "0123456789abcdef0123456789abcdef") + `"}`))
	}))
	defer replay.Close()
	if err := Check(replay.Client(), replay.URL, token, ""); !errors.Is(err, ErrWrongToken) {
		t.Fatalf("a replayed proof: %v", err)
	}
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

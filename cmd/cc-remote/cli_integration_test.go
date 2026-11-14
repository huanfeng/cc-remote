package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"cc-remote/internal/hostauth"
)

// The test binary doubles as Claude so the real direct-launch path can be
// exercised with arbitrary native flags, without requiring Claude installed.
func TestMain(m *testing.M) {
	if path := os.Getenv("CC_REMOTE_TEST_NATIVE_ARGV"); path != "" {
		b, _ := json.Marshal(os.Args[1:])
		if err := os.WriteFile(path, b, 0600); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRunCLINativeAndCatalogRoutes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CC_REMOTE_DATA_DIR", dir)
	t.Setenv("CC_REMOTE_TOKEN", "cli-test-token")
	if err := os.WriteFile(filepath.Join(dir, "access-token"), []byte("cli-test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("CLI created or changed a session: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case hostauth.Path:
			addr := r.Context().Value(http.LocalAddrContextKey).(interface{ String() string }).String()
			json.NewEncoder(w).Encode(map[string]string{"proof": hostauth.Proof("cli-test-token", r.URL.Query().Get("nonce"), addr)})
		case "/api/sessions":
			w.Write([]byte("[]"))
		case "/api/profiles", "/api/profiles/oauth":
			w.Write([]byte(`{"profiles":[],"defaultProfiles":{}}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	b, _ := json.Marshal(map[string]any{"dataDir": dir, "listen": []string{server.Listener.Addr().String()}})
	if err := os.WriteFile(filepath.Join(dir, "service.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, native := range [][]string{{"--help"}, {"-p", "--data-dir"}, {"--resume", "status"}} {
		capture := filepath.Join(t.TempDir(), "argv.json")
		t.Setenv("CC_REMOTE_TEST_NATIVE_ARGV", capture)
		args := append([]string{"claude", "--profile", "@native", "--claude", exe, "--"}, native...)
		if err := runCLI(args); err != nil {
			t.Fatalf("native %q: %v", native, err)
		}
		b, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		if err = json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, native) {
			t.Errorf("native arguments changed: got %q, want %q", got, native)
		}
	}
	for _, args := range [][]string{{"oauth", "ls"}, {"profile", "ls"}, {"--data-dir", dir, "profile", "default"}} {
		if err := runCLI(args); err != nil {
			t.Errorf("catalog %q: %v", args, err)
		}
	}
}

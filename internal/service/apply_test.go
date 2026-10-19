package service

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"cc-remote/internal/i18n"
)

// fakeHost answers the reload and restart endpoints of a running host.
func fakeHost(t *testing.T, reload func(w http.ResponseWriter), restarts *int) *Config {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token-0123456789" {
			w.WriteHeader(401)
		}
	})
	mux.HandleFunc("POST /api/admin/reload", func(w http.ResponseWriter, _ *http.Request) { reload(w) })
	mux.HandleFunc("POST /api/admin/restart", func(w http.ResponseWriter, _ *http.Request) {
		if restarts == nil {
			w.WriteHeader(404)
			return
		}
		*restarts++
		w.WriteHeader(202)
		w.Write([]byte(`{"status":"waiting","busy":["long task"]}`))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "access-token"), []byte("test-token-0123456789\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return &Config{DataDir: dir, Listen: []string{strings.TrimPrefix(ts.URL, "http://")}}
}

func TestApplyToHost(t *testing.T) {
	i18n.Set(i18n.EN)
	t.Setenv("CC_REMOTE_TOKEN", "")
	for _, c := range []struct {
		name     string
		reload   func(w http.ResponseWriter)
		restarts int
		want     string
	}{
		{"listen only", func(w http.ResponseWriter) { w.Write([]byte(`{"listen":["127.0.0.1:8787","192.168.1.5:8787"]}`)) }, 0, "Applied without a restart; listening at 127.0.0.1:8787, 192.168.1.5:8787"},
		{"needs restart", func(w http.ResponseWriter) {
			w.Write([]byte(`{"listen":["127.0.0.1:8787"],"restart":["env","service.json"]}`))
		}, 1, "--env, service.json take effect after a restart"},
		{"old host", func(w http.ResponseWriter) { w.WriteHeader(404) }, 1, "once these sessions are idle: long task"},
	} {
		t.Run(c.name, func(t *testing.T) {
			restarts := 0
			cfg := fakeHost(t, c.reload, &restarts)
			url, _ := findHost(cfg)
			if url == "" {
				t.Fatal("fake host not found")
			}
			var out bytes.Buffer
			if err := applyToHost(url, cfg, &out); err != nil {
				t.Fatal(err)
			}
			if restarts != c.restarts || !strings.Contains(out.String(), c.want) {
				t.Fatalf("restarts = %d\n%s", restarts, out.String())
			}
		})
	}
	cfg := fakeHost(t, func(w http.ResponseWriter) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"listen on 192.168.1.5:8787: address in use"}`))
	}, new(int))
	url, _ := findHost(cfg)
	var failed reloadFailed
	if err := applyToHost(url, cfg, &bytes.Buffer{}); !errors.As(err, &failed) || !strings.Contains(err.Error(), "address in use") {
		t.Fatalf("failed reload: %v", err)
	}
}

// A host before v0.5 can neither reload nor restart itself: config replaces it.
func TestApplyToHostTooOld(t *testing.T) {
	t.Setenv("CC_REMOTE_TOKEN", "")
	cfg := fakeHost(t, func(w http.ResponseWriter) { w.WriteHeader(404) }, nil)
	url, _ := findHost(cfg)
	if err := applyToHost(url, cfg, &bytes.Buffer{}); !errors.Is(err, errNoSelfRestart) {
		t.Fatalf("err = %v", err)
	}
}

// A host started by hand answers 501: config replaces it as before.
func TestApplyToHostNotService(t *testing.T) {
	t.Setenv("CC_REMOTE_TOKEN", "")
	cfg := fakeHost(t, func(w http.ResponseWriter) { w.WriteHeader(501) }, new(int))
	url, _ := findHost(cfg)
	if err := applyToHost(url, cfg, &bytes.Buffer{}); !errors.Is(err, errNotService) {
		t.Fatalf("err = %v", err)
	}
}

// Removed variables are remembered so the host can unset them; setting one
// again forgets the removal.
func TestEnvRemovedTracking(t *testing.T) {
	cfg := &Config{Env: map[string]string{"HTTPS_PROXY": "http://p:1"}}
	var out bytes.Buffer
	if err := applyEnv(cfg, false, nil, []string{"HTTPS_PROXY="}, false, &out); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Env) != 0 || !reflect.DeepEqual(cfg.EnvRemoved, []string{"HTTPS_PROXY"}) {
		t.Fatalf("after removal: %v %v", cfg.Env, cfg.EnvRemoved)
	}
	if err := applyEnv(cfg, false, nil, []string{"HTTPS_PROXY=http://q:2"}, false, &out); err != nil {
		t.Fatal(err)
	}
	if cfg.EnvRemoved != nil {
		t.Fatalf("set again: %v", cfg.EnvRemoved)
	}
}

func TestNoteAutoLanguage(t *testing.T) {
	i18n.Set(i18n.EN)
	cfg := &Config{AutoLanguage: "zh"}
	noteAutoLanguage(cfg, false)
	if cfg.AutoLanguage != "zh" {
		t.Fatalf("another change switched auto to %q", cfg.AutoLanguage)
	}
	noteAutoLanguage(cfg, true)
	if cfg.AutoLanguage != "en" {
		t.Fatalf("--lang auto: auto = %q", cfg.AutoLanguage)
	}
	cfg = &Config{}
	noteAutoLanguage(cfg, false)
	if cfg.AutoLanguage != "en" {
		t.Fatalf("first save: auto = %q", cfg.AutoLanguage)
	}
	cfg.Language = "zh"
	noteAutoLanguage(cfg, false)
	if cfg.AutoLanguage != "" {
		t.Fatalf("explicit language keeps auto %q", cfg.AutoLanguage)
	}
}

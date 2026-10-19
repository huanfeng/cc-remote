package terminal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A stream the host refuses carries its reason; a failing host (5xx) or one
// not answering is tried again after a restart, a refused session is not.
func TestDialStreamErrors(t *testing.T) {
	status := http.StatusInternalServerError
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/terminals/abc/stream" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("request %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"Claude Code just exited; try again shortly"}`))
	}))
	c := &client{host: srv.URL, token: "tok"}
	_, err := c.dialStream(context.Background(), "abc")
	if err == nil || !strings.Contains(err.Error(), "(500)") || !strings.Contains(err.Error(), "Claude Code just exited") || !retryableDial(err) {
		t.Fatalf("500: %v retryable=%v", err, retryableDial(err))
	}
	status = http.StatusNotFound
	if _, err = c.dialStream(context.Background(), "abc"); err == nil || retryableDial(err) {
		t.Fatalf("404: %v retryable=%v", err, retryableDial(err))
	}
	srv.Close()
	if _, err = c.dialStream(context.Background(), "abc"); err == nil || !retryableDial(err) {
		t.Fatalf("host down: %v retryable=%v", err, retryableDial(err))
	}
	if retryableDial(errHostRestart) || retryableDial(nil) {
		t.Fatal("only refused streams are retried here")
	}
}

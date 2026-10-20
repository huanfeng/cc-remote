package server

import (
	"net/http"
	"testing"
)

// TestStandaloneFrontendCORS covers a web frontend served from another site:
// only an allowed origin gets its preflight answered and its replies readable.
func TestStandaloneFrontendCORS(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.AllowOrigins = []string{"https://ccr.example.com"}
	do := func(method, origin, token string, header map[string]string) *http.Response {
		req, _ := http.NewRequest(method, ts.URL+"/api/info", nil)
		req.Header.Set("Origin", origin)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	preflight := map[string]string{"Access-Control-Request-Method": "GET", "Access-Control-Request-Headers": "authorization", "Access-Control-Request-Private-Network": "true"}
	resp := do(http.MethodOptions, "https://ccr.example.com", "", preflight)
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "https://ccr.example.com" || resp.Header.Get("Access-Control-Allow-Private-Network") != "true" {
		t.Fatalf("allowed preflight: %d %v", resp.StatusCode, resp.Header)
	}
	resp = do(http.MethodGet, "https://ccr.example.com", testToken, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != "https://ccr.example.com" || resp.Header.Get("Access-Control-Expose-Headers") != "ETag, X-CC-Path" {
		t.Fatalf("allowed request: %d %v", resp.StatusCode, resp.Header)
	}
	if resp = do(http.MethodGet, "https://ccr.example.com", "wrong-token-wrong-token", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("allowed origin with a wrong token: %d", resp.StatusCode)
	}
	resp = do(http.MethodOptions, "https://evil.example", "", preflight)
	if resp.StatusCode == http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("other origin preflight: %d %v", resp.StatusCode, resp.Header)
	}
	if resp = do(http.MethodGet, "https://evil.example", testToken, nil); resp.StatusCode != http.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("other origin request: %d %v", resp.StatusCode, resp.Header)
	}
}

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSupplierProbeSavedCredentialAndIsolation(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/models" || r.Header.Get("x-api-key") != "fixture-secret" {
			t.Error("wrong endpoint or credential")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"model-b"},{"id":"model-a"},{"id":"model-a"}]}`))
	}))
	defer upstream.Close()
	raw, _ := json.Marshal([]supplierConfig{{ID: "saved", App: "claude", BaseURL: upstream.URL, AuthType: "apiKey", Credential: "fixture-secret"}})
	os.WriteFile(filepath.Join(s.cfg.DataDir, "supplier-configs.json"), raw, 0600)
	q := map[string]string{"id": "saved", "app": "claude", "baseUrl": upstream.URL, "authType": "apiKey"}
	code, b := request(t, ts, "POST", "/api/supplier-probe", q, testToken)
	if code != 200 || calls != 1 || bytes.Contains(b, []byte("fixture-secret")) {
		t.Fatalf("probe %d %s calls%d", code, b, calls)
	}
	var reply supplierProbeReply
	json.Unmarshal(b, &reply)
	if len(reply.Models) != 2 || reply.Models[0] != "model-a" {
		t.Fatalf("models %s", b)
	}
	q["baseUrl"] = upstream.URL + "/changed"
	code, _ = request(t, ts, "POST", "/api/supplier-probe", q, testToken)
	if code != 400 || calls != 1 {
		t.Fatal("saved credential sent to changed endpoint")
	}
}
func TestSupplierProbeRefusesRedirectAndRedactsUpstream(t *testing.T) {
	s, ts, _ := setup(t, nil)
	s.cfg.DataDir = t.TempDir()
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(302)
		w.Write([]byte("fixture-secret"))
	}))
	defer upstream.Close()
	code, b := request(t, ts, "POST", "/api/supplier-probe", map[string]string{"app": "codex", "baseUrl": upstream.URL + "/v1", "authType": "apiKey", "credential": "fixture-secret"}, testToken)
	if code != 502 || targetCalls != 0 || bytes.Contains(b, []byte("fixture-secret")) {
		t.Fatalf("unsafe redirect %d %s", code, b)
	}
}

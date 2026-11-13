package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAccountIdentityRequiresServerConfirmedAccountAndOrganization(t *testing.T) {
	body := `{"account":{"uuid":"account"},"organization":{"uuid":"org"}}`
	status := 200
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("anthropic-beta") == "" {
			t.Error("missing OAuth headers")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer api.Close()
	old := ProfileURL
	ProfileURL = api.URL
	defer func() { ProfileURL = old }()
	id, err := FetchAccountIdentity(context.Background(), api.Client(), "test-token")
	if err != nil || id != "account/org" {
		t.Fatalf("%q %v", id, err)
	}
	for _, invalid := range []string{`{}`, `{"account":{"uuid":"account"}}`, `invalid`} {
		body = invalid
		if id, err = FetchAccountIdentity(context.Background(), api.Client(), "test-token"); err == nil || id != "" {
			t.Fatalf("accepted %s", invalid)
		}
	}
	status = http.StatusForbidden
	body = "private response body test-token"
	if _, err = FetchAccountIdentity(context.Background(), api.Client(), "test-token"); err == nil || err.Error() == body {
		t.Fatalf("unsafe error: %v", err)
	}
}

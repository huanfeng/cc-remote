package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSupplierSwitchWarningPreference(t *testing.T) {
	_, ts, _ := setup(t, nil)
	const path = "/api/profiles/switch-warning"
	check := func(want bool) {
		t.Helper()
		code, body := request(t, ts, http.MethodGet, path, nil, testToken)
		var result map[string]bool
		if code != http.StatusOK || json.Unmarshal(body, &result) != nil || len(result) != 1 || result["enabled"] != want {
			t.Fatalf("warning: got %d %s, want enabled=%v", code, body, want)
		}
	}
	check(true)
	if code, _ := request(t, ts, http.MethodPut, path, map[string]bool{"enabled": false}, ""); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated write: %d", code)
	}
	if code, _ := request(t, ts, http.MethodPut, path, map[string]bool{}, testToken); code != http.StatusBadRequest {
		t.Fatalf("missing choice: %d", code)
	}
	check(true)
	for _, enabled := range []bool{false, true} {
		if code, body := request(t, ts, http.MethodPut, path, map[string]bool{"enabled": enabled}, testToken); code != http.StatusNoContent {
			t.Fatalf("save warning: %d %s", code, body)
		}
		check(enabled)
	}
}

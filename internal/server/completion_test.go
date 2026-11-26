package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"cc-remote/internal/store"
)

func TestCompletionReadIsSharedAcrossClients(t *testing.T) {
	_, ts, st := setup(t, nil)
	x := createSession(t, ts)
	finish := func() store.Session {
		t.Helper()
		for _, status := range []string{"running", "completed"} {
			if err := st.SetStatus(x.ID, status); err != nil {
				t.Fatal(err)
			}
		}
		done, err := st.Get(x.ID)
		if err != nil {
			t.Fatal(err)
		}
		return done
	}
	first := finish()
	path := "/api/sessions/" + x.ID + "/completion-read"
	read := func(tag string, wantStatus int) (string, store.Session) {
		t.Helper()
		r, err := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+testToken)
		if tag != "" {
			r.Header.Set("If-None-Match", tag)
		}
		res, err := new(http.Client).Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != wantStatus {
			t.Fatalf("list: status %d, want %d", res.StatusCode, wantStatus)
		}
		var sessions []store.Session
		if wantStatus == http.StatusOK {
			if err := json.NewDecoder(res.Body).Decode(&sessions); err != nil || len(sessions) != 1 {
				t.Fatalf("list: %+v %v", sessions, err)
			}
			return res.Header.Get("ETag"), sessions[0]
		}
		return res.Header.Get("ETag"), store.Session{}
	}
	tag, unread := read("", http.StatusOK)
	if tag == "" || unread.CompletedAt != first.CompletedAt || unread.CompletionReadAt != "" {
		t.Fatalf("unread: %+v etag=%q", unread, tag)
	}
	receipt := map[string]string{"completedAt": first.CompletedAt}
	if code, _ := request(t, ts, "PUT", path, receipt, ""); code != http.StatusUnauthorized {
		t.Fatalf("uncredentialed receipt: %d", code)
	}
	if code, b := request(t, ts, "PUT", path, receipt, testToken); code != http.StatusNoContent {
		t.Fatalf("receipt: %d %s", code, b)
	}
	readTag, viewed := read(tag, http.StatusOK)
	if viewed.CompletionReadAt != first.CompletedAt || viewed.Status != first.Status || viewed.UpdatedAt != first.UpdatedAt {
		t.Fatalf("read: %+v", viewed)
	}
	if code, b := request(t, ts, "PUT", path, receipt, testToken); code != http.StatusNoContent {
		t.Fatalf("duplicate receipt: %d %s", code, b)
	}
	read(readTag, http.StatusNotModified)
	second := finish()
	if code, b := request(t, ts, "PUT", path, receipt, testToken); code != http.StatusNoContent {
		t.Fatalf("stale receipt: %d %s", code, b)
	}
	_, unread = read(readTag, http.StatusOK)
	if unread.CompletedAt != second.CompletedAt || unread.CompletedAt == first.CompletedAt || unread.CompletionReadAt != "" {
		t.Fatalf("second completion: %+v", unread)
	}
}

package server

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/store"
)

func TestMessageSearchFindsUnloadedHistoryAndOpensContext(t *testing.T) {
	_, host, st := setup(t, noRun)
	session := createSession(t, host)
	old, err := st.AppendAt(session.ID, "user", "🙂 Résumé [old] 原文", nil, "2020-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 320 {
		if _, err := st.Append(session.ID, "tool", fmt.Sprintf("tool %d: Résumé [old]", i), nil); err != nil {
			t.Fatal(err)
		}
	}
	tail := getPage(t, host, host.URL+"/api/sessions/"+session.ID+"?tail=200")
	if tail.Events[0].Seq <= old.Seq {
		t.Fatal("search hit must be outside the loaded page")
	}
	path := "/api/search/messages?" + url.Values{"q": {"RÉSUMÉ [old]"}, "sessionId": {session.ID}}.Encode()
	code, body := request(t, host, "GET", path, nil, testToken)
	var results struct{ Hits []messageHit }
	if code != 200 || json.Unmarshal(body, &results) != nil || len(results.Hits) != 1 {
		t.Fatalf("search %d: %s", code, body)
	}
	hit := results.Hits[0]
	if hit.Locator != strconv.FormatInt(old.Seq, 10) || hit.MatchStart != 3 || hit.MatchEnd != 15 {
		t.Fatalf("unexpected hit %+v", hit)
	}
	path = "/api/search/context?" + url.Values{"sessionId": {session.ID}, "locator": {hit.Locator}}.Encode()
	code, body = request(t, host, "GET", path, nil, testToken)
	var context struct {
		Events   []store.Event
		MatchSeq int64
	}
	if code != 200 || json.Unmarshal(body, &context) != nil || context.MatchSeq != old.Seq ||
		len(context.Events) != 1 || context.Events[0].Text != old.Text {
		t.Fatalf("context %d: %s", code, body)
	}
}

func TestMessageSearchFiltersAndPaginates(t *testing.T) {
	_, host, st := setup(t, noRun)
	session := createSession(t, host)
	base := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	for i := range searchPageSize + 4 {
		if _, err := st.AppendAt(session.ID, "assistant", "message with needle", nil, base.Add(time.Duration(i)*time.Second).Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.AppendAt(session.ID, "user", "too old needle", nil, base.Add(-time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	query := url.Values{"q": {"needle"}, "provider": {"claude"}, "cwd": {session.Cwd}, "since": {base.Format(time.RFC3339)}}
	path := "/api/search/messages?"
	if code, _ := request(t, host, "GET", path+query.Encode(), nil, ""); code != 401 {
		t.Fatalf("unauthenticated search: %d", code)
	}
	search := func() ([]messageHit, string) {
		t.Helper()
		code, body := request(t, host, "GET", path+query.Encode(), nil, testToken)
		var page struct {
			Hits []messageHit
			Next string
		}
		if code != 200 || json.Unmarshal(body, &page) != nil {
			t.Fatalf("search %d: %s", code, body)
		}
		return page.Hits, page.Next
	}
	first, cursor := search()
	if len(first) != searchPageSize || cursor == "" {
		t.Fatalf("first page: %d, cursor %q", len(first), cursor)
	}
	query.Set("before", cursor)
	next, cursor := search()
	if len(next) != 4 || cursor != "" || strings.Compare(first[len(first)-1].CreatedAt, next[0].CreatedAt) <= 0 {
		t.Fatalf("second page: %+v, cursor %q", next, cursor)
	}
	query.Del("before")
	query.Set("provider", "codex")
	if hits, _ := search(); len(hits) != 0 {
		t.Fatalf("provider filter: %+v", hits)
	}
}

package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

func noRun(context.Context, claude.Options, func(claude.Event) error) error { return nil }

type page struct {
	Session json.RawMessage `json:"session"`
	Events  []store.Event   `json:"events"`
	HasMore bool            `json:"hasMore"`
}

func getPage(t *testing.T, ts interface {
	Client() *http.Client
}, url string) page {
	t.Helper()
	resp, err := ts.Client().Do(authed(t, url))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var p page
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&p) != nil {
		t.Fatalf("GET %s: %d", url, resp.StatusCode)
	}
	return p
}

func authed(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	return req
}

// Pages walk a transcript backwards from the end without gaps or overlap,
// and each starts at a user message so a turn is never split.
func TestTranscriptPagesFromTheEnd(t *testing.T) {
	_, ts, st := setup(t, noRun)
	x := createSession(t, ts)
	total := 0
	for turn := range 12 {
		_, _ = st.Append(x.ID, "user", fmt.Sprintf("u%d", turn), nil)
		total++
		for i := range 40 {
			_, _ = st.Append(x.ID, "tool", "", json.RawMessage(fmt.Sprintf(`{"id":"t%d-%d","name":"Read","input":{}}`, turn, i)))
			total++
		}
	}
	p := getPage(t, ts, ts.URL+"/api/sessions/"+x.ID+"?tail=100&compact=1")
	if !p.HasMore || len(p.Events) < 100 || p.Events[0].Kind != "user" {
		t.Fatalf("tail page: %d events, first %q, hasMore %v", len(p.Events), p.Events[0].Kind, p.HasMore)
	}
	seen := append([]store.Event{}, p.Events...)
	for p.HasMore {
		p = getPage(t, ts, fmt.Sprintf("%s/api/sessions/%s/events?before=%d&limit=100", ts.URL, x.ID, seen[0].Seq))
		if len(p.Events) == 0 || p.Events[0].Kind != "user" {
			t.Fatalf("older page %+v", p)
		}
		seen = append(append([]store.Event{}, p.Events...), seen...)
	}
	if len(seen) != total {
		t.Fatalf("walked %d of %d events", len(seen), total)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i].Seq <= seen[i-1].Seq {
			t.Fatal("pages overlap or are out of order")
		}
	}
}

// Long tool output is cut in listings and served whole on request; question
// and plan tools are never cut.
func TestListingsTrimLongToolOutput(t *testing.T) {
	_, ts, st := setup(t, noRun)
	x := createSession(t, ts)
	long := strings.Repeat("界", 5000) // 15000 bytes
	resultPayload, _ := json.Marshal(map[string]any{"tool_use_id": "t1", "content": long})
	cutEvent, _ := st.Append(x.ID, "tool_result", "", resultPayload)
	planPayload, _ := json.Marshal(map[string]any{"id": "t2", "name": "ExitPlanMode", "input": map[string]string{"plan": long}})
	_, _ = st.Append(x.ID, "tool", "", planPayload)
	p := getPage(t, ts, ts.URL+"/api/sessions/"+x.ID+"?tail=50&compact=1")
	if len(p.Events) != 2 || !p.Events[0].Truncated || len(p.Events[0].Payload) > trimLimit+200 || p.Events[1].Truncated {
		t.Fatalf("trim: %d events, first truncated=%v (%d bytes), plan truncated=%v", len(p.Events), p.Events[0].Truncated, len(p.Events[0].Payload), p.Events[1].Truncated)
	}
	var cut map[string]string
	if json.Unmarshal(p.Events[0].Payload, &cut) != nil || !strings.HasPrefix(long, strings.TrimSuffix(cut["content"], "\n…")) {
		t.Fatal("cut content is not a valid prefix")
	}
	code, b := request(t, ts, "GET", fmt.Sprintf("/api/sessions/%s/events/%d", x.ID, cutEvent.Seq), nil, testToken)
	var full store.Event
	if code != 200 || json.Unmarshal(b, &full) != nil || full.Truncated || !strings.Contains(string(full.Payload), long) {
		t.Fatalf("full event %d", code)
	}
	if code, _ := request(t, ts, "GET", "/api/sessions/"+x.ID+"/events/999999", nil, testToken); code != 404 {
		t.Fatalf("missing event: %d", code)
	}
	// Without compact=1 listings stay whole (older clients).
	if p := getPage(t, ts, ts.URL+"/api/sessions/"+x.ID); p.Events[0].Truncated || p.HasMore {
		t.Fatal("full listing was trimmed")
	}
}

func TestNativeTranscriptPages(t *testing.T) {
	s, ts, _ := setup(t, noRun)
	id := store.ID()
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "paged")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for i := range 30 {
		lines = append(lines, fmt.Sprintf(`{"type":"user","cwd":"/tmp","message":{"content":"q%d"}}`, i), fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":"a%d"}]}}`, i))
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := getPage(t, ts, ts.URL+"/api/native/sessions/"+id+"?tail=9&compact=1")
	if !p.HasMore || len(p.Events) != 10 || p.Events[0].Text != "q25" || p.Events[9].Seq != 60 {
		t.Fatalf("native tail %+v", p.Events)
	}
	older := getPage(t, ts, fmt.Sprintf("%s/api/native/sessions/%s/events?before=%d&limit=50", ts.URL, id, p.Events[0].Seq))
	if older.HasMore || len(older.Events) != 50 || older.Events[49].Seq != 50 {
		t.Fatalf("native older page: %d events", len(older.Events))
	}
	code, b := request(t, ts, "GET", "/api/native/sessions/"+id+"/events?after=58", nil, testToken)
	var after []store.Event
	if code != 200 || json.Unmarshal(b, &after) != nil || len(after) != 2 || after[0].Seq != 59 {
		t.Fatalf("native poll %d %s", code, b)
	}
	code, b = request(t, ts, "GET", "/api/native/sessions/"+id+"/events/3", nil, testToken)
	var one store.Event
	if code != 200 || json.Unmarshal(b, &one) != nil || one.Seq != 3 || one.Text != "q1" {
		t.Fatalf("native event %d %s", code, b)
	}
}

// GET responses carry an ETag (unchanged polls get 304) and are gzipped for
// clients that accept it.
func TestResponsesCarryETagAndGzip(t *testing.T) {
	_, ts, st := setup(t, noRun)
	x := createSession(t, ts)
	for i := range 40 {
		_, _ = st.Append(x.ID, "assistant", strings.Repeat(fmt.Sprintf("reply %d ", i), 20), nil)
	}
	req := authed(t, ts.URL+"/api/sessions/"+x.ID)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	tag := resp.Header.Get("ETag")
	if resp.Header.Get("Content-Encoding") != "gzip" || tag == "" {
		t.Fatalf("headers %v", resp.Header)
	}
	zr, err := gzip.NewReader(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if len(raw) >= len(plain) || !strings.Contains(string(plain), "reply 39") {
		t.Fatalf("gzip %d -> %d", len(plain), len(raw))
	}
	req = authed(t, ts.URL+"/api/sessions/"+x.ID)
	req.Header.Set("If-None-Match", tag)
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation: %d", resp.StatusCode)
	}
	_, _ = st.Append(x.ID, "assistant", "new", nil)
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("ETag") == tag {
		t.Fatalf("changed body: %d %s", resp.StatusCode, resp.Header.Get("ETag"))
	}
}

// In a long turn without a user message nearby, a page starts at a tool call
// rather than at the result whose call would sit on the previous page.
func TestPageStartsAtToolCallInLongTurn(t *testing.T) {
	_, ts, st := setup(t, noRun)
	x := createSession(t, ts)
	_, _ = st.Append(x.ID, "user", "go", nil)
	for i := range 300 {
		_, _ = st.Append(x.ID, "tool", "", json.RawMessage(fmt.Sprintf(`{"id":"t%d","name":"Read","input":{}}`, i)))
		_, _ = st.Append(x.ID, "tool_result", "", json.RawMessage(fmt.Sprintf(`{"tool_use_id":"t%d","content":"ok"}`, i)))
	}
	for _, limit := range []int{100, 101} {
		p := getPage(t, ts, fmt.Sprintf("%s/api/sessions/%s?tail=%d", ts.URL, x.ID, limit))
		if !p.HasMore || p.Events[0].Kind != "tool" || len(p.Events) < limit || len(p.Events) > limit+1 {
			t.Fatalf("limit %d: %d events starting with %s", limit, len(p.Events), p.Events[0].Kind)
		}
	}
	events := make([]store.Event, 0, 601)
	all, _ := st.Events(x.ID, 0)
	events = append(events, all...)
	if page, _ := pageSlice(renumber(events), 0, 101, pageReach); page[0].Kind != "tool" {
		t.Fatalf("in-memory page starts with %s", page[0].Kind)
	}
}

// renumber gives events the index+1 seqs of an in-memory transcript.
func renumber(events []store.Event) []store.Event {
	for i := range events {
		events[i].Seq = int64(i + 1)
	}
	return events
}

// A question's result stays whole when its call is on the page; numbers in a
// cut payload keep their exact value.
func TestTrimKeepsQuestionResultsAndNumbers(t *testing.T) {
	long := strings.Repeat("a", trimLimit+10)
	call, _ := json.Marshal(map[string]any{"id": "q1", "name": "AskUserQuestion", "input": map[string]any{}})
	result, _ := json.Marshal(map[string]any{"tool_use_id": "q1", "content": long})
	other, _ := json.Marshal(map[string]any{"tool_use_id": "x", "content": long, "big": json.Number("9007199254740993")})
	out := trimEvents([]store.Event{{Kind: "tool", Payload: call}, {Kind: "tool_result", Payload: result}, {Kind: "tool_result", Payload: other}})
	if out[1].Truncated || !out[2].Truncated {
		t.Fatalf("question result truncated=%v, other truncated=%v", out[1].Truncated, out[2].Truncated)
	}
	if !strings.Contains(string(out[2].Payload), "9007199254740993") {
		t.Fatalf("number changed: %s", out[2].Payload[:80])
	}
}

// A poll past the end of a native transcript (rewritten shorter) asks the web to reload.
func TestNativePollPastEndConflicts(t *testing.T) {
	s, ts, _ := setup(t, noRun)
	id := store.ID()
	dir := filepath.Join(s.cfg.ClaudeConfigDir, "projects", "short")
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(`{"type":"user","message":{"content":"q"}}`+"\n"), 0600)
	if code, _ := request(t, ts, "GET", "/api/native/sessions/"+id+"/events?after=1", nil, testToken); code != 200 {
		t.Fatalf("poll at end: %d", code)
	}
	if code, _ := request(t, ts, "GET", "/api/native/sessions/"+id+"/events?after=5", nil, testToken); code != 409 {
		t.Fatalf("poll past end: %d", code)
	}
}

// Responses beyond the buffer limit stream through without ETag or gzip.
func TestHugeResponsesStream(t *testing.T) {
	_, ts, st := setup(t, noRun)
	x := createSession(t, ts)
	_, _ = st.Append(x.ID, "assistant", strings.Repeat("z", maxBuffered+1024), nil)
	req := authed(t, ts.URL+"/api/sessions/"+x.ID)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("ETag") != "" || resp.Header.Get("Content-Encoding") != "" || n < maxBuffered {
		t.Fatalf("status %d etag %q encoding %q bytes %d", resp.StatusCode, resp.Header.Get("ETag"), resp.Header.Get("Content-Encoding"), n)
	}
}

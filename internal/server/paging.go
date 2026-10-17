package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"unicode/utf8"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

// Transcripts load from the end: the web asks for the latest page
// (?tail=N) and fetches older pages (?before=seq) only when the reader scrolls
// up. Listings may cut long tool text (?compact=1); a cut event is marked
// truncated and the web fetches it whole (/events/{seq}) when expanded.
const (
	pageSize  = 300
	maxPage   = 2000
	pageReach = 100  // how far back a page may extend to start at a turn
	trimLimit = 2000 // bytes kept of each long string in a listed tool event
)

// wholeTools render their full input as a card (questions, plans); never cut them.
var wholeTools = map[string]bool{"AskUserQuestion": true, "ExitPlanMode": true}

// pageLimit reads a page size from the query; ok is false when it is malformed.
func pageLimit(raw string) (int, bool) {
	if raw == "" {
		return pageSize, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, false
	}
	return min(n, maxPage), true
}

func cursor(raw string) (int64, bool) {
	if raw == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	return n, err == nil && n >= 0
}

func trimEvents(events []store.Event) []store.Event {
	// Results of question and plan tools render in their cards; keep them whole
	// when their call is on the page (the web loads others when it sees them).
	whole := map[string]bool{}
	for _, e := range events {
		if e.Kind != "tool" {
			continue
		}
		var call struct{ ID, Name string }
		if json.Unmarshal(e.Payload, &call) == nil && wholeTools[call.Name] {
			whole[call.ID] = true
		}
	}
	out := make([]store.Event, len(events))
	for i, e := range events {
		out[i] = trimEvent(e, whole)
	}
	return out
}

func trimEvent(e store.Event, whole map[string]bool) store.Event {
	if (e.Kind != "tool" && e.Kind != "tool_result") || (len(e.Text) <= trimLimit && len(e.Payload) <= trimLimit) {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(e.Payload))
	d.UseNumber() // keep large integers exact
	var v any
	if d.Decode(&v) != nil {
		return e
	}
	if m, ok := v.(map[string]any); ok {
		if name, _ := m["name"].(string); e.Kind == "tool" && wholeTools[name] {
			return e
		}
		if id, _ := m["tool_use_id"].(string); e.Kind == "tool_result" && whole[id] {
			return e
		}
	}
	cut := false
	v = trimValue(v, &cut)
	if len(e.Text) > trimLimit {
		e.Text, cut = clipString(e.Text), true
	}
	if !cut {
		return e
	}
	if raw, err := json.Marshal(v); err == nil {
		e.Payload = raw
	}
	e.Truncated = true
	return e
}

func trimValue(v any, cut *bool) any {
	switch x := v.(type) {
	case string:
		if len(x) > trimLimit {
			*cut = true
			return clipString(x)
		}
	case []any:
		for i := range x {
			x[i] = trimValue(x[i], cut)
		}
	case map[string]any:
		for k := range x {
			x[k] = trimValue(x[k], cut)
		}
	}
	return v
}

func clipString(s string) string {
	n := trimLimit
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "\n…"
}

// pageSlice is EventsPage for an in-memory transcript whose seq is index+1.
func pageSlice(events []store.Event, before int64, limit, reach int) ([]store.Event, bool) {
	end := len(events)
	if before > 0 && before-1 < int64(end) {
		end = int(before - 1)
	}
	start := max(0, end-limit)
	if start > 0 {
		floor := max(0, end-limit-reach)
		user, call := -1, -1
		for i := start; i >= floor && user < 0; i-- {
			if events[i].Kind == "user" {
				user = i
			} else if call < 0 && events[i].Kind != "tool_result" {
				call = i
			}
		}
		if user >= 0 {
			start = user
		} else if call >= 0 {
			start = call
		}
	}
	return events[start:end], start > 0
}

func nativeEvents(x claude.NativeSession, ev []claude.Event) []store.Event {
	out := make([]store.Event, 0, len(ev))
	for i, v := range ev {
		raw := v.Payload
		if len(raw) == 0 {
			raw = json.RawMessage(`null`)
		}
		out = append(out, store.Event{Seq: int64(i + 1), SessionID: x.ID, Kind: v.Kind, Text: v.Text, Payload: raw, CreatedAt: x.UpdatedAt})
	}
	return out
}

// listEvents answers an events request over a transcript held in memory:
// ?before= gives an older page, otherwise events after ?after= (a poll).
func (s *Server) listEvents(w http.ResponseWriter, r *http.Request, events []store.Event) {
	q := r.URL.Query()
	compact := q.Get("compact") == "1"
	shape := func(x []store.Event) []store.Event {
		if compact {
			return trimEvents(x)
		}
		return x
	}
	if raw := q.Get("before"); raw != "" {
		before, ok := cursor(raw)
		limit, okLimit := pageLimit(q.Get("limit"))
		if !ok || !okLimit {
			fail(w, 400, s.lang(r).T("Invalid paging parameters", "无效的分页参数"))
			return
		}
		page, older := pageSlice(events, before, limit, pageReach)
		respond(w, 200, map[string]any{"events": shape(page), "hasMore": older})
		return
	}
	after, ok := cursor(q.Get("after"))
	if !ok {
		fail(w, 400, s.lang(r).T("Invalid event cursor", "无效的事件游标"))
		return
	}
	if after > int64(len(events)) {
		// The transcript was rewritten shorter and renumbered; the web reloads it.
		fail(w, 409, s.lang(r).T("The session history changed; reload", "会话记录已变化，请重新加载"))
		return
	}
	respond(w, 200, shape(events[after:]))
}

func (s *Server) eventAt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil {
		fail(w, 400, s.lang(r).T("Invalid event sequence number", "无效的事件序号"))
		return
	}
	e, err := s.store.Event(id, seq)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, s.lang(r).T("Event not found", "事件不存在"))
		return
	}
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not read messages", "无法读取消息"))
		return
	}
	respond(w, 200, e)
}

func (s *Server) nativeEventsList(w http.ResponseWriter, r *http.Request) {
	x, ev, e := claude.NativeHistory(s.cfg.ClaudeConfigDir, r.PathValue("id"))
	if e != nil {
		fail(w, 404, s.lang(r).T("Could not read the native session: ", "无法读取原生会话：")+e.Error())
		return
	}
	s.listEvents(w, r, nativeEvents(x, ev))
}

func (s *Server) nativeEventAt(w http.ResponseWriter, r *http.Request) {
	x, ev, e := claude.NativeHistory(s.cfg.ClaudeConfigDir, r.PathValue("id"))
	if e != nil {
		fail(w, 404, s.lang(r).T("Could not read the native session: ", "无法读取原生会话：")+e.Error())
		return
	}
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil || seq < 1 || seq > int64(len(ev)) {
		fail(w, 404, s.lang(r).T("Event not found", "事件不存在"))
		return
	}
	event := nativeEvents(x, ev[seq-1:seq])[0]
	event.Seq = seq
	respond(w, 200, event)
}

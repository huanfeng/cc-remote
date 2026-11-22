package server

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
	"cc-remote/internal/store"
)

const searchPageSize = 30

type messageHit struct {
	SessionID  string `json:"sessionId"`
	Native     string `json:"native,omitempty"`
	Provider   string `json:"provider"`
	Title      string `json:"title"`
	Cwd        string `json:"cwd"`
	Kind       string `json:"kind"`
	CreatedAt  string `json:"createdAt"`
	Snippet    string `json:"snippet"`
	MatchStart int    `json:"matchStart"`
	MatchEnd   int    `json:"matchEnd"`
	Locator    string `json:"locator"`
}

type searchPosition struct {
	At        time.Time
	Native    string
	SessionID string
	Locator   string
}

func (h messageHit) position() searchPosition {
	at, _ := time.Parse(time.RFC3339Nano, h.CreatedAt)
	return searchPosition{at, h.Native, h.SessionID, h.Locator}
}

func comparePositions(a, b searchPosition) int {
	return cmp.Or(b.At.Compare(a.At), cmp.Compare(a.Native, b.Native),
		cmp.Compare(a.SessionID, b.SessionID), cmp.Compare(a.Locator, b.Locator))
}

type rankedHit struct {
	messageHit
	position searchPosition
}

func compareHits(a, b rankedHit) int { return comparePositions(a.position, b.position) }

func searchProvider(value string) bool { return value == "" || value == "claude" || value == "codex" }

func (s *Server) searchMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	phrase := strings.TrimSpace(q.Get("q"))
	id, native, provider, cwd := q.Get("sessionId"), q.Get("native"), q.Get("provider"), q.Get("cwd")
	badSearch := func() { fail(w, 400, s.lang(r).T("Invalid search parameters", "无效的搜索参数")) }
	if phrase == "" || utf8.RuneCountInString(phrase) > 128 || !searchProvider(native) || !searchProvider(provider) || native != "" && id == "" {
		badSearch()
		return
	}
	var since time.Time
	if raw := q.Get("since"); raw != "" {
		var err error
		if since, err = time.Parse(time.RFC3339Nano, raw); err != nil {
			badSearch()
			return
		}
	}
	var before *searchPosition
	if raw := q.Get("before"); raw != "" {
		data, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || json.Unmarshal(data, &before) != nil || before == nil || before.SessionID == "" || before.Locator == "" {
			badSearch()
			return
		}
	}
	pattern := regexp.MustCompile("(?i)" + regexp.QuoteMeta(phrase))
	ranked := make([]rankedHit, 0, searchPageSize+1)
	add := func(hit messageHit, body string) {
		pos := hit.position()
		if (!since.IsZero() && pos.At.Before(since)) || (before != nil && comparePositions(pos, *before) <= 0) {
			return
		}
		if len(ranked) > searchPageSize && comparePositions(pos, ranked[len(ranked)-1].position) >= 0 {
			return
		}
		match := pattern.FindStringIndex(body)
		if match == nil {
			return
		}
		hit.Snippet, hit.MatchStart, hit.MatchEnd = matchedSnippet(body, match[0], match[1])
		entry := rankedHit{hit, pos}
		i, found := slices.BinarySearchFunc(ranked, entry, compareHits)
		if !found {
			ranked = slices.Insert(ranked, i, entry)
			if len(ranked) > searchPageSize+1 {
				ranked = ranked[:searchPageSize+1]
			}
		}
	}
	ctx := r.Context()
	if native == "" {
		scope := store.MessageSearchScope{SessionID: id, Cwd: cwd, Provider: provider}
		err := s.store.VisitMessages(ctx, scope, func(e store.SearchMessage) {
			add(messageHit{SessionID: e.SessionID, Provider: e.Provider, Title: e.Title, Cwd: e.Cwd,
				Kind: e.Kind, CreatedAt: e.CreatedAt, Locator: strconv.FormatInt(e.Seq, 10)}, e.Text)
		})
		if err != nil {
			fail(w, 500, s.lang(r).T("Could not search messages", "无法搜索消息"))
			return
		}
	}
	nativeHit := func(source string, n claude.NativeSession, e claude.Event, locator string) messageHit {
		at := e.CreatedAt
		if at == "" {
			at = n.LastConversationAt
		}
		return messageHit{SessionID: n.ID, Native: source, Provider: source, Title: n.Title, Cwd: n.Cwd,
			Kind: e.Kind, CreatedAt: at, Locator: locator}
	}
	partial := false
	switch {
	case native != "":
		// Metadata is finalized by the provider after walking the file. The
		// hit order uses the original message timestamp and native identity.
		search, dir := claude.SearchNative, s.cfg.ClaudeConfigDir
		if native == "codex" {
			home, err := s.codexNativeHomeForThread(ctx, id)
			if err != nil {
				fail(w, 404, s.lang(r).T("Could not search this native history", "无法搜索这段原生历史"))
				return
			}
			search, dir = codex.SearchNative, home.dir
		}
		n, err := search(ctx, dir, id, func(e claude.Event, locator string) bool {
			add(nativeHit(native, claude.NativeSession{ID: id}, e, locator), e.Text)
			return ctx.Err() == nil
		})
		if err != nil {
			fail(w, 404, s.lang(r).T("Could not search this native history", "无法搜索这段原生历史"))
			return
		}
		for i := range ranked {
			ranked[i].Title, ranked[i].Cwd = n.Title, n.Cwd
		}
	case id == "":
		include := func(n claude.NativeSession) bool { return cwd == "" || n.Cwd == cwd }
		for _, source := range []string{"claude", "codex"} {
			if provider != "" && source != provider {
				continue
			}
			visit := func(n claude.NativeSession, e claude.Event, locator string) bool {
				add(nativeHit(source, n, e, locator), e.Text)
				return ctx.Err() == nil
			}
			if source == "codex" {
				homes, err := s.codexNativeHomes()
				partial = partial || err != nil
				threadCounts := map[string]int{}
				if len(homes) > 1 {
					for _, home := range homes {
						threads, err := codex.ListNative(home.dir)
						partial = partial || err != nil
						for _, thread := range threads {
							threadCounts[strings.ToLower(thread.ID)]++
						}
					}
				}
				codexInclude := func(n claude.NativeSession) bool {
					return threadCounts[strings.ToLower(n.ID)] <= 1 && include(n)
				}
				for _, home := range homes {
					err := codex.SearchNativeAll(ctx, home.dir, codexInclude, visit)
					partial = partial || err != nil
				}
				continue
			}
			err := claude.SearchNativeAll(ctx, s.cfg.ClaudeConfigDir, include, visit)
			partial = partial || err != nil
		}
	}
	if ctx.Err() != nil {
		return
	}
	var next string
	if len(ranked) > searchPageSize {
		ranked = ranked[:searchPageSize]
		data, _ := json.Marshal(ranked[len(ranked)-1].position)
		next = base64.RawURLEncoding.EncodeToString(data)
	}
	hits := make([]messageHit, 0, len(ranked))
	for _, entry := range ranked {
		hits = append(hits, entry.messageHit)
	}
	respond(w, 200, struct {
		Hits    []messageHit `json:"hits"`
		Next    string       `json:"next,omitempty"`
		Partial bool         `json:"partial,omitempty"`
	}{hits, next, partial})
}

func matchedSnippet(body string, start, end int) (string, int, int) {
	left, right := start, end
	for i := 0; i < 70 && left > 0; i++ {
		_, size := utf8.DecodeLastRuneInString(body[:left])
		left -= size
	}
	for i := 0; i < 100 && right < len(body); i++ {
		_, size := utf8.DecodeRuneInString(body[right:])
		right += size
	}
	prefix, suffix := "", ""
	if left > 0 {
		prefix = "…"
	}
	if right < len(body) {
		suffix = "…"
	}
	matchStart := utf16Length(prefix) + utf16Length(body[left:start])
	return prefix + body[left:right] + suffix, matchStart, matchStart + utf16Length(body[start:end])
}

func utf16Length(value string) int {
	length := 0
	for _, r := range value {
		length++
		if r > 0xffff {
			length++
		}
	}
	return length
}

func (s *Server) searchContext(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, native, locator := q.Get("sessionId"), q.Get("native"), q.Get("locator")
	if id == "" || locator == "" || !searchProvider(native) {
		fail(w, 400, s.lang(r).T("Invalid message location", "无效的消息位置"))
		return
	}
	var events []store.Event
	var seq int64
	var err error
	if native == "" {
		seq, err = strconv.ParseInt(locator, 10, 64)
		if err != nil || seq <= 0 {
			fail(w, 400, s.lang(r).T("Invalid message location", "无效的消息位置"))
			return
		}
		events, err = s.store.MessageContext(r.Context(), id, seq)
	} else {
		contextFor, dir := claude.NativeSearchContext, s.cfg.ClaudeConfigDir
		if native == "codex" {
			home, err := s.codexNativeHomeForThread(r.Context(), id)
			if err != nil {
				fail(w, 404, s.lang(r).T("This message is no longer available", "此消息已不可用"))
				return
			}
			contextFor, dir = codex.NativeSearchContext, home.dir
		}
		n, messages, at, readErr := contextFor(r.Context(), dir, id, locator)
		err = readErr
		if err == nil && at >= 0 && at < len(messages) {
			events, seq = nativeEvents(n, messages), int64(at+1)
		}
	}
	if err != nil || !slices.ContainsFunc(events, func(e store.Event) bool { return e.Seq == seq }) {
		fail(w, 404, s.lang(r).T("This message is no longer available", "此消息已不可用"))
		return
	}
	respond(w, 200, struct {
		Events   []store.Event `json:"events"`
		MatchSeq int64         `json:"matchSeq"`
	}{events, seq})
}

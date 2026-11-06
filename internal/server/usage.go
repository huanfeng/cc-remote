package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

// Subscription usage (the 5-hour and weekly limits of a Claude plan), as
// Claude Code's /usage shows it. Only an official Claude subscription login
// has it: Claude Code pointed at another provider, or using an API key, does not.
const (
	usageFresh        = time.Minute      // a page polling sooner gets the last reading
	usageSampleEvery  = 10 * time.Minute // the host reads it this often for the history
	usageSampleGap    = 10 * time.Minute // an unchanged reading is recorded at most this often
	usageKeep         = 30 * 24 * time.Hour
	usageTimeout      = 10 * time.Second
	usageMaxBackoff   = 30 * time.Minute
	usageExpiryMargin = 30 * time.Second
	// A token the usage endpoint refuses is read through a one-token inference
	// call instead; that costs a request, so it is made rarely.
	usageProbeEvery = 5 * time.Minute
)

// usageClient follows no redirects: the login token goes to the usage endpoint only.
var usageClient = &http.Client{
	Timeout:       usageTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// usageCache is the last reading and when to try again. Its mutex is never
// s.mu: a slow request only delays other usage readers.
type usageCache struct {
	mu        sync.Mutex
	loaded    bool // the last reading was taken from the history once
	last      *claude.Usage
	fetchedAt time.Time
	recorded  time.Time // the latest history sample
	err       string    // why the latest attempt failed: "expired", "rate-limited" or a message
	retryAt   time.Time
	backoff   time.Duration
	probe     bool   // the token lacks the usage scope: read the rate limit headers
	tokenKey  string // fingerprint of the token the state above belongs to
}

type usageReply struct {
	Available bool `json:"available"`
	// Reason says why there is none: "third-party" (another provider),
	// "api-key" (billed per token) or "no-login".
	Reason   string        `json:"reason,omitempty"`
	Provider string        `json:"provider,omitempty"`
	Plan     string        `json:"plan,omitempty"`
	// Profile and Account name the OAuth token profile the reading belongs to
	// (empty for the host's own login).
	Profile string        `json:"profile,omitempty"`
	Account string        `json:"account,omitempty"`
	Usage   *claude.Usage `json:"usage,omitempty"`
	// FetchedAt is when Usage was read; Stale marks that the latest attempt
	// failed (Error says why), so it may be out of date.
	FetchedAt *time.Time `json:"fetchedAt,omitempty"`
	Stale     bool       `json:"stale,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// usageSource tells whether Claude Code uses an official subscription login
// and returns it; otherwise reason (and provider) say what it uses instead.
// Like Claude Code, the host environment wins over the user settings' env.
func (s *Server) usageSource() (token claude.OAuthToken, reason, provider string) {
	user, _, _ := readModelSettings(i18n.Current(), filepath.Join(s.cfg.ClaudeConfigDir, "settings.json"))
	env := func(key string) string {
		if v, ok := os.LookupEnv(key); ok {
			return strings.TrimSpace(v)
		}
		return strings.TrimSpace(user.envString(key))
	}
	for _, p := range []struct{ key, name string }{{"CLAUDE_CODE_USE_BEDROCK", "Amazon Bedrock"}, {"CLAUDE_CODE_USE_VERTEX", "Google Vertex AI"}, {"CLAUDE_CODE_USE_FOUNDRY", "Microsoft Foundry"}} {
		if v := env(p.key); v != "" && v != "0" && !strings.EqualFold(v, "false") {
			return token, "third-party", p.name
		}
	}
	if base := env("ANTHROPIC_BASE_URL"); base != "" {
		u, err := url.Parse(base)
		host := ""
		if err == nil {
			host = strings.ToLower(u.Hostname())
		}
		if host != "api.anthropic.com" {
			if host == "" {
				host = base
			}
			return token, "third-party", host
		}
	}
	if env("ANTHROPIC_API_KEY") != "" || env("ANTHROPIC_AUTH_TOKEN") != "" {
		return token, "api-key", ""
	}
	token, err := claude.ReadOAuthToken(s.cfg.ClaudeConfigDir)
	if err != nil {
		return token, "no-login", ""
	}
	return token, "", ""
}

var claudeSemver = regexp.MustCompile(`^\d+\.\d+\.\d+[A-Za-z0-9.+-]*`)

// readUsage returns the usage, reading it again when the last reading is
// older than maxAge (and no backoff holds it off).
func (s *Server) readUsage(maxAge time.Duration) usageReply {
	token, reason, provider := s.usageSource()
	if reason != "" {
		return usageReply{Reason: reason, Provider: provider}
	}
	return s.readUsageOf("", &s.usageCache, token, maxAge)
}

// readUsageOf reads one login's usage through its own cache. source is empty
// for the host's login, else the OAuth profile ID.
func (s *Server) readUsageOf(source string, c *usageCache, token claude.OAuthToken, maxAge time.Duration) usageReply {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		c.loaded = true
		if samples, err := s.usageSamples(source, time.Now().Add(-usageKeep)); err == nil && len(samples) > 0 {
			last := samples[len(samples)-1]
			var u claude.Usage
			if json.Unmarshal(last.Data, &u) == nil {
				c.last, c.fetchedAt, c.recorded = &u, last.At, last.At
			}
		}
	}
	now := time.Now()
	if key := tokenFingerprint(token.AccessToken, nil); key != c.tokenKey {
		// A replaced token starts over: it may have the scope the old one lacked.
		c.tokenKey, c.probe, c.err, c.retryAt, c.backoff, c.fetchedAt = key, false, "", time.Time{}, 0, time.Time{}
	}
	if c.probe {
		maxAge = max(maxAge, usageProbeEvery)
	}
	if (c.last == nil || now.Sub(c.fetchedAt) >= maxAge) && !now.Before(c.retryAt) {
		s.fetchUsageLocked(source, c, token, now)
	}
	reply := usageReply{Available: true, Plan: token.Plan, Usage: c.last, Error: c.err, Stale: c.err != ""}
	if c.last != nil {
		at := c.fetchedAt
		reply.FetchedAt = &at
	}
	return reply
}

func (s *Server) fetchUsageLocked(source string, c *usageCache, token claude.OAuthToken, now time.Time) {
	// An expired login is renewed by Claude Code when it next runs; the host
	// never refreshes it (that would sign the running Claude Code out).
	if !token.ExpiresAt.IsZero() && token.ExpiresAt.Before(now.Add(usageExpiryMargin)) {
		c.err, c.retryAt = "expired", now.Add(usageFresh)
		return
	}
	agent := ""
	if v := claudeSemver.FindString(s.claudeVersion().Version); v != "" {
		agent = "claude-code/" + v
	}
	ctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
	defer cancel()
	var u claude.Usage
	var err error
	if !c.probe {
		u, err = claude.FetchUsage(ctx, usageClient, token.AccessToken, agent)
	}
	var status claude.UsageStatusError
	if refused := errors.As(err, &status); c.probe || refused && (status.Status == http.StatusForbidden || status.Status == http.StatusTooManyRequests) {
		// A setup-token has no user:profile scope: 403 here, but its limits
		// are in the headers of an inference reply. That endpoint also answers
		// 429 for long stretches (its limit is separate from the account's), so
		// the headers stand in then too; only a 403 means "always use them".
		forbidden := c.probe || refused && status.Status == http.StatusForbidden
		fromHeaders, headerErr := claude.FetchUsageFromHeaders(ctx, usageClient, token.AccessToken, agent)
		if headerErr == nil {
			u, err, c.probe = fromHeaders, nil, forbidden
		} else if forbidden {
			err = headerErr
		}
	}
	switch {
	case err == nil:
		c.err, c.backoff, c.retryAt = "", 0, time.Time{}
		changed := c.last == nil || !sameUsage(*c.last, u)
		c.last, c.fetchedAt = &u, now
		if changed || now.Sub(c.recorded) >= usageSampleGap {
			if data, e := json.Marshal(u); e == nil {
				if e = s.addUsageSample(source, now, data); e != nil {
					log.Printf("usage history: %v", e)
				} else {
					c.recorded = now
				}
			}
		}
	case errors.As(err, &status) && status.Status == http.StatusTooManyRequests:
		c.backoff = min(max(5*time.Minute, c.backoff*2), usageMaxBackoff)
		c.err, c.retryAt = "rate-limited", now.Add(c.backoff)
	case errors.As(err, &status) && status.Status == http.StatusUnauthorized:
		c.err, c.retryAt = "expired", now.Add(5*time.Minute)
	default:
		c.err, c.retryAt = err.Error(), now.Add(usageFresh)
	}
}

func sameUsage(a, b claude.Usage) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// sampleUsage reads the usage now and then, so the history has no gaps while
// no page is open.
func (s *Server) sampleUsage(stop <-chan struct{}, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		s.readUsage(every - time.Minute)
		entries, _ := loadOAuthEntries(s.cfg.DataDir)
		for _, e := range entries {
			s.readOAuthUsage(oauthIDPrefix+e.ID, every-time.Minute)
		}
	}
}

func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	provider, ok := requestProvider(w, r)
	if !ok {
		return
	}
	if provider == "codex" {
		s.codexUsage(w, r)
		return
	}
	if id := r.URL.Query().Get("sessionId"); id != "" {
		x, err := s.store.Get(id)
		if err != nil || x.Provider == "codex" {
			fail(w, 404, "Claude session not found")
			return
		}
		if strings.HasPrefix(x.ProfileID, oauthIDPrefix) {
			respond(w, 200, s.readOAuthUsage(x.ProfileID, usageFresh))
			return
		}
		if x.ProfileID != "" {
			respond(w, 200, usageReply{Reason: "third-party", Provider: x.ProfileName})
			return
		}
	}
	if profile := r.URL.Query().Get("profile"); profile != "" {
		if !strings.HasPrefix(profile, oauthIDPrefix) {
			fail(w, 404, "OAuth profile not found")
			return
		}
		respond(w, 200, s.readOAuthUsage(profile, usageFresh))
		return
	}
	respond(w, 200, s.readUsage(usageFresh))
}

type usagePoint struct {
	At       time.Time `json:"at"`
	FiveHour *float64  `json:"fiveHour,omitempty"`
	Weekly   *float64  `json:"weekly,omitempty"`
}

// usageHistory is GET /api/usage/history?hours=N (default 24, at most 30 days).
func (s *Server) usageHistory(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 {
		hours = 24
	}
	hours = min(hours, int(usageKeep/time.Hour))
	samples, err := s.usageSamples(r.URL.Query().Get("profile"), time.Now().Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not read the usage history", "无法读取用量历史"))
		return
	}
	points := []usagePoint{}
	for _, x := range samples {
		var u claude.Usage
		if json.Unmarshal(x.Data, &u) != nil {
			continue
		}
		p := usagePoint{At: x.At}
		if u.FiveHour != nil {
			p.FiveHour = &u.FiveHour.Percent
		}
		if u.Weekly != nil {
			p.Weekly = &u.Weekly.Percent
		}
		points = append(points, p)
	}
	respond(w, 200, map[string]any{"hours": hours, "points": points})
}

// accountUsage holds one usageCache per OAuth profile. A profile's usage is
// that of its own account, so it never shares a reading with the host's login.
type accountUsage struct {
	mu     sync.Mutex
	caches map[string]*usageCache
}

func (a *accountUsage) get(id string) *usageCache {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.caches == nil {
		a.caches = map[string]*usageCache{}
	}
	if a.caches[id] == nil {
		a.caches[id] = &usageCache{}
	}
	return a.caches[id]
}

func (a *accountUsage) forget(id string) {
	a.mu.Lock()
	delete(a.caches, id)
	a.mu.Unlock()
}

// readOAuthUsage reads the usage of an OAuth profile's account. The token is
// a long-lived one from claude setup-token, so it has no expiry to check.
func (s *Server) readOAuthUsage(id string, maxAge time.Duration) usageReply {
	entry, ok, err := findOAuthEntry(s.cfg.DataDir, strings.TrimPrefix(id, oauthIDPrefix))
	if err != nil || !ok {
		s.accountUsage.forget(id)
		return usageReply{Reason: "no-login"}
	}
	reply := s.readUsageOf(id, s.accountUsage.get(id), claude.OAuthToken{AccessToken: entry.Token}, maxAge)
	reply.Profile, reply.Account = id, entry.Name
	return reply
}

func (s *Server) usageSamples(source string, since time.Time) ([]store.UsageSample, error) {
	if source == "" {
		return s.store.UsageSamples(since)
	}
	return s.store.AccountUsageSamples(source, since)
}

func (s *Server) addUsageSample(source string, at time.Time, data json.RawMessage) error {
	if source == "" {
		return s.store.AddUsageSample(at, data, usageKeep)
	}
	return s.store.AddAccountUsageSample(source, at, data, usageKeep)
}

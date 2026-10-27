package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
)

type codexUsageReply struct {
	codex.SubscriptionUsage
	FetchedAt *time.Time `json:"fetchedAt,omitempty"`
	Stale     bool       `json:"stale,omitempty"`
	Error     string     `json:"error,omitempty"`
}
type codexUsageCache struct {
	mu      sync.Mutex
	key     string
	reply   codexUsageReply
	retryAt time.Time
	fetch   func(context.Context, claude.Options) (codex.SubscriptionUsage, error)
}

func (s *Server) codexUsage(w http.ResponseWriter, r *http.Request) {
	o := claude.Options{Executable: s.cfg.CodexExecutable, ConfigDir: s.cfg.CodexConfigDir, Cwd: s.cfg.Cwd}
	if id := r.URL.Query().Get("sessionId"); id != "" {
		x, err := s.store.Get(id)
		if err != nil || x.Provider != "codex" {
			fail(w, 404, "Codex session not found")
			return
		}
		o.Cwd = x.Cwd
		if x.ProfileID != "" {
			// Supplier profiles carry API credentials and their own accounting.
			respond(w, 200, codexUsageReply{SubscriptionUsage: codex.SubscriptionUsage{Reason: "third-party", Provider: x.ProfileName}})
			return
		}
	}
	c := &s.codexUsageCache
	c.mu.Lock()
	defer c.mu.Unlock()
	key := o.ConfigDir + "\x00" + o.Executable + "\x00" + o.Cwd
	if c.key != key {
		c.key, c.reply, c.retryAt = key, codexUsageReply{}, time.Time{}
	}
	now := time.Now().UTC()
	if !now.Before(c.retryAt) {
		fetch := c.fetch
		if fetch == nil {
			fetch = codex.ReadSubscriptionUsage
		}
		ctx, cancel := context.WithTimeout(r.Context(), usageTimeout)
		usage, err := fetch(ctx, o)
		cancel()
		c.retryAt = now.Add(usageFresh)
		if err != nil {
			c.reply.Stale, c.reply.Error = true, "request-failed"
		} else {
			c.reply = codexUsageReply{SubscriptionUsage: usage, FetchedAt: &now}
		}
	}
	respond(w, 200, c.reply)
}

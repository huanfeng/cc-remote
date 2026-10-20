package server

import (
	"net/http"
	"slices"

	"cc-remote/internal/listen"
)

// allowOrigins are the standalone web frontends this host lets in: the ones
// it started with, until a reload of the service settings replaces them.
func (s *Server) allowOrigins() []string {
	if p := s.origins.Load(); p != nil {
		return *p
	}
	return s.cfg.AllowOrigins
}

// SetAllowOrigins replaces the allowed standalone web frontends (normalized).
func (s *Server) SetAllowOrigins(origins []string) {
	s.origins.Store(&origins)
}

// allowedOrigin reports whether origin is a standalone web frontend this host
// lets in (allowOrigins): the page lives elsewhere and calls the API with the
// token like the host's own page does.
func (s *Server) allowedOrigin(origin string) bool {
	origin = listen.NormalizeOrigin(origin)
	return origin != "" && slices.Contains(s.allowOrigins(), origin)
}

// cors answers an allowed origin's preflight and marks its API replies as
// readable by it. It reports true when the request is fully handled.
func (s *Server) cors(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || !s.allowedOrigin(origin) {
		return false
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Add("Vary", "Origin")
	h.Set("Access-Control-Expose-Headers", "ETag, X-CC-Path")
	if r.Method != http.MethodOptions || r.Header.Get("Access-Control-Request-Method") == "" {
		return false
	}
	h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CC-Lang, If-None-Match")
	h.Set("Access-Control-Max-Age", "600")
	// A page on a public site calling a host on a private address (Chrome's
	// Private Network Access) needs this answer too.
	if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
		h.Set("Access-Control-Allow-Private-Network", "true")
	}
	w.WriteHeader(http.StatusNoContent)
	return true
}

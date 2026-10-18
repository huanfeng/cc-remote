package server

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// NormalizeOrigin turns "https://Example.com/" into "https://example.com", the
// form a browser sends in Origin; "" when s is not an http(s) origin.
func NormalizeOrigin(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	// Browsers leave a default port out of Origin.
	if port := u.Port(); (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		host = strings.TrimSuffix(host, ":"+port)
	}
	return u.Scheme + "://" + host
}

// allowedOrigin reports whether origin is a standalone web frontend this host
// lets in (Config.AllowOrigins): the page lives elsewhere and calls the API
// with the token like the host's own page does.
func (s *Server) allowedOrigin(origin string) bool {
	origin = NormalizeOrigin(origin)
	return origin != "" && slices.Contains(s.cfg.AllowOrigins, origin)
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
	h.Set("Access-Control-Expose-Headers", "ETag")
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

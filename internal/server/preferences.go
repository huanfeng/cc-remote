package server

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"cc-remote/internal/i18n"
	"cc-remote/internal/prefs"
	"cc-remote/internal/store"
)

type preferencesBody struct {
	prefs.Preferences
	Revision         string `json:"revision"`
	ConfirmDangerous bool   `json:"confirmDangerous,omitempty"`
}

// Called under s.configMu.
func (s *Server) savePreferences(l i18n.Lang, next prefs.Preferences, revision string, confirm bool) (preferencesBody, error) {
	old, _, err := prefs.LoadProvider(s.cfg.DataDir, next.Provider)
	if err != nil {
		return preferencesBody{}, err
	}
	if next.PermissionMode != old.PermissionMode && prefs.Dangerous(next.PermissionMode) && !confirm {
		return preferencesBody{}, errConfirmDangerous(l)
	}
	saved, rev, err := prefs.SaveProvider(s.cfg.DataDir, next.Provider, next, revision)
	return preferencesBody{Preferences: saved, Revision: rev}, err
}

func (s *Server) preferences(w http.ResponseWriter, r *http.Request) {
	provider, ok := requestProvider(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		s.configMu.Lock()
		p, rev, err := prefs.LoadProvider(s.cfg.DataDir, provider)
		s.configMu.Unlock()
		if err != nil {
			fail(w, 500, i18n.Text(s.lang(r), err))
			return
		}
		respond(w, 200, preferencesBody{Preferences: p, Revision: rev})
		return
	}
	var req preferencesBody
	if !decode(w, r, &req) {
		return
	}
	req.Provider = provider
	s.configMu.Lock()
	result, err := s.savePreferences(s.lang(r), req.Preferences, req.Revision, req.ConfirmDangerous)
	s.configMu.Unlock()
	if err != nil {
		status := 400
		if errors.Is(err, prefs.ErrConflict) {
			status = 409
		}
		fail(w, status, i18n.Text(s.lang(r), err))
		return
	}
	respond(w, 200, result)
}

type overrideRequest struct {
	PermissionMode   *string `json:"permissionMode"`
	Model            *string `json:"model"`
	Effort           *string `json:"effort"`
	ConfirmDangerous bool    `json:"confirmDangerous"`
}

// apply merges the requested fields into x and validates the result.
func (o overrideRequest) apply(l i18n.Lang, x *store.Session) error {
	before := x.PermissionMode
	if o.PermissionMode != nil {
		x.PermissionMode = *o.PermissionMode
	}
	if o.Model != nil {
		x.Model = strings.TrimSpace(*o.Model)
	}
	if o.Effort != nil {
		x.Effort = *o.Effort
	}
	if err := sessionOverrides(*x).Validate(); err != nil {
		return err
	}
	if x.PermissionMode != before && prefs.Dangerous(x.PermissionMode) && !o.ConfirmDangerous {
		return errConfirmDangerous(l)
	}
	return nil
}

func sessionOverrides(x store.Session) prefs.Preferences {
	return prefs.Preferences{Provider: x.Provider, PermissionMode: x.PermissionMode, Model: x.Model, Effort: x.Effort}
}

// effectivePreferences resolves session override, then cc-remote preferences.
// Empty fields leave the choice to Claude's own configuration.
func (s *Server) effectivePreferences(x store.Session) prefs.Preferences {
	p, _, err := prefs.LoadProvider(s.cfg.DataDir, x.Provider)
	if err != nil {
		log.Printf("preferences: %v", err)
	}
	return p.Over(sessionOverrides(x))
}

func (s *Server) patchSession(w http.ResponseWriter, r *http.Request) {
	var req overrideRequest
	if !decode(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	s.configMu.Lock()
	defer s.configMu.Unlock()
	x, err := s.store.Get(id)
	if err != nil {
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	if err = req.apply(s.lang(r), &x); err != nil {
		fail(w, 400, i18n.Text(s.lang(r), err))
		return
	}
	if err = s.store.SetOverrides(id, x.PermissionMode, x.Model, x.Effort); err != nil {
		fail(w, 500, s.lang(r).T("Could not save session settings", "无法保存会话设置"))
		return
	}
	s.mu.Lock()
	s.decorate(&x)
	s.mu.Unlock()
	respond(w, 200, x)
}

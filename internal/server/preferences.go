package server

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"cc-remote/internal/prefs"
	"cc-remote/internal/store"
)

type preferencesBody struct {
	prefs.Preferences
	Revision         string `json:"revision"`
	ConfirmDangerous bool   `json:"confirmDangerous,omitempty"`
}

const channelOverrideMessage = "共享终端会话由运行中的终端进程持有，网页无法实时切换模型、推理强度或权限模式；请在终端执行 /model、/effort，或按 Shift+Tab 切换权限模式"

// Called under s.configMu.
func (s *Server) savePreferences(next prefs.Preferences, revision string, confirm bool) (preferencesBody, error) {
	old, _, err := prefs.Load(s.cfg.DataDir)
	if err != nil {
		return preferencesBody{}, err
	}
	if next.PermissionMode != old.PermissionMode && prefs.Dangerous(next.PermissionMode) && !confirm {
		return preferencesBody{}, errConfirmDangerous
	}
	saved, rev, err := prefs.Save(s.cfg.DataDir, next, revision)
	return preferencesBody{Preferences: saved, Revision: rev}, err
}

func (s *Server) preferences(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.configMu.Lock()
		p, rev, err := prefs.Load(s.cfg.DataDir)
		s.configMu.Unlock()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		respond(w, 200, preferencesBody{Preferences: p, Revision: rev})
		return
	}
	var req preferencesBody
	if !decode(w, r, &req) {
		return
	}
	s.configMu.Lock()
	result, err := s.savePreferences(req.Preferences, req.Revision, req.ConfirmDangerous)
	s.configMu.Unlock()
	if err != nil {
		status := 400
		if errors.Is(err, prefs.ErrConflict) {
			status = 409
		}
		fail(w, status, err.Error())
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
func (o overrideRequest) apply(x *store.Session) error {
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
		return errConfirmDangerous
	}
	return nil
}

func sessionOverrides(x store.Session) prefs.Preferences {
	return prefs.Preferences{PermissionMode: x.PermissionMode, Model: x.Model, Effort: x.Effort}
}

// effectivePreferences resolves session override, then cc-remote preferences.
// Empty fields leave the choice to Claude's own configuration.
func (s *Server) effectivePreferences(x store.Session) prefs.Preferences {
	p, _, err := prefs.Load(s.cfg.DataDir)
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
		fail(w, 404, "会话不存在")
		return
	}
	if x.Mode == "channel" {
		fail(w, 409, channelOverrideMessage)
		return
	}
	if err = req.apply(&x); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if err = s.store.SetOverrides(id, x.PermissionMode, x.Model, x.Effort); err != nil {
		fail(w, 500, "无法保存会话设置")
		return
	}
	s.mu.Lock()
	s.decorateChannel(&x)
	s.mu.Unlock()
	respond(w, 200, x)
}

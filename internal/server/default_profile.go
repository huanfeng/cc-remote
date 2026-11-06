package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The default supplier of new sessions, per app, chosen by the user. It is one
// host-wide setting: the web's new-session form and `ccr` both resolve an
// unspecified supplier through it. Unset, the CC-Switch current provider is the
// default (and without one, the native configuration).
const defaultProfilesFile = "default-profiles.json"

var defaultProfilesMu sync.Mutex

func loadDefaultProfiles(dataDir string) (map[string]string, error) {
	out := map[string]string{}
	if dataDir == "" {
		return out, nil
	}
	b, err := os.ReadFile(filepath.Join(dataDir, defaultProfilesFile))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(b, &out); err != nil {
		return map[string]string{}, err
	}
	return out, nil
}

func writeDefaultProfiles(dataDir string, v map[string]string) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dataDir, defaultProfilesFile+".*") // 0600
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dataDir, defaultProfilesFile))
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// defaultProfileAPI is PUT /api/profiles/default {app, profileId}. profileId is
// "" (follow CC-Switch), "@native" or a supplier of that app.
func (s *Server) defaultProfileAPI(w http.ResponseWriter, r *http.Request) {
	l := s.lang(r)
	var req struct {
		App       string `json:"app"`
		ProfileID string `json:"profileId"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.App != "claude" && req.App != "codex" || s.cfg.DataDir == "" {
		fail(w, 400, l.T("Unsupported provider", "不支持的提供方"))
		return
	}
	id := req.ProfileID
	if id != "" && id != "@native" {
		f, err := s.profileCatalog()
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		id = ""
		for _, p := range f.Profiles {
			if (p.ID == req.ProfileID || strings.TrimPrefix(p.ID, "ccswitch:") == req.ProfileID) && p.App == req.App {
				id = p.ID
			}
		}
		if id == "" {
			fail(w, 404, l.T("Supplier configuration not found", "未找到该供应商配置"))
			return
		}
	}
	defaultProfilesMu.Lock()
	defer defaultProfilesMu.Unlock()
	current, err := loadDefaultProfiles(s.cfg.DataDir)
	if err != nil {
		current = map[string]string{} // unreadable: replaced by this choice
	}
	if id == "" {
		delete(current, req.App)
	} else {
		current[req.App] = id
	}
	if err = writeDefaultProfiles(s.cfg.DataDir, current); err != nil {
		fail(w, 500, l.T("Could not save the default supplier", "无法保存默认供应商"))
		return
	}
	respond(w, 200, map[string]any{"configuredDefaults": current})
}

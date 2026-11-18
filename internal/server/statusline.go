package server

import (
	"bytes"
	"cc-remote/internal/jsonedit"
	"cc-remote/internal/statusline"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const statuslineBackupKey = "ccRemoteStatuslineBackup"

type statuslineState struct {
	Installed    bool                `json:"installed"`
	Style        string              `json:"style"`
	Revision     string              `json:"revision"`
	Options      *statusline.Options `json:"options,omitempty"`
	Capabilities map[string]bool     `json:"capabilities"`
	HasExisting  bool                `json:"hasExisting"`
}
type statuslineBackup struct {
	Options  *statusline.Options `json:"options,omitempty"`
	Original json.RawMessage     `json:"original"`
	Managed  json.RawMessage     `json:"managed"`
	Style    string              `json:"style"`
}

func statuslineSnapshot(v map[string]json.RawMessage, b []byte) statuslineState {
	var backup statuslineBackup
	_ = json.Unmarshal(v[statuslineBackupKey], &backup)
	installed := len(backup.Managed) > 0 && jsonEqual(v["statusLine"], backup.Managed)
	x := statuslineState{Installed: installed, Capabilities: map[string]bool{"customize": true, "shortLabels": true}, Revision: settingsRevision(b), HasExisting: len(v["statusLine"]) > 0}
	if installed {
		x.Style = backup.Style
		x.Options = backup.Options
	}
	return x
}
func jsonEqual(a, b []byte) bool {
	var aa, bb any
	if json.Unmarshal(a, &aa) != nil || json.Unmarshal(b, &bb) != nil {
		return false
	}
	x, _ := json.Marshal(aa)
	y, _ := json.Marshal(bb)
	return bytes.Equal(x, y)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (s *Server) statuslineAPI(w http.ResponseWriter, r *http.Request) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	l := s.lang(r)
	v, b, err := s.readSettings(l)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	current := statuslineSnapshot(v, b)
	if r.Method == http.MethodGet {
		respond(w, 200, current)
		return
	}
	var req struct {
		Options  *statusline.Options `json:"options,omitempty"`
		Style    string              `json:"style"`
		Revision string              `json:"revision"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Revision != current.Revision {
		fail(w, 409, l.T("Settings changed; reload first", "配置已修改，请先刷新"))
		return
	}
	var backup statuslineBackup
	if raw, exists := v[statuslineBackupKey]; exists {
		if json.Unmarshal(raw, &backup) != nil || !current.Installed {
			fail(w, 409, l.T("Status line changed outside CC Remote; restore it before retrying", "状态栏已在 CC Remote 外修改，请先恢复后重试"))
			return
		}
	}
	var next []byte
	if r.Method == http.MethodDelete {
		if !current.Installed {
			respond(w, 200, current)
			return
		}
		original := backup.Original
		if bytes.Equal(original, []byte("null")) {
			original = nil
		}
		next, err = jsonedit.Set(b, []string{"statusLine"}, original)
		if err == nil {
			next, err = jsonedit.Set(next, []string{statuslineBackupKey}, nil)
		}
	} else {
		if req.Options == nil {
			req.Options = backup.Options
		}
		if req.Options != nil {
			if e := req.Options.Validate(); e != nil {
				fail(w, 400, e.Error())
				return
			}
		}
		if !statusline.ValidStyle(req.Style) {
			fail(w, 400, "Invalid status line style")
			return
		}
		exe, e := os.Executable()
		if e != nil {
			fail(w, 500, e.Error())
			return
		}
		command := shellQuote(filepath.ToSlash(exe)) + " statusline " + req.Style
		if req.Options != nil {
			command += " " + shellQuote(statusline.Encode(req.Options))
		}
		managed, _ := json.Marshal(map[string]string{"type": "command", "command": command})
		if !current.Installed {
			backup.Original = v["statusLine"]
		}
		backup.Managed = managed
		backup.Style = req.Style
		backup.Options = req.Options
		raw, _ := json.Marshal(backup)
		next, err = jsonedit.Set(b, []string{statuslineBackupKey}, raw)
		if err == nil {
			next, err = jsonedit.Set(next, []string{"statusLine"}, managed)
		}
	}
	if err == nil {
		err = os.MkdirAll(s.cfg.ClaudeConfigDir, 0700)
	}
	if err == nil {
		var f *os.File
		f, err = os.CreateTemp(s.cfg.ClaudeConfigDir, ".statusline-*")
		if err == nil {
			defer os.Remove(f.Name())
			_, err = f.Write(next)
			if err == nil {
				err = f.Sync()
			}
			ce := f.Close()
			if err == nil {
				err = ce
			}
			if err == nil {
				_, latest, e := s.readSettings(l)
				err = e
				if err == nil && settingsRevision(latest) != current.Revision {
					err = errSettingsConflict
				}
			}
			if err == nil {
				err = os.Rename(f.Name(), filepath.Join(s.cfg.ClaudeConfigDir, "settings.json"))
			}
		}
	}
	if err != nil {
		code := 500
		if errors.Is(err, errSettingsConflict) {
			code = 409
		}
		fail(w, code, err.Error())
		return
	}
	v, b, err = s.readSettings(l)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	respond(w, 200, statuslineSnapshot(v, b))
}

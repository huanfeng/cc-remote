package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"cc-remote/internal/i18n"
	"cc-remote/internal/jsonedit"
	"cc-remote/internal/prefs"
)

// Only expose editable preferences. The rest of settings.json may contain secrets.
type claudeSettings struct {
	Model          string `json:"model"`
	Effort         string `json:"effortLevel"`
	PermissionMode string `json:"permissionMode"`
	Revision       string `json:"revision"`
	Path           string `json:"path,omitempty"`
	// Required when newly selecting bypassPermissions, dontAsk or auto.
	ConfirmDangerous bool `json:"confirmDangerous,omitempty"`
}

func settingsRevision(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (s *Server) readSettings(l i18n.Lang) (map[string]json.RawMessage, []byte, error) {
	path := filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		b = nil
		err = nil
	}
	if err != nil {
		return nil, nil, err
	}
	if len(b) > 2<<20 {
		return nil, nil, l.Errorf("Claude's settings file is too large", "Claude settings 文件过大")
	}
	v := map[string]json.RawMessage{}
	if body := jsonedit.StripBOM(b); len(bytes.TrimSpace(body)) != 0 {
		if err = json.Unmarshal(body, &v); err != nil || v == nil {
			return nil, nil, l.Errorf("Claude's settings are not a valid JSON object; fix them on the host first", "Claude settings 不是有效 JSON 对象，请先在宿主修复")
		}
	}
	return v, b, nil
}

func (s *Server) settingsSnapshot(v map[string]json.RawMessage, b []byte) claudeSettings {
	x := claudeSettings{Revision: settingsRevision(b), Path: filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")}
	_ = json.Unmarshal(v["model"], &x.Model)
	_ = json.Unmarshal(v["effortLevel"], &x.Effort)
	var permissions map[string]json.RawMessage
	_ = json.Unmarshal(v["permissions"], &permissions)
	_ = json.Unmarshal(permissions["defaultMode"], &x.PermissionMode)
	return x
}

// validSettings checks the edited values; an unchanged model already in the file is kept as is.
func validSettings(x, old claudeSettings) bool {
	p := prefs.Preferences{PermissionMode: x.PermissionMode, Model: x.Model, Effort: x.Effort}
	if x.Model == old.Model {
		p.Model = ""
	}
	return p.Validate() == nil
}

// Backups are taken once per path per process, before the first write.
var settingsBackups sync.Map

// Called under s.configMu so concurrent browser writes cannot overwrite each other.
// Only model, effortLevel and permissions.defaultMode are edited in place; all
// other bytes, key order, indentation and newline style are kept.
func (s *Server) saveSettings(l i18n.Lang, x claudeSettings) (claudeSettings, error) {
	v, before, err := s.readSettings(l)
	if err != nil {
		return x, err
	}
	if x.Revision != settingsRevision(before) {
		return x, errSettingsConflict
	}
	x.Model = strings.TrimSpace(x.Model)
	old := s.settingsSnapshot(v, before)
	if !validSettings(x, old) {
		return x, l.Errorf("Invalid Claude settings", "无效的 Claude 设置")
	}
	if x.PermissionMode != old.PermissionMode && prefs.Dangerous(x.PermissionMode) && !x.ConfirmDangerous {
		return x, errConfirmDangerous(l)
	}
	b := before
	for _, edit := range []struct {
		path     []string
		old, new string
	}{{[]string{"model"}, old.Model, x.Model}, {[]string{"effortLevel"}, old.Effort, x.Effort}, {[]string{"permissions", "defaultMode"}, old.PermissionMode, x.PermissionMode}} {
		if edit.old == edit.new {
			continue
		}
		var value json.RawMessage
		if edit.new != "" {
			value, _ = json.Marshal(edit.new)
		}
		if b, err = jsonedit.Set(b, edit.path, value); err != nil {
			return x, l.Errorf("Could not update Claude's settings: %v", "无法更新 Claude 设置：%v", err)
		}
	}
	if string(b) == string(before) {
		return old, nil
	}
	path := filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")
	if err = os.MkdirAll(s.cfg.ClaudeConfigDir, 0700); err != nil {
		return x, err
	}
	if _, done := settingsBackups.Load(path); !done && len(before) > 0 {
		if err = os.WriteFile(path+".cc-remote.bak", before, 0600); err != nil {
			return x, l.Errorf("Could not back up Claude's settings: %w", "无法备份 Claude 设置：%w", err)
		}
		settingsBackups.Store(path, true)
	}
	f, err := os.CreateTemp(s.cfg.ClaudeConfigDir, ".cc-remote-settings-*")
	if err != nil {
		return x, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return x, err
	}
	if closeErr != nil {
		return x, closeErr
	}
	// Check again immediately before replacing a file Claude may also be editing.
	_, latest, err := s.readSettings(l)
	if err != nil {
		return x, err
	}
	if settingsRevision(latest) != x.Revision {
		return x, errSettingsConflict
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return x, err
	}
	v, b, err = s.readSettings(l)
	if err != nil {
		return x, err
	}
	return s.settingsSnapshot(v, b), nil
}

func errConfirmDangerous(l i18n.Lang) error {
	return l.Errorf("This permission mode skips or automatically handles approvals; confirm before saving", "该权限模式会跳过或自动处理审批，请确认后再保存")
}

// errSettingsConflict is told to the user in their language by settings.
var errSettingsConflict = errors.New("claude settings changed concurrently")

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	l := s.lang(r)
	if r.Method == http.MethodGet {
		s.configMu.Lock()
		v, b, err := s.readSettings(l)
		s.configMu.Unlock()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		respond(w, 200, s.settingsSnapshot(v, b))
		return
	}
	var x claudeSettings
	if !decode(w, r, &x) {
		return
	}
	s.configMu.Lock()
	result, err := s.saveSettings(l, x)
	s.configMu.Unlock()
	if err != nil {
		status := 400
		message := err.Error()
		if errors.Is(err, errSettingsConflict) {
			status = 409
			message = l.T("Claude's settings were changed elsewhere; reload and save again", "Claude 设置已被其它端修改，请重新加载后保存")
		}
		fail(w, status, message)
		return
	}
	respond(w, 200, result)
}

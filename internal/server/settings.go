package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Only expose editable preferences. The rest of settings.json may contain secrets.
type claudeSettings struct {
	Model          string `json:"model"`
	Effort         string `json:"effortLevel"`
	PermissionMode string `json:"permissionMode"`
	Revision       string `json:"revision"`
	Path           string `json:"path,omitempty"`
}

func settingsRevision(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (s *Server) readSettings() (map[string]json.RawMessage, []byte, error) {
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
		return nil, nil, fmt.Errorf("Claude settings 文件过大")
	}
	v := map[string]json.RawMessage{}
	if len(b) != 0 {
		if err = json.Unmarshal(b, &v); err != nil || v == nil {
			return nil, nil, fmt.Errorf("Claude settings 不是有效 JSON 对象，请先在宿主修复")
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

func validSettings(x claudeSettings) bool {
	if len(x.Model) > 200 || strings.ContainsAny(x.Model, "\n\r\x00") {
		return false
	}
	switch x.Effort {
	case "", "low", "medium", "high", "xhigh", "max":
	default:
		return false
	}
	switch x.PermissionMode {
	case "", "default", "plan", "acceptEdits", "dontAsk", "auto", "bypassPermissions":
	default:
		return false
	}
	return true
}

// Called under s.mu so concurrent browser writes cannot overwrite each other.
func (s *Server) saveSettings(x claudeSettings) (claudeSettings, error) {
	v, before, err := s.readSettings()
	if err != nil {
		return x, err
	}
	if x.Revision != settingsRevision(before) {
		return x, errSettingsConflict
	}
	if !validSettings(x) {
		return x, fmt.Errorf("无效的 Claude 设置")
	}
	old := s.settingsSnapshot(v, before)
	// Preserve an existing advanced permission mode but do not enable one through a typo.
	if x.PermissionMode != old.PermissionMode && x.PermissionMode != "" && x.PermissionMode != "default" && x.PermissionMode != "plan" && x.PermissionMode != "acceptEdits" {
		return x, fmt.Errorf("网页可选择 default、plan 或 acceptEdits；其它模式请在原生 Claude 中配置")
	}
	for k, value := range map[string]string{"model": strings.TrimSpace(x.Model), "effortLevel": x.Effort} {
		if value == "" {
			delete(v, k)
		} else {
			v[k], _ = json.Marshal(value)
		}
	}
	permissions := map[string]json.RawMessage{}
	if raw, ok := v["permissions"]; ok {
		if err = json.Unmarshal(raw, &permissions); err != nil || permissions == nil {
			return x, fmt.Errorf("permissions 配置不是对象")
		}
	}
	if x.PermissionMode == "" {
		delete(permissions, "defaultMode")
	} else {
		permissions["defaultMode"], _ = json.Marshal(x.PermissionMode)
	}
	if len(permissions) == 0 {
		delete(v, "permissions")
	} else {
		v["permissions"], _ = json.Marshal(permissions)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return x, err
	}
	b = append(b, '\n')
	if err = os.MkdirAll(s.cfg.ClaudeConfigDir, 0700); err != nil {
		return x, err
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
	_, latest, err := s.readSettings()
	if err != nil {
		return x, err
	}
	if settingsRevision(latest) != x.Revision {
		return x, errSettingsConflict
	}
	if err = os.Rename(f.Name(), filepath.Join(s.cfg.ClaudeConfigDir, "settings.json")); err != nil {
		return x, err
	}
	return s.settingsSnapshot(v, b), nil
}

var errSettingsConflict = errors.New("Claude 设置已被其它端修改，请重新加载后保存")

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.mu.Lock()
		v, b, err := s.readSettings()
		s.mu.Unlock()
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
	s.mu.Lock()
	result, err := s.saveSettings(x)
	s.mu.Unlock()
	if err != nil {
		status := 400
		if errors.Is(err, errSettingsConflict) {
			status = 409
		}
		fail(w, status, err.Error())
		return
	}
	respond(w, 200, result)
}

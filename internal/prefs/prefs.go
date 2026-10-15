// Package prefs stores cc-remote's own Claude launch preferences.
// They are passed as CLI flags and never written into Claude's configuration.
package prefs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Empty fields mean "follow Claude's own configuration".
type Preferences struct {
	PermissionMode string `json:"permissionMode"`
	Model          string `json:"model"`
	Effort         string `json:"effort"`
}

var ErrConflict = errors.New("CC Remote 默认设置已被其它端修改，请重新加载后保存")

const fileName = "preferences.json"

func Revision(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func ValidPermissionMode(x string) bool {
	switch x {
	case "", "default", "plan", "acceptEdits", "dontAsk", "auto", "bypassPermissions":
		return true
	}
	return false
}

func ValidEffort(x string) bool {
	switch x {
	case "", "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}

// modelPattern accepts model IDs and aliases such as claude-opus-4-1@20250805,
// opus[1m] or provider/model ARNs; a leading '-' could be parsed as a CLI flag.
var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\[\]@-]{0,199}$`)

// ValidModel reports whether x is empty (follow defaults) or a safe model name.
func ValidModel(x string) bool {
	return x == "" || modelPattern.MatchString(x)
}

// Dangerous modes skip or auto-answer approvals and need explicit confirmation.
func Dangerous(mode string) bool {
	return mode == "bypassPermissions" || mode == "dontAsk" || mode == "auto"
}

func (p Preferences) Validate() error {
	if !ValidPermissionMode(p.PermissionMode) {
		return fmt.Errorf("无效的权限模式：%s", p.PermissionMode)
	}
	if !ValidEffort(p.Effort) {
		return fmt.Errorf("无效的推理强度：%s", p.Effort)
	}
	if !ValidModel(p.Model) {
		return fmt.Errorf("模型名称无效或过长")
	}
	return nil
}

// Over returns p with every non-empty field of override applied.
func (p Preferences) Over(override Preferences) Preferences {
	if override.PermissionMode != "" {
		p.PermissionMode = override.PermissionMode
	}
	if override.Model != "" {
		p.Model = override.Model
	}
	if override.Effort != "" {
		p.Effort = override.Effort
	}
	return p
}

// Load returns zero preferences when the file does not exist.
func Load(dataDir string) (Preferences, string, error) {
	var p Preferences
	if dataDir == "" {
		return p, Revision(nil), nil
	}
	b, err := os.ReadFile(filepath.Join(dataDir, fileName))
	if errors.Is(err, os.ErrNotExist) {
		return p, Revision(nil), nil
	}
	if err != nil {
		return p, "", err
	}
	if err = json.Unmarshal(b, &p); err != nil {
		return Preferences{}, "", fmt.Errorf("preferences.json 不是有效 JSON：%w", err)
	}
	p.Model = strings.TrimSpace(p.Model)
	if err = p.Validate(); err != nil {
		return Preferences{}, "", err
	}
	return p, Revision(b), nil
}

// Save replaces the file only when revision still matches. Callers serialize writers.
func Save(dataDir string, p Preferences, revision string) (Preferences, string, error) {
	p.Model = strings.TrimSpace(p.Model)
	if err := p.Validate(); err != nil {
		return p, "", err
	}
	if dataDir == "" {
		return p, "", errors.New("未配置数据目录")
	}
	_, current, err := Load(dataDir)
	if err != nil {
		return p, "", err
	}
	if revision != current {
		return p, "", ErrConflict
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return p, "", err
	}
	b = append(b, '\n')
	if err = os.MkdirAll(dataDir, 0700); err != nil {
		return p, "", err
	}
	f, err := os.CreateTemp(dataDir, ".preferences-*")
	if err != nil {
		return p, "", err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return p, "", err
	}
	if err = os.Chmod(f.Name(), 0600); err != nil {
		return p, "", err
	}
	if err = os.Rename(f.Name(), filepath.Join(dataDir, fileName)); err != nil {
		return p, "", err
	}
	return p, Revision(b), nil
}

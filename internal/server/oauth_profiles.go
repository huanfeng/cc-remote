package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

// An OAuth profile is a Claude Code OAuth token (from `claude setup-token`)
// kept on this host. Unlike a CC-Switch supplier it replaces nothing but the
// credential: models, plugins, hooks and settings stay the host's own.
const (
	oauthKind       = "oauth"
	oauthIDPrefix   = "oauth:"
	oauthFileName   = "oauth-profiles.json"
	oauthTokenStart = "sk-ant-oat"
)

type oauthEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Token     string `json:"token"`
	CreatedAt string `json:"createdAt"`
}

// One process-wide lock: the file is small and edits are rare.
var oauthMu sync.Mutex

// tokenFingerprint is part of the runtime launch key only, never stored.
func tokenFingerprint(token string, launchErr error) string {
	if launchErr != nil {
		return "#unavailable"
	}
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return "#" + hex.EncodeToString(sum[:6])
}

func (e oauthEntry) profile() supplierProfile {
	p := supplierProfile{ID: oauthIDPrefix + e.ID, Name: e.Name, App: "claude"}
	p.Settings.Kind = oauthKind
	p.Settings.Env = map[string]string{claude.OAuthEnvKey: e.Token}
	return p
}

func loadOAuthEntries(dataDir string) ([]oauthEntry, error) {
	oauthMu.Lock()
	defer oauthMu.Unlock()
	return readOAuthEntries(dataDir)
}

func readOAuthEntries(dataDir string) ([]oauthEntry, error) {
	if dataDir == "" {
		return nil, nil
	}
	b, err := os.ReadFile(filepath.Join(dataDir, oauthFileName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []oauthEntry
	if err = json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func findOAuthEntry(dataDir, id string) (oauthEntry, bool, error) {
	entries, err := loadOAuthEntries(dataDir)
	if err != nil {
		return oauthEntry{}, false, err
	}
	for _, e := range entries {
		if e.ID == id {
			return e, true, nil
		}
	}
	return oauthEntry{}, false, nil
}

func writeOAuthEntries(dataDir string, entries []oauthEntry) error {
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dataDir, oauthFileName+".*") // 0600
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dataDir, oauthFileName))
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

func validOAuthName(name string) bool {
	if name == "" || len([]rune(name)) > 60 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validOAuthToken(token string) bool {
	return strings.HasPrefix(token, oauthTokenStart) && len(token) <= 1024 && !strings.ContainsFunc(token, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

type oauthRequest struct {
	Name  *string `json:"name"`
	Token *string `json:"token"`
}

// listOAuthAPI serves GET /api/profiles/oauth: names only, independent of the
// CC-Switch catalog so the settings panel still works when that database does not.
func (s *Server) listOAuthAPI(w http.ResponseWriter, r *http.Request) {
	entries, err := loadOAuthEntries(s.cfg.DataDir)
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not read the OAuth token store", "无法读取 OAuth 令牌配置"))
		return
	}
	items := []map[string]string{}
	for _, e := range entries {
		items = append(items, map[string]string{"id": oauthIDPrefix + e.ID, "name": e.Name, "app": "claude", "kind": oauthKind})
	}
	respond(w, 200, map[string]any{"profiles": items})
}

// oauthAPI serves POST /api/profiles/oauth and PATCH|DELETE /api/profiles/oauth/{id}.
// Replies never include the token.
func (s *Server) oauthAPI(w http.ResponseWriter, r *http.Request) {
	l := s.lang(r)
	id := r.PathValue("id")
	var req oauthRequest
	if r.Method != http.MethodDelete && !decode(w, r, &req) {
		return
	}
	oauthMu.Lock()
	defer oauthMu.Unlock()
	if s.cfg.DataDir == "" {
		fail(w, 500, l.T("No data directory configured", "未配置数据目录"))
		return
	}
	entries, err := readOAuthEntries(s.cfg.DataDir)
	if err != nil {
		fail(w, 500, l.T("Could not read the OAuth token store", "无法读取 OAuth 令牌配置"))
		return
	}
	at := -1
	for i, e := range entries {
		if e.ID == id {
			at = i
		}
	}
	if r.Method != http.MethodPost && at < 0 {
		fail(w, 404, l.T("OAuth token configuration not found", "未找到该 OAuth 令牌配置"))
		return
	}
	var saved oauthEntry
	switch r.Method {
	case http.MethodDelete:
		entries = append(entries[:at], entries[at+1:]...)
		s.accountUsage.forget(oauthIDPrefix + id)
		// Removing a credential stops collection; account history is retained.
	case http.MethodPost, http.MethodPatch:
		if r.Method == http.MethodPost {
			if req.Name == nil || req.Token == nil {
				fail(w, 400, l.T("Name and token are required", "需要名称和令牌"))
				return
			}
			saved = oauthEntry{ID: store.ID()[:8], CreatedAt: time.Now().UTC().Format(time.RFC3339)}
			for slices.ContainsFunc(entries, func(e oauthEntry) bool { return e.ID == saved.ID }) {
				saved.ID = store.ID()[:8]
			}
		} else {
			saved = entries[at]
		}
		if req.Name != nil {
			saved.Name = strings.TrimSpace(*req.Name)
		}
		if req.Token != nil {
			saved.Token = strings.TrimSpace(*req.Token)
		}
		if !validOAuthName(saved.Name) {
			fail(w, 400, l.T("Name must be 1-60 characters without control characters", "名称需为 1-60 个字符，且不含控制字符"))
			return
		}
		if !validOAuthToken(saved.Token) {
			fail(w, 400, l.T("Token must be an OAuth token from `claude setup-token` (sk-ant-oat…)", "令牌应是 `claude setup-token` 生成的 OAuth 令牌（sk-ant-oat…）"))
			return
		}
		for i, e := range entries {
			if i != at && strings.EqualFold(e.Name, saved.Name) {
				fail(w, 409, l.T("An OAuth configuration with this name already exists", "已有同名的 OAuth 令牌配置"))
				return
			}
		}
		if at < 0 {
			entries = append(entries, saved)
		} else {
			entries[at] = saved
		}
	}
	if err = writeOAuthEntries(s.cfg.DataDir, entries); err != nil {
		fail(w, 500, l.T("Could not save the OAuth token store", "无法保存 OAuth 令牌配置"))
		return
	}
	if r.Method == http.MethodDelete {
		respond(w, 200, map[string]string{"id": oauthIDPrefix + id})
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	respond(w, status, map[string]string{"id": oauthIDPrefix + saved.ID, "name": saved.Name, "app": "claude", "kind": oauthKind})
}

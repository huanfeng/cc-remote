package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"cc-remote/internal/codex"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

const (
	codexAccountIDPrefix = "codex-account:"
	codexAccountsFile    = "codex-accounts.json"
	codexLoginTimeout    = 15 * time.Minute
)

type codexAccount struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Disabled       bool   `json:"disabled,omitempty"`
	SignOutPending bool   `json:"signOutPending,omitempty"`
}

type codexAccountRegistry struct {
	mu       sync.Mutex
	attempts map[string]*codexAccountLogin
	closing  bool
}

type codexAccountLogin struct {
	account   codexAccount
	cancel    context.CancelFunc
	done      chan struct{}
	createdAt time.Time
	status    string
	err       string
}

func (s *Server) codexAccountHome(id string) string {
	uuid, ok := strings.CutPrefix(id, codexAccountIDPrefix)
	if !ok || !channelUUID.MatchString(uuid) || uuid != strings.ToLower(uuid) || s.cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(s.cfg.DataDir, "codex-accounts", uuid)
}

func (s *Server) loadCodexAccounts() ([]codexAccount, error) {
	if s.cfg.DataDir == "" {
		return []codexAccount{}, nil
	}
	b, err := os.ReadFile(filepath.Join(s.cfg.DataDir, codexAccountsFile))
	if errors.Is(err, os.ErrNotExist) {
		return []codexAccount{}, nil
	}
	if err != nil {
		return nil, err
	}
	accounts := []codexAccount{}
	if err := json.Unmarshal(b, &accounts); err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(accounts))
	for _, account := range accounts {
		if s.codexAccountHome(account.ID) == "" || !validOAuthName(account.Name) || seen[account.ID] ||
			(account.SignOutPending && !account.Disabled) {
			return nil, errors.New("invalid Codex account metadata")
		}
		seen[account.ID] = true
	}
	return accounts, nil
}

// Registry writers hold codexAccounts.mu. The atomic replacement also permits
// catalog and history readers while an account is being renamed or disabled.
func (s *Server) saveCodexAccounts(accounts []codexAccount) error {
	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.cfg.DataDir, "codex-accounts.*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.cfg.DataDir, codexAccountsFile))
}

func (s *Server) listCodexAccountsAPI(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.loadCodexAccounts()
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not read Codex accounts", "无法读取 Codex 账号"))
		return
	}
	accounts = slices.DeleteFunc(accounts, func(account codexAccount) bool {
		return account.Disabled && !account.SignOutPending
	})
	respond(w, 200, map[string]any{"profiles": accounts})
}

func (s *Server) codexAccountLoginAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodPost {
		s.beginCodexAccountLogin(w, r)
		return
	}
	l := s.lang(r)
	a := &s.codexAccounts
	a.mu.Lock()
	defer a.mu.Unlock()
	attempt := a.attempts[r.PathValue("id")]
	if attempt == nil {
		fail(w, 404, l.T("Codex login attempt not found", "未找到 Codex 登录请求"))
		return
	}
	if r.Method == http.MethodDelete {
		if attempt.status == "complete" {
			fail(w, 409, l.T("This Codex account is already connected", "此 Codex 账号已连接"))
			return
		}
		attempt.cancel()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	respond(w, 200, struct {
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}{attempt.status, attempt.err})
}

func (s *Server) beginCodexAccountLogin(w http.ResponseWriter, r *http.Request) {
	l := s.lang(r)
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validOAuthName(name) {
		fail(w, 400, l.T("Name must be 1-60 characters without control characters", "名称需为 1-60 个字符，且不含控制字符"))
		return
	}
	a := &s.codexAccounts
	a.mu.Lock()
	if a.closing {
		a.mu.Unlock()
		fail(w, 503, l.T("The host is shutting down", "宿主正在关闭"))
		return
	}
	if a.attempts == nil {
		a.attempts = make(map[string]*codexAccountLogin)
	}
	now := time.Now()
	pending := 0
	for id, attempt := range a.attempts {
		if attempt.status == "pending" {
			pending++
		} else if now.Sub(attempt.createdAt) > codexLoginTimeout {
			delete(a.attempts, id)
		}
	}
	if pending >= 4 {
		a.mu.Unlock()
		fail(w, 429, l.T("Finish a pending Codex login first", "请先完成或取消一个 Codex 登录请求"))
		return
	}
	accounts, err := s.loadCodexAccounts()
	if err != nil || s.cfg.DataDir == "" {
		a.mu.Unlock()
		fail(w, 500, l.T("Could not read Codex accounts", "无法读取 Codex 账号"))
		return
	}
	if codexAccountNameInUse(a, accounts, name, "") {
		a.mu.Unlock()
		fail(w, 409, l.T("A Codex account with this name already exists", "已有同名的 Codex 账号"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), codexLoginTimeout)
	attemptID := store.ID()
	account := codexAccount{ID: codexAccountIDPrefix + store.ID(), Name: name}
	attempt := &codexAccountLogin{account: account, cancel: cancel, done: make(chan struct{}), createdAt: now, status: "pending"}
	a.attempts[attemptID] = attempt
	a.mu.Unlock()

	home := s.codexAccountHome(account.ID)
	message := l.T("Could not start Codex device login. Check the Codex CLI version and allow device-code login in ChatGPT security settings.", "无法启动 Codex 设备登录。请检查 Codex CLI 版本，并在 ChatGPT 安全设置中允许设备验证码登录。")
	if err := os.MkdirAll(filepath.Dir(home), 0700); err == nil {
		err = os.Mkdir(home, 0700)
		if err == nil {
			var login *codex.AccountLogin
			login, err = codex.BeginAccountLogin(ctx, s.cfg.CodexExecutable, home)
			if err == nil {
				go s.finishCodexAccountLogin(ctx, attempt, login, l)
				respond(w, 201, map[string]string{"id": attemptID, "verificationUrl": login.VerificationURL, "userCode": login.UserCode})
				return
			}
			message += s.discardUnregisteredCodexAccount(account, l)
		}
	}
	cancel()
	a.mu.Lock()
	delete(a.attempts, attemptID)
	close(attempt.done)
	a.mu.Unlock()
	fail(w, 502, message)
}

func (s *Server) finishCodexAccountLogin(ctx context.Context, attempt *codexAccountLogin, login *codex.AccountLogin, l i18n.Lang) {
	defer close(attempt.done)
	defer attempt.cancel()
	loginErr := login.Wait(ctx)
	a := &s.codexAccounts
	var failure string
	a.mu.Lock()
	switch {
	case ctx.Err() != nil:
		failure = l.T("Codex login was canceled or expired", "Codex 登录已取消或超时")
	case loginErr != nil:
		failure = l.T("Codex did not complete device login", "Codex 未完成设备登录")
	default:
		accounts, err := s.loadCodexAccounts()
		if err == nil {
			err = s.saveCodexAccounts(append(accounts, attempt.account))
		}
		if err != nil {
			failure = l.T("Could not save the Codex account", "无法保存 Codex 账号")
		}
	}
	if failure != "" {
		a.mu.Unlock()
		login.Discard()
		failure += s.discardUnregisteredCodexAccount(attempt.account, l)
		a.mu.Lock()
		attempt.status, attempt.err = "failed", failure
		a.mu.Unlock()
		return
	}
	attempt.status = "complete"
	a.mu.Unlock()
	login.Close()
}

// A newly allocated home is forced to use file credentials. Once its login
// process has exited, remove any unregistered credentials left by a failed logout.
func (s *Server) discardUnregisteredCodexAccount(account codexAccount, l i18n.Lang) string {
	err := os.Remove(filepath.Join(s.codexAccountHome(account.ID), "auth.json"))
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return ""
	}
	a := &s.codexAccounts
	a.mu.Lock()
	defer a.mu.Unlock()
	accounts, err := s.loadCodexAccounts()
	if err == nil && !slices.ContainsFunc(accounts, func(existing codexAccount) bool { return existing.ID == account.ID }) {
		account.Disabled, account.SignOutPending = true, true
		if s.saveCodexAccounts(append(accounts, account)) == nil {
			return l.T(" Retry sign-out in supplier settings to clean up this login.", " 请在供应商设置中重试退出，清理本次登录。")
		}
	}
	return l.T(" Check the host's Codex account directory for leftover credentials.", " 请检查宿主的 Codex 账号目录是否留有凭据。")
}

func codexAccountNameInUse(a *codexAccountRegistry, accounts []codexAccount, name, exceptID string) bool {
	for _, account := range accounts {
		if !account.Disabled && account.ID != exceptID && strings.EqualFold(account.Name, name) {
			return true
		}
	}
	for _, attempt := range a.attempts {
		if attempt.status == "pending" && attempt.account.ID != exceptID && strings.EqualFold(attempt.account.Name, name) {
			return true
		}
	}
	return false
}

func (s *Server) closeCodexAccountLogins(ctx context.Context) error {
	a := &s.codexAccounts
	a.mu.Lock()
	a.closing = true
	wait := make([]<-chan struct{}, 0, len(a.attempts))
	for _, attempt := range a.attempts {
		attempt.cancel()
		wait = append(wait, attempt.done)
	}
	a.mu.Unlock()
	for _, done := range wait {
		select {
		case <-done:
		case <-ctx.Done():
			return fmt.Errorf("waiting for Codex logins: %w", ctx.Err())
		}
	}
	return nil
}

func (s *Server) codexAccountAPI(w http.ResponseWriter, r *http.Request) {
	l := s.lang(r)
	id := r.PathValue("id")
	if s.codexAccountHome(id) == "" {
		fail(w, 404, l.T("Codex account not found", "未找到 Codex 账号"))
		return
	}
	if r.Method == http.MethodDelete {
		s.removeCodexAccount(w, r, id)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validOAuthName(name) {
		fail(w, 400, l.T("Name must be 1-60 characters without control characters", "名称需为 1-60 个字符，且不含控制字符"))
		return
	}
	a := &s.codexAccounts
	a.mu.Lock()
	defer a.mu.Unlock()
	accounts, err := s.loadCodexAccounts()
	if err != nil {
		fail(w, 500, l.T("Could not read Codex accounts", "无法读取 Codex 账号"))
		return
	}
	index := slices.IndexFunc(accounts, func(account codexAccount) bool { return account.ID == id && !account.Disabled })
	if index < 0 {
		fail(w, 404, l.T("Codex account not found", "未找到 Codex 账号"))
		return
	}
	if codexAccountNameInUse(a, accounts, name, id) {
		fail(w, 409, l.T("A Codex account with this name already exists", "已有同名的 Codex 账号"))
		return
	}
	accounts[index].Name = name
	if err := s.saveCodexAccounts(accounts); err != nil {
		fail(w, 500, l.T("Could not save the Codex account", "无法保存 Codex 账号"))
		return
	}
	respond(w, 200, accounts[index])
}

func (s *Server) removeCodexAccount(w http.ResponseWriter, r *http.Request, id string) {
	l := s.lang(r)
	status, err := s.disableCodexAccount(id, l)
	if err != nil {
		fail(w, status, err.Error())
		return
	}
	if status != http.StatusNoContent {
		if err := codex.SignOutAccount(r.Context(), s.cfg.CodexExecutable, s.codexAccountHome(id)); err != nil {
			fail(w, 502, l.T("The Codex account is disabled. Retry to complete Codex sign-out.", "Codex 账号已停用。请重试以完成退出登录。"))
			return
		}
		if err := s.finishCodexAccountSignOut(id); err != nil {
			fail(w, 500, l.T("Could not save the Codex account status. Retry to complete sign-out.", "无法保存 Codex 账号状态。请重试以完成退出登录。"))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) disableCodexAccount(id string, l i18n.Lang) (int, error) {
	// Block launch selection while publishing the disabled record.
	s.mu.Lock()
	defer s.mu.Unlock()
	a := &s.codexAccounts
	a.mu.Lock()
	defer a.mu.Unlock()
	accounts, err := s.loadCodexAccounts()
	if err != nil {
		return 500, errors.New(l.T("Could not read Codex accounts", "无法读取 Codex 账号"))
	}
	index := slices.IndexFunc(accounts, func(account codexAccount) bool { return account.ID == id })
	if index < 0 {
		return 404, errors.New(l.T("Codex account not found", "未找到 Codex 账号"))
	}
	if accounts[index].Disabled {
		if !accounts[index].SignOutPending {
			return http.StatusNoContent, nil
		}
		return 0, nil
	}
	if s.codexAccountUsers[id] != 0 {
		return 409, errors.New(l.T("Close sessions using this Codex account and wait for any requests to finish before signing out", "请先关闭使用此 Codex 账号的会话，并等待相关请求结束，再退出登录"))
	}
	sessions, err := s.store.List()
	if err != nil {
		return 500, errors.New(l.T("Could not read sessions", "无法读取会话"))
	}
	for _, session := range sessions {
		if session.ProfileID == id && (s.terminals[session.ID] != nil || s.procs[session.ID] != nil || s.switching[session.ID]) {
			return 409, errors.New(l.T("Close sessions using this Codex account before signing out", "请先关闭使用此 Codex 账号的会话，再退出登录"))
		}
	}
	accounts[index].Disabled = true
	accounts[index].SignOutPending = true
	if err := s.saveCodexAccounts(accounts); err != nil {
		return 500, errors.New(l.T("Could not disable the Codex account", "无法停用 Codex 账号"))
	}
	return 0, nil
}

func (s *Server) finishCodexAccountSignOut(id string) error {
	a := &s.codexAccounts
	a.mu.Lock()
	defer a.mu.Unlock()
	accounts, err := s.loadCodexAccounts()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(accounts, func(account codexAccount) bool { return account.ID == id && account.SignOutPending })
	if index < 0 {
		return nil // another request already finished sign-out
	}
	accounts[index].SignOutPending = false
	return s.saveCodexAccounts(accounts)
}

// holdCodexAccount prevents sign-out until an account-scoped request has
// finished using its home. The returned release runs outside s.mu.
func (s *Server) holdCodexAccount(id string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeCodexAccountProfile(id); err != nil {
		return nil, err
	}
	return s.holdCodexAccountLocked(id), nil
}

func (s *Server) holdCodexAccountLocked(id string) func() {
	if s.codexAccountUsers == nil {
		s.codexAccountUsers = make(map[string]int)
	}
	s.codexAccountUsers[id]++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.codexAccountUsers[id]--
		if s.codexAccountUsers[id] == 0 {
			delete(s.codexAccountUsers, id)
		}
	}
}

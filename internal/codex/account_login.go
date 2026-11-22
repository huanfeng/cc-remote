package codex

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"cc-remote/internal/claude"
)

// AccountConfig makes Codex own the login and credential refresh for one home.
func AccountConfig() map[string]any {
	return map[string]any{
		"cli_auth_credentials_store": "file",
		"forced_login_method":        "chatgpt",
		"model_provider":             "openai",
		"chatgpt_base_url":           "https://chatgpt.com/backend-api/",
		"openai_base_url":            "https://chatgpt.com/backend-api/codex",
	}
}

// AccountEnvironment excludes host auth and endpoint overrides from an
// isolated ChatGPT account process. The host's default Codex is unaffected.
func AccountEnvironment(home string) []string {
	return slices.DeleteFunc(Environment(home), func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "OPENAI_API_KEY", "CODEX_API_KEY", "OPENAI_BASE_URL", "CODEX_BASE_URL",
			"CHATGPT_BASE_URL", "CODEX_AUTH_TOKEN", "CHATGPT_AUTH_TOKEN", "CODEX_SQLITE_HOME",
			"CODEX_ACCESS_TOKEN", "CODEX_APP_SERVER_LOGIN_ISSUER", "CODEX_APP_SERVER_LOGIN_CLIENT_ID",
			"CODEX_REFRESH_TOKEN_URL_OVERRIDE", "CODEX_REVOKE_TOKEN_URL_OVERRIDE",
			"CODEX_INTERNAL_ORIGINATOR_OVERRIDE", "CODEX_INTERNAL_CHATGPT_LOGIN_BASE_URL":
			return true
		}
		return false
	})
}

type loginNotification struct {
	ID      string `json:"loginId"`
	Success bool   `json:"success"`
}

// AccountLogin owns one temporary app-server and its device-code attempt.
type AccountLogin struct {
	VerificationURL string
	UserCode        string

	id        string
	rpc       *rpcClient
	completed <-chan loginNotification
}

// BeginAccountLogin delegates the protocol and credential storage to Codex.
func BeginAccountLogin(ctx context.Context, executable, home string) (*AccountLogin, error) {
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	completed := make(chan loginNotification, 1)
	handler := func(m rpcMessage) error {
		if m.Method == "account/login/completed" && len(m.ID) == 0 {
			var result loginNotification
			if json.Unmarshal(m.Params, &result) == nil && result.ID != "" {
				select {
				case completed <- result:
				default:
				}
			}
		}
		return nil
	}
	options := claude.Options{CodexAccount: true, ConfigOverrides: AccountConfig()}
	r, err := startRPC(executable, home, home, handler, nil, options)
	if err != nil {
		return nil, errors.New("could not start Codex for account login")
	}
	if err := r.initialize(ctx); err != nil {
		r.close()
		return nil, errors.New("could not initialize Codex account login")
	}
	var reply struct {
		Type            string `json:"type"`
		ID              string `json:"loginId"`
		VerificationURL string `json:"verificationUrl"`
		UserCode        string `json:"userCode"`
	}
	if err := r.call(ctx, "account/login/start", map[string]string{"type": "chatgptDeviceCode"}, &reply); err != nil {
		r.close()
		return nil, errors.New("Codex did not start device login")
	}
	u, err := url.Parse(reply.VerificationURL)
	if err != nil || !validDeviceVerification(u) || reply.Type != "chatgptDeviceCode" || reply.ID == "" ||
		reply.UserCode == "" || len(reply.UserCode) > 64 || strings.ContainsFunc(reply.UserCode, unicode.IsControl) {
		r.close()
		return nil, errors.New("Codex returned invalid device login instructions")
	}
	return &AccountLogin{VerificationURL: u.String(), UserCode: reply.UserCode, id: reply.ID, rpc: r, completed: completed}, nil
}

func validDeviceVerification(u *url.URL) bool {
	return u.Scheme == "https" && u.Host == "auth.openai.com" && u.User == nil &&
		u.Path == "/codex/device" && u.RawQuery == "" && u.Fragment == ""
}

func (l *AccountLogin) Wait(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.rpc.done:
			return errors.New("Codex exited during account login")
		case result := <-l.completed:
			if result.ID != l.id {
				continue
			}
			if !result.Success {
				return errors.New("Codex could not complete device login")
			}
			return nil
		}
	}
}

func (l *AccountLogin) Close() { l.rpc.close() }

// Discard cancels an uncommitted attempt. Logging out also handles the case
// where Codex stored credentials just before the application canceled.
func (l *AccountLogin) Discard() {
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = l.rpc.call(ctx, "account/login/cancel", map[string]string{"loginId": l.id}, nil)
	_ = l.rpc.call(ctx, "account/logout", nil, nil)
}

func SignOutAccount(ctx context.Context, executable, home string) error {
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	options := claude.Options{CodexAccount: true, ConfigOverrides: AccountConfig()}
	r, err := startRPC(executable, home, home, nil, nil, options)
	if err != nil {
		return errors.New("could not start Codex for account logout")
	}
	defer r.close()
	if err = r.initialize(ctx); err == nil {
		err = r.call(ctx, "account/logout", nil, nil)
	}
	if err != nil {
		return errors.New("Codex did not complete account logout")
	}
	return nil
}

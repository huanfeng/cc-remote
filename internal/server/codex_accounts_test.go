package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const codexAccountURL = "/api/profiles/codex-accounts"

func fakeCodexAccount() {
	mode := os.Getenv("FAKE_CODEX_ACCOUNT")
	home := os.Getenv("CODEX_HOME")
	fakeFail := func() { os.Exit(4) }
	if home == "" || os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("CODEX_API_KEY") != "" ||
		os.Getenv("OPENAI_BASE_URL") != "" || os.Getenv("CODEX_SQLITE_HOME") != "" ||
		os.Getenv("CODEX_ACCESS_TOKEN") != "" || os.Getenv("CODEX_APP_SERVER_LOGIN_ISSUER") != "" ||
		os.Getenv("CODEX_APP_SERVER_LOGIN_CLIENT_ID") != "" || os.Getenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE") != "" ||
		os.Getenv("CODEX_REVOKE_TOKEN_URL_OVERRIDE") != "" ||
		!slices.Contains(os.Args, `cli_auth_credentials_store="file"`) ||
		!slices.Contains(os.Args, `forced_login_method="chatgpt"`) ||
		!slices.Contains(os.Args, `model_provider="openai"`) ||
		!slices.Contains(os.Args, `chatgpt_base_url="https://chatgpt.com/backend-api/"`) ||
		!slices.Contains(os.Args, `openai_base_url="https://chatgpt.com/backend-api/codex"`) {
		fakeFail()
	}
	out := json.NewEncoder(os.Stdout)
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &msg) != nil {
			fakeFail()
		}
		reply := func(result any) {
			if err := out.Encode(map[string]any{"id": msg.ID, "result": result}); err != nil {
				fakeFail()
			}
		}
		var params map[string]string
		if msg.Method == "account/login/start" || msg.Method == "account/login/cancel" {
			if json.Unmarshal(msg.Params, &params) != nil {
				fakeFail()
			}
		}
		switch msg.Method {
		case "initialize":
			reply(map[string]string{})
		case "initialized":
		case "account/login/start":
			if params["type"] != "chatgptDeviceCode" {
				fakeFail()
			}
			if mode == "pending" || mode == "invalid" {
				if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte("fixture credential"), 0600); err != nil {
					fakeFail()
				}
			}
			verifyURL := "https://auth.openai.com/codex/device"
			if mode == "invalid" {
				verifyURL = "https://not-openai.invalid/codex/device"
			}
			reply(map[string]string{"type": "chatgptDeviceCode", "loginId": "synthetic", "verificationUrl": verifyURL, "userCode": "TEST-CODE"})
			if mode == "complete" {
				if err := os.WriteFile(filepath.Join(home, "synthetic-signed-in"), nil, 0600); err != nil {
					fakeFail()
				}
				_ = out.Encode(map[string]any{"method": "account/login/completed", "params": map[string]any{"loginId": "synthetic", "success": true}})
			}
		case "account/login/cancel":
			if params["loginId"] != "synthetic" {
				fakeFail()
			}
			_ = os.WriteFile(filepath.Join(home, "synthetic-canceled"), nil, 0600)
			reply(map[string]string{"status": "canceled"})
		case "account/logout":
			if _, err := os.Stat(filepath.Join(home, "fail-synthetic-sign-out")); err == nil || mode == "pending" {
				_ = out.Encode(map[string]any{"id": msg.ID, "error": map[string]any{"code": -1, "message": "private RPC detail"}})
				continue
			}
			_ = os.Remove(filepath.Join(home, "synthetic-signed-in"))
			_ = os.WriteFile(filepath.Join(home, "synthetic-signed-out"), nil, 0600)
			reply(struct{}{})
		default:
			fakeFail()
		}
	}
}

func setupCodexAccountTest(t *testing.T, mode string) (*Server, string) {
	t.Helper()
	s, host, _ := setup(t, nil)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.DataDir = t.TempDir()
	s.cfg.CodexExecutable = exe
	t.Setenv("FAKE_CODEX_ACCOUNT", mode)
	t.Setenv("OPENAI_API_KEY", "host-key")
	t.Setenv("CODEX_API_KEY", "host-codex-key")
	t.Setenv("OPENAI_BASE_URL", "https://host-relay.invalid")
	t.Setenv("CODEX_SQLITE_HOME", t.TempDir())
	t.Setenv("CODEX_ACCESS_TOKEN", "host-token")
	t.Setenv("CODEX_APP_SERVER_LOGIN_ISSUER", "https://host-auth.invalid")
	t.Setenv("CODEX_APP_SERVER_LOGIN_CLIENT_ID", "host-app")
	t.Setenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE", "https://host-auth.invalid/refresh")
	t.Setenv("CODEX_REVOKE_TOKEN_URL_OVERRIDE", "https://host-auth.invalid/revoke")
	return s, host.URL
}

func TestCodexAccountDeviceLoginAndLogout(t *testing.T) {
	s, host := setupCodexAccountTest(t, "complete")
	logIn := func(name string) codexAccount {
		t.Helper()
		status, body := call(t, host, http.MethodPost, codexAccountURL+"/login", map[string]string{"name": name})
		var attempt struct {
			ID, VerificationURL, UserCode string
		}
		if status != 201 || json.Unmarshal(body, &attempt) != nil || attempt.ID == "" ||
			attempt.VerificationURL != "https://auth.openai.com/codex/device" || attempt.UserCode != "TEST-CODE" {
			t.Fatalf("invalid device login response: %d %s", status, body)
		}
		var result []byte
		awaitLong(t, func() string { return string(result) }, func() bool {
			status, result = call(t, host, http.MethodGet, codexAccountURL+"/login/"+attempt.ID, nil)
			return status == 200 && string(result) == "{\"status\":\"complete\"}\n"
		})
		accounts, err := s.loadCodexAccounts()
		if err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(accounts, func(a codexAccount) bool { return a.Name == name })
		if index < 0 {
			t.Fatalf("connected account missing from metadata: %+v", accounts)
		}
		return accounts[index]
	}
	work, personal := logIn("Work"), logIn("Personal")
	if work.ID == personal.ID || s.codexAccountHome(work.ID) == s.codexAccountHome(personal.ID) {
		t.Fatal("accounts share a home")
	}
	for _, account := range []codexAccount{work, personal} {
		if _, err := os.Stat(filepath.Join(s.codexAccountHome(account.ID), "synthetic-signed-in")); err != nil {
			t.Fatal(err)
		}
	}
	status, body := call(t, host, http.MethodGet, codexAccountURL, nil)
	if status != 200 || !strings.Contains(string(body), work.ID) || !strings.Contains(string(body), personal.ID) ||
		strings.Contains(string(body), "TEST-CODE") || strings.Contains(string(body), "verificationUrl") {
		t.Fatalf("unsafe or incorrect account list: %d %s", status, body)
	}
	data, err := os.ReadFile(filepath.Join(s.cfg.DataDir, codexAccountsFile))
	if err != nil || strings.Contains(string(data), "TEST-CODE") || strings.Contains(string(data), "host-key") {
		t.Fatalf("unsafe account metadata: %v", err)
	}
	path := codexAccountURL + "/" + url.PathEscape(work.ID)
	status, body = call(t, host, http.MethodPatch, path, map[string]string{"name": "Work 2"})
	if status != 200 || !strings.Contains(string(body), "Work 2") {
		t.Fatalf("rename failed: %d %s", status, body)
	}
	marker := filepath.Join(s.codexAccountHome(work.ID), "fail-synthetic-sign-out")
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	status, body = call(t, host, http.MethodDelete, path, nil)
	if status != 502 || strings.Contains(string(body), "private RPC detail") {
		t.Fatalf("incorrect sign-out failure: %d %s", status, body)
	}
	status, body = call(t, host, http.MethodGet, codexAccountURL, nil)
	if status != 200 || !strings.Contains(string(body), work.ID) || !strings.Contains(string(body), `"signOutPending":true`) {
		t.Fatalf("failed sign-out cannot be retried: %d %s", status, body)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	status, body = call(t, host, http.MethodDelete, path, nil)
	if status != 204 {
		t.Fatalf("logout failed: %d %s", status, body)
	}
	if _, err := os.Stat(filepath.Join(s.codexAccountHome(work.ID), "synthetic-signed-out")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.codexAccountHome(personal.ID), "synthetic-signed-in")); err != nil {
		t.Fatal("sign-out affected other account", err)
	}
	status, body = call(t, host, http.MethodGet, codexAccountURL, nil)
	if status != 200 || strings.Contains(string(body), work.ID) || !strings.Contains(string(body), personal.ID) {
		t.Fatalf("completed sign-out still shown in the account list: %d %s", status, body)
	}
	accounts, err := s.loadCodexAccounts()
	if err != nil || len(accounts) != 2 || accounts[0].ID != work.ID || !accounts[0].Disabled || accounts[0].SignOutPending || accounts[1].Disabled {
		t.Fatalf("sign-out discarded account provenance: %+v %v", accounts, err)
	}
}

func TestCodexAccountCancelDoesNotRegister(t *testing.T) {
	s, host := setupCodexAccountTest(t, "pending")
	status, body := call(t, host, http.MethodPost, codexAccountURL+"/login", map[string]string{"name": "Pending"})
	var response struct{ ID string }
	if status != 201 || json.Unmarshal(body, &response) != nil || response.ID == "" {
		t.Fatalf("could not start login: %d %s", status, body)
	}
	status, body = call(t, host, http.MethodDelete, codexAccountURL+"/login/"+response.ID, nil)
	if status != 204 {
		t.Fatalf("could not cancel login: %d %s", status, body)
	}
	a := &s.codexAccounts
	a.mu.Lock()
	attempt := a.attempts[response.ID]
	a.mu.Unlock()
	home := s.codexAccountHome(attempt.account.ID)
	awaitLong(t, func() string { return fmt.Sprintf("waiting for cancel in %s", home) }, func() bool {
		_, err := os.Stat(filepath.Join(home, "synthetic-canceled"))
		return err == nil
	})
	<-attempt.done
	status, body = call(t, host, http.MethodGet, codexAccountURL+"/login/"+response.ID, nil)
	accounts, err := s.loadCodexAccounts()
	if status != 200 || !strings.Contains(string(body), `"status":"failed"`) || strings.Contains(string(body), "TEST-CODE") ||
		err != nil || len(accounts) != 0 {
		t.Fatalf("canceled login was registered: %d %s %+v %v", status, body, accounts, err)
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("canceled login left credentials after logout RPC failure: %v", err)
	}
}

func TestCodexAccountRejectsUntrustedVerificationURL(t *testing.T) {
	s, host := setupCodexAccountTest(t, "invalid")
	status, body := call(t, host, http.MethodPost, codexAccountURL+"/login", map[string]string{"name": "Invalid"})
	accounts, err := s.loadCodexAccounts()
	if status != 502 || strings.Contains(string(body), "not-openai.invalid") || strings.Contains(string(body), "TEST-CODE") ||
		err != nil || len(accounts) != 0 {
		t.Fatalf("unsafe login start response: %d %s %+v %v", status, body, accounts, err)
	}
	homes, err := os.ReadDir(filepath.Join(s.cfg.DataDir, "codex-accounts"))
	if err != nil || len(homes) != 1 {
		t.Fatalf("could not find attempted login home: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.cfg.DataDir, "codex-accounts", homes[0].Name(), "auth.json")); !os.IsNotExist(err) {
		t.Fatalf("failed login start left credentials: %v", err)
	}
}

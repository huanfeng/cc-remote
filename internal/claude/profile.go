package claude

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// ProfileURL is kept separate from usage: setup-token may lack user:profile.
var ProfileURL = "https://api.anthropic.com/api/oauth/profile"

// FetchAccountIdentity only accepts server-confirmed account and organization
// IDs. Local .claude.json metadata can lag behind a login switch.
func FetchAccountIdentity(ctx context.Context, client *http.Client, token string) (string, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, ProfileURL, nil)
	if err != nil {
		return "", err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("anthropic-beta", "oauth-2025-04-20")
	resp, err := client.Do(r)
	if err != nil {
		return "", errors.New("identity-unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", UsageStatusError{resp.StatusCode}
	}
	var p struct {
		Account struct {
			UUID string `json:"uuid"`
		} `json:"account"`
		Organization struct {
			UUID string `json:"uuid"`
		} `json:"organization"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p) != nil || p.Account.UUID == "" || p.Organization.UUID == "" {
		return "", errors.New("identity-unavailable")
	}
	return p.Account.UUID + "/" + p.Organization.UUID, nil
}

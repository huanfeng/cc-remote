package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// UsageWindow is how much of one subscription limit is used.
type UsageWindow struct {
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resetsAt,omitempty"`
	// Severity is the service's own reading of it: "normal", "warning", ...
	Severity string `json:"severity,omitempty"`
}

// ModelUsage is a weekly limit of one model family (e.g. Opus).
type ModelUsage struct {
	Name string `json:"name"`
	UsageWindow
}

// Usage is the subscription usage Claude Code shows in /usage: the 5-hour
// session window, the week, and weekly limits of single models.
type Usage struct {
	FiveHour *UsageWindow `json:"fiveHour,omitempty"`
	Weekly   *UsageWindow `json:"weekly,omitempty"`
	Models   []ModelUsage `json:"models,omitempty"`
}

// OAuthToken is the Claude subscription login Claude Code keeps.
type OAuthToken struct {
	AccessToken string
	ExpiresAt   time.Time // zero: does not expire (CLAUDE_CODE_OAUTH_TOKEN)
	Plan        string
}

// ErrNoOAuth means Claude Code is not signed in with a subscription (an API
// key has no usage limits to show).
var ErrNoOAuth = errors.New("no Claude subscription login")

// ReadOAuthToken reads the login Claude Code uses, without refreshing it: a
// refresh replaces the refresh token, which would sign out a running Claude
// Code that still holds the old one.
func ReadOAuthToken(configDir string) (OAuthToken, error) {
	if token := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")); token != "" {
		return OAuthToken{AccessToken: token}, nil
	}
	data, err := os.ReadFile(filepath.Join(configDir, ".credentials.json"))
	if err != nil && runtime.GOOS == "darwin" {
		// macOS keeps it in the login keychain instead of the file.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		data, err = exec.CommandContext(ctx, "security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
		cancel()
	}
	if err != nil {
		return OAuthToken{}, ErrNoOAuth
	}
	var file struct {
		OAuth struct {
			AccessToken      string `json:"accessToken"`
			ExpiresAt        int64  `json:"expiresAt"`
			SubscriptionType string `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &file) != nil || file.OAuth.AccessToken == "" {
		return OAuthToken{}, ErrNoOAuth
	}
	t := OAuthToken{AccessToken: file.OAuth.AccessToken, Plan: file.OAuth.SubscriptionType}
	if file.OAuth.ExpiresAt > 0 {
		t.ExpiresAt = time.UnixMilli(file.OAuth.ExpiresAt)
	}
	return t, nil
}

// UsageURL is the endpoint Claude Code's /usage reads (a variable for tests).
var UsageURL = "https://api.anthropic.com/api/oauth/usage"

// UsageStatusError is a refused usage request; 429 asks to come back later.
type UsageStatusError struct{ Status int }

func (e UsageStatusError) Error() string { return fmt.Sprintf("usage request refused (HTTP %d)", e.Status) }

// FetchUsage asks the service for the subscription usage. userAgent names the
// Claude Code version, as Claude Code itself does.
func FetchUsage(ctx context.Context, client *http.Client, token, userAgent string) (Usage, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, UsageURL, nil)
	if err != nil {
		return Usage{}, err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("anthropic-beta", "oauth-2025-04-20")
	if userAgent != "" {
		r.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(r)
	if err != nil {
		return Usage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Usage{}, UsageStatusError{resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Usage{}, err
	}
	return ParseUsage(body)
}

// ParseUsage reads a usage response: the limits list when present (it names
// model limits), else the older fixed fields.
func ParseUsage(body []byte) (Usage, error) {
	type window struct {
		Utilization *float64 `json:"utilization"`
		ResetsAt    string   `json:"resets_at"`
	}
	var v struct {
		FiveHour       *window `json:"five_hour"`
		SevenDay       *window `json:"seven_day"`
		SevenDayOpus   *window `json:"seven_day_opus"`
		SevenDaySonnet *window `json:"seven_day_sonnet"`
		Limits         []struct {
			Kind     string   `json:"kind"`
			Percent  *float64 `json:"percent"`
			Severity string   `json:"severity"`
			ResetsAt string   `json:"resets_at"`
			Scope    *struct {
				Model *struct {
					DisplayName string `json:"display_name"`
				} `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return Usage{}, err
	}
	var u Usage
	seen := map[string]bool{}
	for _, l := range v.Limits {
		if l.Percent == nil || math.IsNaN(*l.Percent) {
			continue
		}
		w := &UsageWindow{Percent: clampPercent(*l.Percent), ResetsAt: l.ResetsAt, Severity: l.Severity}
		switch l.Kind {
		case "session":
			u.FiveHour = w
		case "weekly_all":
			u.Weekly = w
		case "weekly_scoped":
			if l.Scope == nil || l.Scope.Model == nil || strings.TrimSpace(l.Scope.Model.DisplayName) == "" {
				continue
			}
			name := strings.TrimSpace(l.Scope.Model.DisplayName)
			if key := strings.ToLower(name); !seen[key] {
				seen[key] = true
				u.Models = append(u.Models, ModelUsage{Name: name, UsageWindow: *w})
			}
		}
	}
	old := func(w *window) *UsageWindow {
		if w == nil || w.Utilization == nil {
			return nil
		}
		return &UsageWindow{Percent: clampPercent(*w.Utilization), ResetsAt: w.ResetsAt}
	}
	if u.FiveHour == nil {
		u.FiveHour = old(v.FiveHour)
	}
	if u.Weekly == nil {
		u.Weekly = old(v.SevenDay)
	}
	for _, m := range []struct {
		name string
		w    *window
	}{{"Opus", v.SevenDayOpus}, {"Sonnet", v.SevenDaySonnet}} {
		if w := old(m.w); w != nil && !seen[strings.ToLower(m.name)] {
			u.Models = append(u.Models, ModelUsage{Name: m.name, UsageWindow: *w})
		}
	}
	if u.FiveHour == nil && u.Weekly == nil && len(u.Models) == 0 {
		return u, errors.New("the usage response has no limits")
	}
	return u, nil
}

func clampPercent(p float64) float64 { return math.Max(0, math.Min(100, p)) }

package codex

import (
	"context"
	"time"

	"cc-remote/internal/claude"
)

// SubscriptionUsage is account quota, independent of per-thread token usage.
// It contains no credentials, email addresses or account identifiers.
type SubscriptionUsage struct {
	Available bool                 `json:"available"`
	Reason    string               `json:"reason,omitempty"`
	Provider  string               `json:"provider,omitempty"`
	Plan      string               `json:"plan,omitempty"`
	Usage     *SubscriptionWindows `json:"usage,omitempty"`
}
type SubscriptionWindows struct {
	Windows []QuotaWindow `json:"windows"`
}
type QuotaWindow struct {
	Name          string  `json:"name"`
	Percent       float64 `json:"percent"`
	WindowMinutes int64   `json:"windowMinutes,omitempty"`
	ResetsAt      string  `json:"resetsAt,omitempty"`
}
type rateWindow struct {
	UsedPercent        *float64 `json:"usedPercent"`
	WindowDurationMins int64    `json:"windowDurationMins"`
	ResetsAt           int64    `json:"resetsAt"`
}
type rateSnapshot struct {
	LimitID   string      `json:"limitId"`
	LimitName string      `json:"limitName"`
	PlanType  string      `json:"planType"`
	Primary   *rateWindow `json:"primary"`
	Secondary *rateWindow `json:"secondary"`
}

// ReadSubscriptionUsage uses the CLI's own authentication and public app-server
// RPCs. No thread or inference turn is started, and no token files are read.
func ReadSubscriptionUsage(ctx context.Context, o claude.Options) (SubscriptionUsage, error) {
	r, err := startRPC(o.Executable, o.ConfigDir, o.Cwd, nil, nil, o)
	if err != nil {
		return SubscriptionUsage{}, err
	}
	defer r.close()
	if err = r.initialize(ctx); err != nil {
		return SubscriptionUsage{}, err
	}
	var config struct {
		Config struct {
			ModelProvider string `json:"model_provider"`
		} `json:"config"`
	}
	if err = r.call(ctx, "config/read", map[string]any{"includeLayers": false, "cwd": o.Cwd}, &config); err != nil {
		return SubscriptionUsage{}, err
	}
	if provider := config.Config.ModelProvider; provider != "" && provider != "openai" {
		return SubscriptionUsage{Reason: "third-party", Provider: provider}, nil
	}
	var account struct {
		Account *struct {
			Type     string `json:"type"`
			PlanType string `json:"planType"`
		} `json:"account"`
	}
	if err = r.call(ctx, "account/read", map[string]any{"refreshToken": false}, &account); err != nil {
		return SubscriptionUsage{}, err
	}
	if account.Account == nil {
		return SubscriptionUsage{Reason: "no-login"}, nil
	}
	if account.Account.Type != "chatgpt" {
		reason := "third-party"
		if account.Account.Type == "apiKey" {
			reason = "api-key"
		}
		return SubscriptionUsage{Reason: reason}, nil
	}
	var limits struct {
		RateLimits          rateSnapshot            `json:"rateLimits"`
		RateLimitsByLimitID map[string]rateSnapshot `json:"rateLimitsByLimitId"`
	}
	if err = r.call(ctx, "account/rateLimits/read", nil, &limits); err != nil {
		return SubscriptionUsage{}, err
	}
	// Prefer the Codex bucket; the legacy view is the compatibility fallback.
	main := limits.RateLimits
	if bucket, ok := limits.RateLimitsByLimitID["codex"]; ok {
		main = bucket
	}
	result := SubscriptionUsage{Available: true, Plan: account.Account.PlanType, Usage: &SubscriptionWindows{Windows: []QuotaWindow{}}}
	appendWindow := func(name string, w *rateWindow) {
		if w == nil || w.UsedPercent == nil || *w.UsedPercent < 0 || *w.UsedPercent > 100 {
			return
		}
		window := QuotaWindow{Name: name, Percent: *w.UsedPercent, WindowMinutes: w.WindowDurationMins}
		if w.ResetsAt > 0 {
			window.ResetsAt = time.Unix(w.ResetsAt, 0).UTC().Format(time.RFC3339)
		}
		result.Usage.Windows = append(result.Usage.Windows, window)
	}
	appendWindow("primary", main.Primary)
	appendWindow("secondary", main.Secondary)
	return result, nil
}

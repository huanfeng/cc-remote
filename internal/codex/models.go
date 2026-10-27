package codex

import (
	"context"
	"errors"
)

type Model struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Description   string   `json:"description"`
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"defaultEffort,omitempty"`
	IsDefault     bool     `json:"isDefault,omitempty"`
}

// Models reads the native catalog without starting a thread or inference turn.
func Models(ctx context.Context, executable, configDir, cwd string) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	r, err := startRPC(executable, configDir, cwd, nil, nil)
	if err != nil {
		return nil, err
	}
	defer r.close()
	if err = r.initialize(ctx); err != nil {
		return nil, err
	}
	return readModels(ctx, r)
}

func readModels(ctx context.Context, r *rpcClient) ([]Model, error) {
	var models []Model
	cursor := ""
	seen := map[string]bool{}
	for {
		params := map[string]any{"limit": 100, "includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var page struct {
			Data []struct {
				ID               string `json:"id"`
				Model            string `json:"model"`
				DisplayName      string `json:"displayName"`
				Description      string `json:"description"`
				Hidden           bool   `json:"hidden"`
				DefaultEffort    string `json:"defaultReasoningEffort"`
				IsDefault        bool   `json:"isDefault"`
				SupportedEfforts []struct {
					Effort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := r.call(ctx, "model/list", params, &page); err != nil {
			return nil, err
		}
		for _, m := range page.Data {
			if m.Hidden {
				continue
			}
			id := m.Model
			if id == "" {
				id = m.ID
			}
			if id != "" {
				efforts := []string{}
				for _, e := range m.SupportedEfforts {
					if e.Effort != "" {
						efforts = append(efforts, e.Effort)
					}
				}
				models = append(models, Model{ID: id, Label: m.DisplayName, Description: m.Description, Efforts: efforts, DefaultEffort: m.DefaultEffort, IsDefault: m.IsDefault})
			}
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			return models, nil
		}
		cursor = *page.NextCursor
		if seen[cursor] {
			return nil, errors.New("Codex repeated model pagination cursor")
		}
		seen[cursor] = true
	}
}

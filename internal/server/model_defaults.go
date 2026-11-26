package server

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"
	"cc-remote/internal/prefs"
	"cc-remote/internal/store"
)

// Preview the launch without the session overrides. Preferences and supplier
// mappings are read separately from the cached model catalog.
func (s *Server) previewModelDefaults(l i18n.Lang, x store.Session, list *modelList) {
	x.Model, x.PermissionMode = "", ""
	p := s.effectivePreferences(x)
	o := claude.Options{Model: p.Model, PermissionMode: p.PermissionMode}
	if len(x.ClaudeArgs) > 0 || s.profileOptions(x, &o) != nil {
		list.DefaultModel, list.DefaultPermission = "", ""
		return
	}
	if x.Provider == "codex" {
		if o.Model != "" {
			list.DefaultModel = validPreviewModel(o.Model)
		}
		if o.PermissionMode != "" {
			list.DefaultPermission = permissionDefault("codex", o.PermissionMode)
		}
		return
	}

	managed, layers, warnings := s.modelSettingsLayers(l, x.Cwd)
	if len(warnings) > 0 {
		list.DefaultModel = validPreviewModel(o.Model)
		list.DefaultPermission = permissionDefault("claude", o.PermissionMode)
		return
	}
	// Highest priority first. Empty supplier overlay entries clear model
	// mappings inherited from the host.
	slices.Reverse(layers)
	settings := append([]modelSettings{managed}, layers...)
	env := func(key string) string {
		if v := managed.envString(key); v != "" {
			return v
		}
		if v, ok := o.ProfileEnv[key]; ok {
			return v
		}
		if _, ok := o.ProfileEnv["ANTHROPIC_BASE_URL"]; !ok {
			if v := os.Getenv(key); v != "" {
				return v
			}
		}
		for _, layer := range layers {
			if v := layer.envString(key); v != "" {
				return v
			}
		}
		return ""
	}
	mappings := map[string]string{}
	for _, family := range []string{"opus", "sonnet", "haiku", "fable"} {
		if id := validPreviewModel(env("ANTHROPIC_DEFAULT_" + strings.ToUpper(family) + "_MODEL")); id != "" {
			mappings[family] = id
		}
	}
	nativeDefault := validPreviewModel(env("ANTHROPIC_DEFAULT_MODEL"))
	if nativeDefault == "haiku" || nativeDefault == "opusplan" || managed.EnforceAvailableModels {
		nativeDefault = ""
	}
	for _, layer := range settings {
		if layer.AvailableModels != nil && !modelAllowed(nativeDefault, *layer.AvailableModels) {
			nativeDefault = ""
			break
		}
	}
	if nativeDefault != "" || mappings["opus"] != "" {
		var major, minor, patch int
		n, _ := fmt.Sscanf(s.claudeVersion().Version, "%d.%d.%d", &major, &minor, &patch)
		supports := func(minimum int) bool {
			return n == 3 && (major > 2 || major == 2 && (minor > 1 || minor == 1 && patch >= minimum))
		}
		if supports(321) && mappings["opus"] != "" {
			mappings["best"] = mappings["opus"]
		}
		if nativeDefault != "" && supports(236) {
			target := nativeDefault
			if mapped := mappings[target]; mapped != "" {
				target = mapped
			}
			mappings["default"] = target
		}
	}
	list.AliasModels = mappings

	model := o.Model
	if model == "" {
		model = env("ANTHROPIC_MODEL")
	}
	// A resumed conversation can restore its native model ahead of a settings
	// default. The current live model does not establish what inheritance means.
	if model == "" && !x.Resume {
		for _, layer := range settings {
			if layer.Model != "" {
				model = layer.Model
				break
			}
		}
	}
	list.DefaultModel = validPreviewModel(model)
	if strings.EqualFold(strings.TrimSpace(model), "default") || model == "" && !x.Resume {
		list.DefaultModel = mappings["default"]
	}
	mode := o.PermissionMode
	if mode == "" && !x.Resume {
		for i, layer := range settings {
			candidate := permissionDefault("claude", layer.Permissions.DefaultMode)
			if i > 0 && i < len(settings)-1 && (candidate == "auto" || candidate == "bypassPermissions") {
				break
			}
			if candidate != "" {
				mode = candidate
				break
			}
		}
		if mode == "" && x.Mode != "terminal" {
			mode = "default" // non-interactive Claude Code starts in Manual mode
		}
	}
	for _, layer := range settings {
		if layer.Permissions.DisableAutoMode == "disable" && (mode == "auto" || mode == "" && !x.Resume) {
			mode = "default"
		}
		if mode == "bypassPermissions" && layer.Permissions.DisableBypassPermissionsMode == "disable" {
			mode = ""
			break
		}
	}
	list.DefaultPermission = permissionDefault("claude", mode)
}

func validPreviewModel(value string) string {
	value = strings.TrimSpace(value)
	if !prefs.ValidModel(value) || strings.EqualFold(value, "default") || strings.EqualFold(value, "inherit") {
		return ""
	}
	return value
}

func permissionDefault(provider, mode string) string {
	if provider == "claude" && mode == "manual" {
		return "default"
	}
	if (prefs.Preferences{Provider: provider, PermissionMode: mode}).Validate() == nil {
		return mode
	}
	return ""
}

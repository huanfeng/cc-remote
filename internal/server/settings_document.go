package server

import (
	"encoding/json"
)

const nativeRedacted = "[CC Remote: preserved on server]"

// Sensitive containers and unknown extension settings stay write-only. Public
// native settings remain editable; the marker restores the exact original value.
func nativePublicKey(key string) bool {
	switch key {
	case "$schema", "model", "effortLevel", "permissions", "language", "cleanupPeriodDays", "theme", "includeCoAuthoredBy", "respectGitignore", "outputStyle", "alwaysThinkingEnabled", "showTurnDuration", "spinnerTipsEnabled", "autoUpdatesChannel", "skipDangerousModePermissionPrompt", "enableAllProjectMcpServers", "enabledMcpjsonServers", "disabledMcpjsonServers", "enabledPlugins":
		return true
	}
	return false
}
func redactNativeDocument(v map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(v))
	for key, value := range v {
		if nativePublicKey(key) {
			if key == "permissions" {
				var permissions map[string]json.RawMessage
				if json.Unmarshal(value, &permissions) == nil && permissions != nil {
					for name := range permissions {
						switch name {
						case "defaultMode", "allow", "deny", "ask", "additionalDirectories":
						default:
							permissions[name], _ = json.Marshal(nativeRedacted)
						}
					}
					out[key], _ = json.Marshal(permissions)
				} else {
					out[key] = value
				}
			} else {
				out[key] = value
			}
		} else {
			out[key], _ = json.Marshal(nativeRedacted)
		}
	}
	return out
}
func restoreNativeDocument(v, old map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(v))
	for key, value := range v {
		var marker string
		if json.Unmarshal(value, &marker) == nil && marker == nativeRedacted {
			if previous, ok := old[key]; ok {
				out[key] = previous
			}
		} else {
			if key == "permissions" {
				var next, previous map[string]json.RawMessage
				if json.Unmarshal(value, &next) == nil && next != nil && json.Unmarshal(old[key], &previous) == nil {
					for name, prior := range previous {
						var marker string
						current, ok := next[name]
						if !ok {
							switch name {
							case "defaultMode", "allow", "deny", "ask", "additionalDirectories":
							default:
								next[name] = prior
							}
						} else if json.Unmarshal(current, &marker) == nil && marker == nativeRedacted {
							next[name] = prior
						}
					}
					out[key], _ = json.Marshal(next)
				} else {
					out[key] = value
				}
			} else {
				out[key] = value
			}
		}
	}
	// Removing permissions still preserves private extension values nested inside it.
	if _, present := v["permissions"]; !present {
		var previous map[string]json.RawMessage
		if json.Unmarshal(old["permissions"], &previous) == nil {
			for name := range previous {
				switch name {
				case "defaultMode", "allow", "deny", "ask", "additionalDirectories":
					delete(previous, name)
				}
			}
			if len(previous) > 0 {
				out["permissions"], _ = json.Marshal(previous)
			}
		}
	}
	// Removing a hidden container from the public document never deletes secrets.
	for key, value := range old {
		if !nativePublicKey(key) {
			if _, ok := out[key]; !ok {
				out[key] = value
			}
		}
	}
	return out
}

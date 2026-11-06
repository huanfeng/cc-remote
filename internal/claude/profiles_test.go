package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileEnvironmentPreservesHostHooksAndRemovesOldAuth(t *testing.T) {
	values := map[string]string{"ANTHROPIC_BASE_URL": "https://new.example", "ANTHROPIC_API_KEY": "new-key"}
	env := ProfileEnvironment([]string{"PATH=/bin", "ANTHROPIC_AUTH_TOKEN=old-token", "ANTHROPIC_MODEL=old-model", "CLAUDE_CODE_OAUTH_TOKEN=old-oauth", "CC_REMOTE_ASK_FILE=private-hook"}, values)
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "old-") || !strings.Contains(joined, "CC_REMOTE_ASK_FILE=private-hook") || !strings.Contains(joined, "ANTHROPIC_API_KEY=new-key") {
		t.Fatal("incorrect environment")
	}
	o := Options{ProfileEnv: values, HostExecutable: "host", BridgeURL: "bridge", BridgeToken: "token"}
	file, _, e := writeConfigs(t.TempDir(), o)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Clean(file))
	var settings map[string]any
	if json.Unmarshal(b, &settings) != nil || settings["hooks"] == nil || settings["env"] == nil {
		t.Fatal("profile replaced hooks")
	}
}

func TestSupplierEnvClearsGlobalCompetingCredential(t *testing.T) {
	original := map[string]string{"ANTHROPIC_AUTH_TOKEN": "global-token", "ANTHROPIC_DEFAULT_SONNET_MODEL": "global-model"}
	for k, v := range SupplierEnv(map[string]string{"ANTHROPIC_API_KEY": "new-key", "ANTHROPIC_BASE_URL": "https://new.example"}) {
		original[k] = v
	}
	if original["ANTHROPIC_AUTH_TOKEN"] != "" || original["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "" || original["ANTHROPIC_API_KEY"] != "new-key" {
		t.Fatal("global credentials survived settings merge")
	}
}

func TestRegularSupplierIsNeverTreatedAsOverlay(t *testing.T) {
	// The production chain: SupplierEnv output is what reaches ProfileEnvironment.
	values := SupplierEnv(map[string]string{"ANTHROPIC_BASE_URL": "https://relay.example", "ANTHROPIC_API_KEY": "k"})
	env := strings.Join(ProfileEnvironment([]string{"ANTHROPIC_CUSTOM_HEADERS=Authorization: host-secret", "ANTHROPIC_BETAS=x", "PATH=/bin"}, values), "\n")
	if strings.Contains(env, "host-secret") || strings.Contains(env, "ANTHROPIC_BETAS") || !strings.Contains(env, "PATH=/bin") {
		t.Fatal("a regular supplier must still drop every host ANTHROPIC_* variable:", env)
	}
}

func TestOAuthOverlayKeepsNativeModelsAndClearsConflicts(t *testing.T) {
	values := map[string]string{OAuthEnvKey: "oat-new"}
	env := strings.Join(ProfileEnvironment([]string{"PATH=/bin", "ANTHROPIC_AUTH_TOKEN=old", "ANTHROPIC_API_KEY=old", "ANTHROPIC_BASE_URL=https://third.example", "ANTHROPIC_MODEL=native-model", "CLAUDE_CODE_OAUTH_TOKEN=oat-old"}, values), "\n")
	if strings.Contains(env, "old") || strings.Contains(env, "third.example") || !strings.Contains(env, "ANTHROPIC_MODEL=native-model") || !strings.Contains(env, "CLAUDE_CODE_OAUTH_TOKEN=oat-new") || !strings.Contains(env, "PATH=/bin") {
		t.Fatal("overlay must replace credentials only:", env)
	}
	settings := SupplierEnv(values)
	if len(settings) != 4 || settings[OAuthEnvKey] != "oat-new" || settings["ANTHROPIC_API_KEY"] != "" || settings["ANTHROPIC_AUTH_TOKEN"] != "" || settings["ANTHROPIC_BASE_URL"] != "" {
		t.Fatal("overlay settings env must blank only conflicting credentials:", settings)
	}
	if _, touched := settings["ANTHROPIC_MODEL"]; touched {
		t.Fatal("overlay must not touch model variables")
	}
}

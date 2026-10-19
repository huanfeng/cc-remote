package service

import (
	"fmt"
	"io"
	"net/url"
	"runtime"
	"slices"
	"strings"

	"cc-remote/internal/i18n"
)

// proxyEnvNames are captured automatically from the installing process. Only
// proxy settings are captured: anything else (API keys, base URLs) must be
// passed explicitly with --env.
var proxyEnvNames = []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"}

const managerEnvName = "CC_REMOTE_SERVICE_MANAGER"

func isProxyEnv(key string) bool {
	return slices.Contains(proxyEnvNames, strings.ToUpper(key))
}

// envKeyEqual compares variable names; Windows names are case-insensitive.
func envKeyEqual(a, b string, foldCase bool) bool {
	return a == b || (foldCase && strings.EqualFold(a, b))
}

// environKey returns the name of a "KEY=VALUE" entry. Windows keeps hidden
// per-drive entries such as "=C:=C:\"; their name starts with "=".
func environKey(entry string) (key, value string) {
	start := 0
	if strings.HasPrefix(entry, "=") {
		start = 1
	}
	i := strings.IndexByte(entry[start:], '=')
	if i < 0 {
		return entry, ""
	}
	i += start
	return entry[:i], entry[i+1:]
}

// captureProxyEnv returns the non-empty proxy variables in environ with their
// exact name. With foldCase (Windows) only the first spelling of each name is kept.
func captureProxyEnv(environ []string, foldCase bool) map[string]string {
	out := map[string]string{}
	for _, entry := range environ {
		k, v := environKey(entry)
		if !isProxyEnv(k) || strings.TrimSpace(v) == "" {
			continue
		}
		dup := false
		for existing := range out {
			if envKeyEqual(existing, k, foldCase) {
				dup = true
				break
			}
		}
		if !dup {
			out[k] = v
		}
	}
	return out
}

// setEnv sets key in env, replacing any spelling of it when foldCase is set.
func setEnv(env map[string]string, key, value string, foldCase bool) {
	deleteEnv(env, key, foldCase)
	env[key] = value
}

func deleteEnv(env map[string]string, key string, foldCase bool) bool {
	removed := false
	for k := range env {
		if envKeyEqual(k, key, foldCase) {
			delete(env, k)
			removed = true
		}
	}
	return removed
}

// parseEnvFlag splits a --env KEY=VALUE value; an empty VALUE means removal.
func parseEnvFlag(s string) (key, value string, err error) {
	key, value, ok := strings.Cut(s, "=")
	if !ok {
		return "", "", i18n.Errorf("invalid --env %q: expected KEY=VALUE (KEY= removes it)", "无效的 --env %q：应为 KEY=VALUE（KEY= 表示删除）", s)
	}
	if key == "" || strings.ContainsAny(key, " \t\r\n\x00") {
		return "", "", i18n.Errorf("invalid --env %q: bad variable name", "无效的 --env %q：变量名不合法", s)
	}
	if strings.ContainsRune(value, 0) {
		return "", "", i18n.Errorf("invalid --env %q: value contains NUL", "无效的 --env %q：值包含 NUL", key)
	}
	if strings.EqualFold(key, managerEnvName) {
		return "", "", i18n.Errorf("--env %s is set by the service itself", "--env %s 由服务自己设置", managerEnvName)
	}
	return key, value, nil
}

// envDisplay shows a variable for logs and status: proxy values in full
// (passwords in proxy URLs redacted), everything else masked.
func envDisplay(key, value string) string {
	if !isProxyEnv(key) {
		return key + "=****"
	}
	if u, err := url.Parse(value); err == nil && u.User != nil {
		value = u.Redacted()
	}
	return key + "=" + value
}

// EnvSummary lists the service environment with secret values masked.
func EnvSummary(env map[string]string) string {
	parts := make([]string, 0, len(env))
	for _, k := range sortedKeys(env) {
		parts = append(parts, envDisplay(k, env[k]))
	}
	return strings.Join(parts, " ")
}

// applyEnv updates cfg.Env: optional capture of proxy variables from environ,
// then explicit --env values (KEY= removes KEY). cfg.Env is non-nil afterwards
// so a later reinstall knows the environment was already configured.
func applyEnv(cfg *Config, capture bool, environ, envVals []string, foldCase bool, out io.Writer) error {
	type kv struct{ k, v string }
	parsed := make([]kv, 0, len(envVals))
	for _, s := range envVals {
		k, v, err := parseEnvFlag(s)
		if err != nil {
			return err
		}
		parsed = append(parsed, kv{k, v})
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	if capture {
		got := captureProxyEnv(environ, foldCase)
		for _, k := range sortedKeys(got) {
			setEnv(cfg.Env, k, got[k], foldCase)
			cfg.EnvRemoved = withoutKey(cfg.EnvRemoved, k, foldCase)
		}
		if len(got) == 0 {
			fmt.Fprintln(out, i18n.T("No proxy variables (HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY) found in the current environment", "当前环境中没有代理变量（HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY）"))
		} else {
			fmt.Fprint(out, i18n.F("Captured service environment: %s\n", "已捕获服务环境变量：%s\n", EnvSummary(got)))
		}
	}
	for _, p := range parsed {
		if p.v == "" {
			if deleteEnv(cfg.Env, p.k, foldCase) {
				fmt.Fprint(out, i18n.F("Removed service environment: %s\n", "已删除服务环境变量：%s\n", p.k))
				cfg.EnvRemoved = append(withoutKey(cfg.EnvRemoved, p.k, foldCase), p.k)
			}
			continue
		}
		setEnv(cfg.Env, p.k, p.v, foldCase)
		cfg.EnvRemoved = withoutKey(cfg.EnvRemoved, p.k, foldCase)
		fmt.Fprint(out, i18n.F("Set service environment: %s\n", "已设置服务环境变量：%s\n", envDisplay(p.k, p.v)))
	}
	return nil
}

// withoutKey drops key from keys.
func withoutKey(keys []string, key string, foldCase bool) []string {
	var out []string
	for _, k := range keys {
		if !envKeyEqual(k, key, foldCase) {
			out = append(out, k)
		}
	}
	return out
}

// mergeEnviron returns base with overrides applied (replacing any existing
// entry of the same name, case-insensitively with foldCase).
func mergeEnviron(base []string, overrides map[string]string, foldCase bool) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		k, _ := environKey(entry)
		replaced := false
		for ok := range overrides {
			if envKeyEqual(k, ok, foldCase) {
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, entry)
		}
	}
	for _, k := range sortedKeys(overrides) {
		result = append(result, k+"="+overrides[k])
	}
	return result
}

// serviceEnvFor merges the user's service environment over the base entries
// (PATH, CC_REMOTE_SERVICE_MANAGER) of systemd/launchd; the manager marker
// cannot be overridden.
func serviceEnvFor(base, extra map[string]string) map[string]string {
	env := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		env[k] = v
	}
	for k, v := range extra {
		if k != managerEnvName {
			env[k] = v
		}
	}
	return env
}

func foldEnvCase() bool { return runtime.GOOS == "windows" }

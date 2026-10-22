//go:build linux || darwin

package service

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"cc-remote/internal/i18n"
)

func runQuiet(name string, args ...string) (string, error) {
	if managerDisabled != nil {
		return "", managerDisabled
	}
	b, err := exec.Command(name, args...).CombinedOutput()
	out := strings.TrimSpace(string(b))
	if err != nil {
		if out != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

func installBinaries(src string, dryRun bool, out io.Writer) error {
	dst, err := InstalledExe()
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Fprint(out, i18n.F("[dry-run] copy %s -> %s\n", "[dry-run] 复制 %s -> %s\n", src, dst))
		return nil
	}
	if !samePath(src, dst) {
		if err := copyExecutable(src, dst, nil); err != nil {
			return err
		}
	}
	fmt.Fprint(out, i18n.F("Installed %s\n", "已安装 %s\n", dst))
	return nil
}

// serviceEnv captures the install-time PATH: service managers start with a
// minimal PATH that usually lacks claude and node. The directories of the
// installed service's PATH stay too: an update run from a shell with a shorter
// PATH (ssh without the login profile, say) must not lose where node lives.
func serviceEnv(manager string) map[string]string {
	env := map[string]string{"CC_REMOTE_SERVICE_MANAGER": manager}
	if p := UnionPath(os.Getenv("PATH"), installedPath(), ":"); p != "" {
		env["PATH"] = p
	}
	return env
}

// pathHint warns when the service would not find node: Claude Code plugins
// (hooks, MCP servers) often run with it.
func pathHint(cfg *Config, out io.Writer) {
	if !onPath(serviceEnvFor(serviceEnv(managerName), cfg.Env)["PATH"], "node") {
		fmt.Fprint(out, warning(i18n.T(
			"node is not on the service's PATH: Claude Code plugins (hooks, MCP servers) may not run in sessions the host starts. Run cc-remote service install again from a login shell that finds node, or set it with cc-remote service config --env PATH=…",
			"服务的 PATH 中找不到 node：宿主启动的会话里，Claude Code 的插件（hook、MCP 服务）可能无法运行。请在能找到 node 的登录 shell 中重新运行 cc-remote service install，或用 cc-remote service config --env PATH=… 指定。")))
	}
}

func onPath(path, name string) bool {
	for dir := range strings.SplitSeq(path, ":") {
		if info, err := os.Stat(filepath.Join(dir, name)); dir != "" && err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

func homeDir(fallback string) string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return fallback
}

func firewallHint([]string, io.Writer) {}

// Supervise is only used by the Windows scheduled task.
func Supervise([]string) error {
	return i18n.Errorf("supervise is only used on Windows", "supervise 仅在 Windows 上使用")
}

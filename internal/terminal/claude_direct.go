package terminal

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"golang.org/x/term"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"
)

// RunClaudeDirect launches the native Claude Code CLI right here in this
// terminal, applying a CC-Switch supplier through environment variables. The
// host is contacted only to read the supplier catalog: no session is created,
// nothing is recorded, and Ctrl+C belongs to Claude Code alone.
func RunClaudeDirect(args []string, profile, defaultDataDir string, ensureHost func(dataDir, host string) error) error {
	fs := flag.NewFlagSet("cc-remote claude", flag.ExitOnError)
	host := fs.String("host", "http://127.0.0.1:8787", i18n.T("host to read the supplier catalog from", "读取供应商目录的宿主"))
	dataDir := fs.String("data-dir", defaultDataDir, i18n.T("SQLite and access-token directory (or CC_REMOTE_DATA_DIR)", "SQLite 数据库和访问令牌所在目录（或 CC_REMOTE_DATA_DIR）"))
	claudeBin := fs.String("claude", "claude", i18n.T("Claude Code executable to launch", "要启动的 Claude Code 可执行文件"))
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), i18n.T(`usage: cc-remote claude [--profile ID] [--claude BIN] [-- claude arguments]

Run the native Claude Code CLI in this terminal. Unlike ccr, the host does not
hold the process: this is plain Claude Code, with a CC-Switch supplier applied
through environment variables so normal "cc" workflows can pick suppliers too.
`, `用法：cc-remote claude [--profile ID] [--claude BIN] [-- claude 参数]

在当前终端直接运行原生 Claude Code CLI。与 ccr 不同，宿主不持有进程：这就是
普通的 Claude Code，仅通过环境变量应用一个 CC-Switch 供应商配置，让普通的
"cc" 工作流也能选择供应商。
`))
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if err := checkHost(*host); err != nil {
		return err
	}
	if err := ensureHost(*dataDir, *host); err != nil {
		return err
	}
	api, err := newClient(*host, *dataDir)
	if err != nil {
		return err
	}
	env, err := api.supplierEnvironment(profile, term.IsTerminal(int(os.Stdin.Fd())))
	if err != nil {
		return err
	}
	// Claude Code resolves git and shell integrations through a real console:
	// stdin, stdout and stderr pass through untouched.
	cmd := exec.Command(*claudeBin, fs.Args()...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		return i18n.Errorf("cannot launch %s: %w", "无法启动 %s：%w", *claudeBin, err)
	}
	return nil
}

// supplierEnvironment reads the supplier catalog from the host, picks a Claude
// supplier (Enter keeps the default, "@native" or "" applies nothing) and
// returns this process's environment with the supplier's variables applied.
func (c *client) supplierEnvironment(explicit string, interactive bool) ([]string, error) {
	var catalog struct {
		Profiles        []supplierProfile `json:"profiles"`
		DefaultProfiles map[string]string `json:"defaultProfiles"`
	}
	if err := c.do("GET", "/api/profiles", nil, &catalog); err != nil {
		var refused hostError
		if errors.As(err, &refused) && refused.status == http.StatusNotFound && explicit == "" {
			return os.Environ(), nil // host predates supplier profiles
		}
		return nil, err
	}
	list := make([]supplierProfile, 0, len(catalog.Profiles))
	for _, p := range catalog.Profiles {
		if p.App == "claude" {
			list = append(list, p)
		}
	}
	chosen := explicit
	if chosen == "" && interactive && len(list) > 0 {
		list = append(list, supplierProfile{ID: "@native", Name: i18n.T("Native configuration", "宿主原生配置"), App: "claude"})
		chosen, _ = chooseProfile(list, catalog.DefaultProfiles["claude"])
	}
	if chosen == "" || chosen == "@native" {
		return os.Environ(), nil
	}
	for _, p := range list {
		if p.ID == chosen || strings.TrimPrefix(p.ID, "ccswitch:") == chosen {
			if len(p.Env) == 0 {
				return os.Environ(), nil
			}
			return claude.ProfileEnvironment(os.Environ(), p.Env), nil
		}
	}
	if !interactive {
		return os.Environ(), nil
	}
	return nil, i18n.Errorf("Claude supplier configuration %q was not found on the host", "宿主上找不到 Claude 供应商配置 %q", chosen)
}

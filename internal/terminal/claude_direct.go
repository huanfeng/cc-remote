package terminal

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"

	"golang.org/x/term"
)

// RunClaudeDirect launches the native Claude Code CLI right here in this
// terminal, applying a CC-Switch supplier through environment variables. The
// host is contacted only to read the supplier catalog: no session is created,
// nothing is recorded, and Ctrl+C belongs to Claude Code alone.
func RunClaudeDirect(args []string, defaultDataDir string, ensureHost func(dataDir, host string) error) error {
	fs := flag.NewFlagSet("ccr local", flag.ContinueOnError)
	host := fs.String("host", "http://127.0.0.1:8787", i18n.T("host to read the supplier catalog from", "读取供应商目录的宿主"))
	dataDir := fs.String("data-dir", defaultDataDir, i18n.T("SQLite and access-token directory (or CC_REMOTE_DATA_DIR)", "SQLite 数据库和访问令牌所在目录（或 CC_REMOTE_DATA_DIR）"))
	profileFlag := fs.String("supplier", "", i18n.T("supplier configuration to apply (empty: pick interactively, Enter = default)", "要应用的供应商配置（留空：交互选择，回车 = 默认）"))
	claudeBin := fs.String("claude", "claude", i18n.T("Claude Code executable to launch", "要启动的 Claude Code 可执行文件"))
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), i18n.T(`usage: ccr local [claude] [--supplier ID] [--claude BIN] [-- Claude arguments]

Run Claude Code in the current terminal with the selected CC-Switch supplier.
`, `用法：ccr local [claude] [--supplier ID] [--claude BIN] [-- Claude 参数]

在当前终端运行 Claude Code，并应用所选的 CC-Switch 供应商。
`))
		fs.PrintDefaults()
	}
	words, native, err := parseCLIFlags(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(words) > 1 || len(words) == 1 && words[0] != "claude" {
		fs.Usage()
		return errors.New(i18n.T("invalid local command arguments", "本地命令参数无效"))
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
	env, supplier, err := api.supplierEnvironment(*profileFlag, term.IsTerminal(int(os.Stdin.Fd())))
	if err != nil {
		return err
	}
	// Mode banner before Claude Code takes over the screen: it says which mode
	// this is and which supplier is in effect (scrollback keeps a copy).
	if supplier == "" {
		fmt.Println(i18n.T("⏺ native claude · no supplier applied (plain login)", "⏺ 原生 claude · 未应用供应商（原生登录）"))
	} else {
		fmt.Println(i18n.F("⏺ native claude · supplier: %s · %d connection variables injected", "⏺ 原生 claude · 供应商：%s（已注入 %d 个连接变量）", supplier, injected(env)))
	}
	// Claude Code resolves git and shell integrations through a real console:
	// stdin, stdout and stderr pass through untouched.
	cmd := exec.Command(*claudeBin, native...)
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

// injected counts the supplier variables that differ from the current environment.
func injected(env []string) int {
	base := map[string]bool{}
	for _, kv := range os.Environ() {
		base[kv] = true
	}
	n := 0
	for _, kv := range env {
		if !base[kv] {
			n++
		}
	}
	return n
}

// supplierEnvironment reads the supplier catalog from the host, picks a Claude
// supplier (Enter keeps the default, "@native" or "" applies nothing) and
// returns this process's environment with the supplier's variables applied,
// together with the supplier's display name ("" when none).
func (c *client) supplierEnvironment(explicit string, interactive bool) ([]string, string, error) {
	var catalog struct {
		Profiles        []supplierProfile `json:"profiles"`
		DefaultProfiles map[string]string `json:"defaultProfiles"`
	}
	if err := c.do("GET", "/api/profiles", nil, &catalog); err != nil {
		var refused hostError
		if errors.As(err, &refused) && refused.status == http.StatusNotFound && explicit == "" {
			return os.Environ(), "", nil // host predates supplier profiles
		}
		return nil, "", err
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
		var err error
		chosen, err = c.chooseSupplier("claude", list, catalog.DefaultProfiles["claude"])
		if err != nil {
			return nil, "", err
		}
		if chosen == "" {
			chosen = catalog.DefaultProfiles["claude"] // Enter keeps the default
		}
	}
	if chosen == "" || chosen == "@native" {
		return os.Environ(), "", nil
	}
	// The catalog carries names only: the connection variables come one by one
	// from the host, which keeps secrets out of every list response.
	var reply struct {
		Name string            `json:"name"`
		Env  map[string]string `json:"env"`
	}
	if err := c.do("GET", "/api/profiles/"+chosen+"/env", nil, &reply); err != nil {
		return nil, "", err
	}
	if len(reply.Env) == 0 {
		return os.Environ(), reply.Name, nil
	}
	return claude.ProfileNameEnvironment(claude.ProfileEnvironment(os.Environ(), reply.Env), reply.Name), reply.Name, nil
}

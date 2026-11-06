package terminal

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"cc-remote/internal/i18n"
)

type profileEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	App  string `json:"app"`
	Kind string `json:"kind"`
}

type profileList struct {
	Profiles           []profileEntry    `json:"profiles"`
	DefaultProfiles    map[string]string `json:"defaultProfiles"`
	ConfiguredDefaults map[string]string `json:"configuredDefaults"`
}

// RunProfile is `cc-remote profile`: list the suppliers new sessions can use
// and choose which one is the default for the web and ccr alike.
func RunProfile(args []string, defaultDataDir string, ensureHost func(dataDir, host string) error) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(profileUsage())
		return nil
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("profile "+sub, flag.ContinueOnError)
	host := fs.String("host", "http://127.0.0.1:8787", i18n.T("cc-remote host URL", "cc-remote 宿主 URL"))
	dataDir := fs.String("data-dir", defaultDataDir, i18n.T("cc-remote data directory (access token)", "cc-remote 数据目录（访问令牌所在）"))
	app := fs.String("app", "claude", i18n.T("claude or codex", "claude 或 codex"))
	fs.Usage = func() { fmt.Fprint(fs.Output(), profileUsage()) }
	var names []string
	for {
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		names = append(names, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if (sub != "ls" && sub != "list" && sub != "default") || len(names) > 1 || (sub != "default" && len(names) > 0) {
		return i18n.Errorf("unknown profile command %q (try: ls, default)", "未知的 profile 命令 %q（可用：ls、default）", sub)
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
	return runProfile(api, os.Stdout, sub, *app, names)
}

func profileUsage() string {
	return i18n.T(`usage: cc-remote profile <command> [--app claude|codex]

  ls                  list the suppliers new sessions can use; * marks the default
  default             show the default supplier
  default NAME        make NAME (a name or id from ls) the default, for the web and ccr
  default @native     default to the host's native configuration
  default follow      no choice of its own: follow the CC-Switch current provider

The default applies to new sessions only; existing ones keep their supplier.
`, `用法：cc-remote profile <命令> [--app claude|codex]

  ls                  列出新会话可用的供应商，* 标记默认项
  default             显示默认供应商
  default 名称        把名称（ls 里的名称或 id）设为默认，网页和 ccr 一致
  default @native     默认使用宿主原生配置
  default follow      不单独指定：跟随 CC-Switch 当前选中的供应商

默认只影响新建的会话；已有会话保持原供应商。
`)
}

func runProfile(api doer, out io.Writer, sub, app string, names []string) error {
	var list profileList
	if err := api.do("GET", "/api/profiles", nil, &list); err != nil {
		return err
	}
	effective, configured := list.DefaultProfiles[app], list.ConfiguredDefaults[app]
	label := func(id string) string {
		switch id {
		case "":
			return i18n.T("native configuration", "宿主原生配置")
		case "@native":
			return i18n.T("native configuration", "宿主原生配置")
		}
		for _, p := range list.Profiles {
			if p.ID == id && p.App == app {
				return p.Name
			}
		}
		return i18n.F("unavailable (%s)", "不可用（%s）", id)
	}
	switch sub {
	case "ls", "list":
		fmt.Fprintf(out, "%s\t@native\t%s\n", mark(effective == "" || effective == "@native"), i18n.T("native configuration", "宿主原生配置"))
		for _, p := range list.Profiles {
			if p.App != app {
				continue
			}
			kind := p.Kind
			if kind == "" {
				kind = "cc-switch"
			}
			fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", mark(p.ID == effective), p.ID, p.Name, kind)
		}
	case "default":
		if len(names) == 0 {
			source := i18n.T("CC-Switch current provider", "CC-Switch 当前项")
			if configured != "" {
				source = i18n.T("set by you", "手动指定")
			}
			fmt.Fprintln(out, i18n.F("Default for %s: %s (%s)", "%s 的默认供应商：%s（%s）", app, label(effective), source))
			return nil
		}
		want := names[0]
		switch strings.ToLower(want) {
		case "follow", "-":
			want = ""
		case "native":
			want = "@native"
		default:
			if want != "@native" {
				id, err := matchProfile(list, app, want)
				if err != nil {
					return err
				}
				want = id
			}
		}
		if err := api.do("PUT", "/api/profiles/default", map[string]string{"app": app, "profileId": want}, nil); err != nil {
			return err
		}
		if want == "" {
			fmt.Fprintln(out, i18n.T("The default now follows the CC-Switch current provider.", "默认已改为跟随 CC-Switch 当前项。"))
		} else {
			fmt.Fprintln(out, i18n.F("Default for %s is now %s. New sessions use it; existing ones keep theirs.", "%s 的默认供应商已设为 %s。新会话使用它，已有会话不变。", app, label(want)))
		}
	}
	return nil
}

func mark(on bool) string {
	if on {
		return "*"
	}
	return " "
}

func matchProfile(list profileList, app, want string) (string, error) {
	var hit []profileEntry
	for _, p := range list.Profiles {
		if p.App == app && (p.ID == want || strings.TrimPrefix(p.ID, "ccswitch:") == want || strings.TrimPrefix(p.ID, "oauth:") == want || strings.EqualFold(p.Name, want)) {
			hit = append(hit, p)
		}
	}
	switch len(hit) {
	case 1:
		return hit[0].ID, nil
	case 0:
		return "", i18n.Errorf("no supplier named %q (see: cc-remote profile ls)", "没有名为 %q 的供应商（见：cc-remote profile ls）", want)
	}
	return "", i18n.Errorf("%q matches several suppliers; use the id", "%q 对应多个供应商，请改用 id", want)
}

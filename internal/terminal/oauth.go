package terminal

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"cc-remote/internal/i18n"
)

type oauthItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// doer is the part of client the OAuth commands use; tests substitute a fake.
type doer interface {
	do(method, path string, body any, out any) error
}

// RunOAuth is `cc-remote oauth`: list, add, replace the token of, rename and
// remove the OAuth tokens kept on the host. It talks to the running host, so
// the file is only ever written by one process.
func RunOAuth(args []string, defaultDataDir string, ensureHost func(dataDir, host string) error) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(oauthUsage())
		return nil
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("oauth "+sub, flag.ContinueOnError)
	host := fs.String("host", "http://127.0.0.1:8787", i18n.T("cc-remote host URL", "cc-remote 宿主 URL"))
	dataDir := fs.String("data-dir", defaultDataDir, i18n.T("cc-remote data directory (access token)", "cc-remote 数据目录（访问令牌所在）"))
	token := fs.String("token", "", i18n.T("the OAuth token (otherwise read from CC_REMOTE_OAUTH_TOKEN, stdin or a hidden prompt)", "OAuth 令牌（否则读 CC_REMOTE_OAUTH_TOKEN、标准输入或隐藏输入）"))
	fs.Usage = func() { fmt.Fprint(fs.Output(), oauthUsage()) }
	// Flags may come before or after the names.
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
	need := map[string]int{"ls": 0, "list": 0, "add": 1, "token": 1, "rename": 2, "rm": 1, "remove": 1}
	want, ok := need[sub]
	if !ok {
		return i18n.Errorf("unknown oauth command %q (try: ls, add, token, rename, rm)", "未知的 oauth 命令 %q（可用：ls、add、token、rename、rm）", sub)
	}
	if len(names) != want {
		return i18n.Errorf("wrong number of arguments for oauth %s", "oauth %s 的参数个数不对", sub)
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
	return runOAuth(api, os.Stdout, sub, names, func() (string, error) { return readOAuthToken(*token) })
}

func oauthUsage() string {
	return i18n.T(`usage: cc-remote oauth <command> [flags]

Manage the OAuth tokens (from "claude setup-token") kept on the host. A session
that picks one runs with the native Claude Code configuration and only that login.

  ls                       list tokens (names only, never the token)
  add NAME                 add a token; it is read from --token, CC_REMOTE_OAUTH_TOKEN,
                           stdin, or a hidden prompt (prefer those over --token: shell
                           history keeps flags)
  token NAME               replace the token of NAME
  rename NAME NEW          rename
  rm NAME                  remove (sessions using it must pick another configuration)

NAME may also be the id shown by ls. Use it with: ccr --new --profile oauth:<id>
`, `用法：cc-remote oauth <命令> [参数]

管理保存在宿主上的 OAuth 令牌（由 "claude setup-token" 生成）。选用其中一项的会话
沿用原生 Claude Code 配置，只替换登录令牌。

  ls                       列出令牌（只显示名称，不显示令牌）
  add 名称                 添加令牌；从 --token、环境变量 CC_REMOTE_OAUTH_TOKEN、
                           标准输入或隐藏输入读取（建议不用 --token：shell 历史会记录参数）
  token 名称               更换该名称的令牌
  rename 名称 新名称        改名
  rm 名称                  删除（使用它的会话需要重新选择配置）

名称也可以写 ls 显示的 id。使用方式：ccr --new --profile oauth:<id>
`)
}

func readOAuthToken(flagValue string) (string, error) {
	if flagValue != "" {
		return strings.TrimSpace(flagValue), nil
	}
	if v := strings.TrimSpace(os.Getenv("CC_REMOTE_OAUTH_TOKEN")); v != "" {
		return v, nil
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, i18n.T("OAuth token (hidden): ", "OAuth 令牌（不回显）："))
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return strings.TrimSpace(string(b)), err
	}
	b, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
	return strings.TrimSpace(string(b)), err
}

func runOAuth(api doer, out io.Writer, sub string, names []string, token func() (string, error)) error {
	var list struct {
		Profiles []oauthItem `json:"profiles"`
	}
	if err := api.do("GET", "/api/profiles/oauth", nil, &list); err != nil {
		var refused hostError
		if errors.As(err, &refused) && refused.status == 404 {
			return errors.New(i18n.T("this host predates OAuth tokens; update it first (cc-remote update)", "此宿主版本不支持 OAuth 令牌，请先更新（cc-remote update）"))
		}
		return err
	}
	bare := func(id string) string { return strings.TrimPrefix(id, "oauth:") }
	find := func(name string) (oauthItem, error) {
		var hit []oauthItem
		for _, p := range list.Profiles {
			if bare(p.ID) == bare(name) || strings.EqualFold(p.Name, name) {
				hit = append(hit, p)
			}
		}
		switch len(hit) {
		case 1:
			return hit[0], nil
		case 0:
			return oauthItem{}, i18n.Errorf("no OAuth token named %q (see: cc-remote oauth ls)", "没有名为 %q 的 OAuth 令牌（见：cc-remote oauth ls）", name)
		}
		return oauthItem{}, i18n.Errorf("%q matches several tokens; use the id", "%q 对应多个令牌，请改用 id", name)
	}
	noToken := func() (string, error) {
		t, err := token()
		if err != nil || t == "" {
			return "", errors.New(i18n.T("no token given", "没有提供令牌"))
		}
		return t, nil
	}
	switch sub {
	case "ls", "list":
		if len(list.Profiles) == 0 {
			fmt.Fprintln(out, i18n.T("No OAuth tokens. Add one: cc-remote oauth add NAME", "还没有 OAuth 令牌。添加：cc-remote oauth add 名称"))
		}
		for _, p := range list.Profiles {
			fmt.Fprintf(out, "%s\t%s\n", p.ID, p.Name)
		}
	case "add":
		t, err := noToken()
		if err != nil {
			return err
		}
		var made oauthItem
		if err = api.do("POST", "/api/profiles/oauth", map[string]string{"name": names[0], "token": t}, &made); err != nil {
			return err
		}
		fmt.Fprintln(out, i18n.F("Added %s (%s). Use: ccr --new --profile %s", "已添加 %s（%s）。使用：ccr --new --profile %s", made.Name, made.ID, made.ID))
	case "token":
		p, err := find(names[0])
		if err != nil {
			return err
		}
		t, err := noToken()
		if err != nil {
			return err
		}
		if err = api.do("PATCH", "/api/profiles/oauth/"+bare(p.ID), map[string]string{"token": t}, nil); err != nil {
			return err
		}
		fmt.Fprintln(out, i18n.F("Token of %s replaced; sessions pick it up on their next message.", "%s 的令牌已更换；会话在下一条消息时生效。", p.Name))
	case "rename":
		p, err := find(names[0])
		if err != nil {
			return err
		}
		if err = api.do("PATCH", "/api/profiles/oauth/"+bare(p.ID), map[string]string{"name": names[1]}, nil); err != nil {
			return err
		}
		fmt.Fprintln(out, i18n.F("Renamed %s to %s.", "已将 %s 改名为 %s。", p.Name, names[1]))
	case "rm", "remove":
		p, err := find(names[0])
		if err != nil {
			return err
		}
		if err = api.do("DELETE", "/api/profiles/oauth/"+bare(p.ID), nil, nil); err != nil {
			return err
		}
		fmt.Fprintln(out, i18n.F("Removed %s.", "已删除 %s。", p.Name))
	}
	return nil
}

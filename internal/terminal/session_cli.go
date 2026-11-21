package terminal

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"

	"golang.org/x/term"
)

type agentOptions struct {
	cwd, title, supplier, model, effort, permission, prompt string
	fresh, last, resume                                     bool
	nativeID                                                string
	native                                                  []string
}

func connectionFlags(fs *flag.FlagSet, defaultDataDir string) (*string, *string) {
	host := fs.String("host", defaultHost, i18n.T("CC Remote host URL", "CC Remote 宿主 URL"))
	dir := fs.String("data-dir", defaultDataDir, i18n.T("CC Remote data directory", "CC Remote 数据目录"))
	return host, dir
}

// parseCLIFlags accepts flags on either side of a subcommand or ID. The native
// argument tail is kept verbatim, including words that look like CCR flags.
func parseCLIFlags(fs *flag.FlagSet, args []string) (words, native []string, err error) {
	var flags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			native = args[i+1:]
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			words = append(words, arg)
			continue
		}
		flags = append(flags, arg)
		name, _, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if f := fs.Lookup(name); f != nil && !inline && i+1 < len(args) {
			if b, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !b.IsBoolFlag() {
				i++
				flags = append(flags, args[i])
			}
		}
	}
	return words, native, fs.Parse(flags)
}

// AgentCLI opens the selected provider's hosted terminal. An empty provider
// enables the project picker; recent enables the cross-project picker.
func AgentCLI(args []string, agent string, recent bool, defaultDataDir string, ensureHost func(string, string) error) error {
	fs := flag.NewFlagSet("ccr "+agent, flag.ContinueOnError)
	host, dir := connectionFlags(fs, defaultDataDir)
	var o agentOptions
	if !recent {
		fs.BoolVar(&o.fresh, "new", false, i18n.T("start a new terminal session", "新建终端会话"))
	}
	if agent == "" && !recent {
		fs.Func("resume", i18n.T("resume a native conversation by ID", "按原生 ID 恢复对话"), func(id string) error {
			id = strings.ToLower(strings.TrimSpace(id))
			if id == "" || strings.HasPrefix(id, "-") {
				return errors.New(i18n.T("--resume requires a native session ID", "--resume 需要原生会话 ID"))
			}
			o.resume, o.nativeID = true, id
			return nil
		})
	}
	if agent != "" {
		fs.StringVar(&o.cwd, "cwd", "", i18n.T("project directory on the host", "宿主上的项目目录"))
		fs.StringVar(&o.title, "title", "", i18n.T("session title", "会话标题"))
		fs.StringVar(&o.prompt, "prompt", "", i18n.T("first message", "首条消息"))
		fs.StringVar(&o.model, "model", "", i18n.T("model", "模型"))
		fs.BoolVar(&o.last, "last", false, i18n.T("resume the latest history in this project", "恢复当前项目最近的历史对话"))
		if agent == "claude" {
			fs.StringVar(&o.supplier, "supplier", "", i18n.T("supplier ID", "供应商 ID"))
			fs.StringVar(&o.effort, "effort", "", i18n.T("reasoning effort", "推理强度"))
			fs.StringVar(&o.permission, "permission-mode", "", i18n.T("permission mode", "权限模式"))
		}
	}
	fs.Usage = func() {
		switch {
		case agent != "":
			fmt.Fprintf(fs.Output(), i18n.T("Usage: ccr %[1]s [--new] [options] [-- native flags]\n       ccr %[1]s resume [native ID | --last] [options] [-- native flags]\n", "用法：ccr %[1]s [--new] [选项] [-- 原生选项]\n      ccr %[1]s resume [原生 ID | --last] [选项] [-- 原生选项]\n"), agent)
		case recent:
			fmt.Fprintln(fs.Output(), i18n.T("Usage: ccr ls", "用法：ccr ls"))
		default:
			fmt.Fprintln(fs.Output(), i18n.T("Usage: ccr [--new | --resume <native ID>]", "用法：ccr [--new | --resume <原生 ID>]"))
		}
		fs.PrintDefaults()
	}
	words, native, err := parseCLIFlags(fs, args)
	if err != nil {
		return err
	}
	if agent != "" && len(words) > 0 && words[0] == "resume" && len(words) <= 2 {
		o.resume = true
		if len(words) == 2 {
			o.nativeID = words[1]
		}
		words = nil
	}
	if len(words) > 0 || agent == "" && len(native) > 0 || o.last && (!o.resume || o.nativeID != "") || o.resume && (o.fresh || o.prompt != "") {
		fs.Usage()
		return errors.New(i18n.T("invalid terminal command arguments", "终端命令参数无效"))
	}
	o.native = native
	if o.cwd == "" && !recent {
		o.cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	if err := requireAgentTerminal(); err != nil {
		return err
	}
	if err := checkHost(*host); err != nil {
		return err
	}
	if err := ensureHost(*dir, *host); err != nil {
		return err
	}
	api, err := newClient(*host, *dir)
	if err != nil {
		return err
	}
	target, err := api.openAgent(agent, o, recent)
	if errors.Is(err, errPickerCancelled) {
		return nil
	}
	if err != nil {
		return err
	}
	if !target.Online {
		_, err := attachedSession([]remoteSession{target}, target.ID)
		return err
	}
	if !confirmAttached(target) {
		return nil
	}
	return api.attach(target)
}

func requireAgentTerminal() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New(i18n.T("opening an Agent terminal requires an interactive terminal", "接入 Agent 终端需要交互式终端"))
	}
	return nil
}

func (c *client) openAgent(agent string, o agentOptions, recent bool) (remoteSession, error) {
	if o.resume {
		if agent == "" {
			target, err := c.detectResumeTarget(o.nativeID)
			if err != nil || target.Online {
				return target, err
			}
			o.cwd = target.Cwd
			return c.startAgent(target.Provider, o, o.nativeID)
		}
		return c.resumeAgent(agent, o)
	}
	create := func() (remoteSession, error) {
		newAgent := agent
		if newAgent == "" {
			items := []pickerItem{
				{id: "claude", lines: [2]string{"1  Claude Code"}, search: "Claude Code"},
				{id: "codex", lines: [2]string{"2  Codex"}, search: "Codex"},
			}
			var err error
			newAgent, err = pickItems(items, "claude", nil, nil)
			if err != nil {
				return remoteSession{}, err
			}
		}
		open := o
		if open.cwd == "" {
			var err error
			open.cwd, err = os.Getwd()
			if err != nil {
				return remoteSession{}, err
			}
		}
		return c.startAgent(newAgent, open, "")
	}
	if o.fresh || o.title != "" || o.supplier != "" || o.prompt != "" || o.model != "" || o.effort != "" || o.permission != "" || len(o.native) > 0 {
		return create()
	}
	load := func() ([]remoteSession, error) {
		all, err := c.sessions()
		var list []remoteSession
		for _, x := range all {
			if x.Mode == "terminal" && (recent || x.Online) && (agent == "" || x.agent() == agent) && (recent || samePath(x.Cwd, o.cwd)) {
				list = append(list, x)
			}
		}
		return list, err
	}
	list, err := load()
	if err != nil {
		return remoteSession{}, err
	}
	if len(list) == 0 {
		return create()
	}
	if agent != "" && len(list) == 1 {
		return list[0], nil
	}
	return chooseWithReload(list, create, load)
}

func (c *client) resumeAgent(agent string, o agentOptions) (remoteSession, error) {
	if o.nativeID != "" {
		return c.resumeAgentID(agent, o, o.nativeID)
	}
	history := func() ([]remoteSession, error) {
		var all []remoteSession
		err := c.do(http.MethodGet, "/api/native/sessions?provider="+agent, nil, &all)
		var list []remoteSession
		for _, x := range all {
			if !samePath(x.Cwd, o.cwd) {
				continue
			}
			x.Provider = agent
			list = append(list, x)
		}
		return list, err
	}
	list, err := history()
	if err != nil {
		return remoteSession{}, err
	}
	if len(list) == 0 {
		return remoteSession{}, i18n.Errorf("no %s history in %s", "%s 在目录 %s 下没有历史对话", agent, o.cwd)
	}
	chosen := list[0]
	if len(list) > 1 && !o.last {
		chosen, err = chooseWithReload(list, nil, history)
		if err != nil {
			return remoteSession{}, err
		}
	}
	o.cwd = chosen.Cwd
	return c.resumeAgentID(agent, o, chosen.ID)
}

func (c *client) resumeAgentID(agent string, o agentOptions, id string) (remoteSession, error) {
	if agent == "claude" {
		running, err := c.liveClaudeTerminal(id)
		if err != nil || running.ID != "" {
			return running, err
		}
		// Trust confirmation must name the directory saved by Claude, which
		// can differ from the current project when an explicit ID was given.
		if o.nativeID != "" {
			history, err := c.nativeSession(agent, id)
			if err != nil {
				return remoteSession{}, err
			}
			o.cwd = history.Cwd
		}
	}
	return c.startAgent(agent, o, id)
}

func (c *client) liveClaudeTerminal(id string) (remoteSession, error) {
	all, err := c.sessions()
	if err != nil {
		return remoteSession{}, err
	}
	for _, x := range all {
		if x.Mode == "terminal" && x.Online && x.agent() == "claude" && strings.EqualFold(x.NativeID, id) {
			return x, nil
		}
	}
	return remoteSession{}, nil
}

func (c *client) nativeSession(agent, id string) (remoteSession, error) {
	var history struct {
		Session remoteSession `json:"session"`
	}
	path := "/api/native/sessions/" + url.PathEscape(strings.ToLower(id)) + "?provider=" + agent + "&tail=1&compact=1"
	if err := c.do(http.MethodGet, path, nil, &history); err != nil {
		return remoteSession{}, err
	}
	if !strings.EqualFold(history.Session.ID, id) {
		return remoteSession{}, i18n.Errorf("the host returned the wrong %s history ID", "宿主返回了错误的 %s 历史 ID", agent)
	}
	return remoteSession{ID: history.Session.ID, Cwd: history.Session.Cwd, Provider: agent}, nil
}

func (c *client) detectResumeTarget(id string) (remoteSession, error) {
	// A running Claude terminal can already have a native ID before Claude
	// writes a transcript. Codex's live thread identity can change in the TUI.
	match, err := c.liveClaudeTerminal(id)
	if err != nil {
		return remoteSession{}, err
	}
	for _, agent := range []string{"claude", "codex"} {
		if agent == "claude" && match.ID != "" {
			continue
		}
		history, err := c.nativeSession(agent, id)
		var refused hostError
		if errors.As(err, &refused) && refused.status == http.StatusNotFound {
			continue
		}
		if err != nil {
			return remoteSession{}, err
		}
		if match.ID != "" {
			return remoteSession{}, i18n.Errorf(
				"native ID %q matches both Claude and Codex; use ccr claude resume <ID> or ccr codex resume <ID>",
				"原生 ID %q 同时匹配 Claude 和 Codex，请用 ccr claude resume <ID> 或 ccr codex resume <ID>", id)
		}
		match = history
	}
	if match.ID == "" {
		return remoteSession{}, i18n.Errorf(
			"no Claude or Codex history found for native ID %q; for a CC Remote session ID use ccr session attach <ID>",
			"找不到原生 ID %q 对应的 Claude 或 Codex 历史。若使用的是 CCR 会话 ID，请运行 ccr session attach <ID>", id)
	}
	return match, nil
}

func (c *client) startAgent(agent string, o agentOptions, nativeID string) (remoteSession, error) {
	body := map[string]any{"provider": agent, "mode": "terminal", "cwd": o.cwd, "title": o.title, "start": true}
	if nativeID != "" {
		body["resume"] = nativeID
	}
	if agent == "claude" {
		for key, value := range map[string]string{"model": o.model, "effort": o.effort, "permissionMode": o.permission} {
			if value != "" {
				body[key] = value
			}
		}
		if len(o.native) > 0 {
			body["claudeArgs"] = o.native
		}
		if o.title == "" {
			for _, arg := range claude.ParseArgs(o.native) {
				if (arg.Name == "--name" || arg.Name == "-n") && len(arg.Values) > 0 {
					body["title"] = arg.Values[0]
					break
				}
			}
		}
		pick := c.selectProfile
		if nativeID != "" {
			pick = c.selectResumeProfile
		}
		profile, err := pick(o.supplier, term.IsTerminal(int(os.Stdin.Fd())))
		if err != nil {
			return remoteSession{}, err
		}
		if profile != "" {
			body["profileId"] = profile
		}
	} else {
		native := o.native
		if o.model != "" {
			native = append([]string{"--model=" + o.model}, native...)
		}
		if len(native) > 0 {
			body["codexArgs"] = native
		}
		if o.prompt != "" {
			body["prompt"] = o.prompt
		}
	}
	x, err := c.start(body, o.cwd)
	if err != nil {
		return remoteSession{}, err
	}
	if agent == "claude" && o.prompt != "" {
		if err := c.do(http.MethodPost, "/api/sessions/"+url.PathEscape(x.ID)+"/messages", map[string]string{"text": o.prompt}, nil); err != nil {
			return remoteSession{}, err
		}
	}
	return x, nil
}

// SessionCLI lists recorded sessions or attaches to a live terminal by ID.
func SessionCLI(args []string, defaultDataDir string) error {
	fs := flag.NewFlagSet("ccr session", flag.ContinueOnError)
	host, dir := connectionFlags(fs, defaultDataDir)
	agent := fs.String("agent", "", i18n.T("filter by claude or codex (list only)", "按 claude 或 codex 筛选（仅 list）"))
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), i18n.T("Usage: ccr session list [--agent claude|codex]\n       ccr session attach <session ID>", "用法：ccr session list [--agent claude|codex]\n      ccr session attach <会话 ID>"))
		fs.PrintDefaults()
	}
	words, native, err := parseCLIFlags(fs, args)
	if err != nil {
		return err
	}
	if len(words) == 0 && len(native) == 0 {
		fs.Usage()
		return nil
	}
	list := len(words) == 1 && (words[0] == "ls" || words[0] == "list") && (*agent == "" || *agent == "claude" || *agent == "codex")
	attach := len(words) == 2 && words[0] == "attach" && *agent == ""
	if len(native) > 0 || !list && !attach {
		fs.Usage()
		return errors.New(i18n.T("invalid session command arguments", "会话命令参数无效"))
	}
	api, err := newClient(*host, *dir)
	if err != nil {
		return err
	}
	all, err := api.sessions()
	if err != nil {
		return err
	}
	if list {
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, i18n.T("ID\tAGENT\tMODE\tSTATUS\tPROJECT\tTITLE", "ID\tAGENT\t模式\t状态\t项目\t标题"))
		for _, x := range all {
			if *agent == "" || *agent == x.agent() {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", printable(x.ID), printable(x.agent()), printable(x.Mode), sessionState(x), printable(x.Cwd), printable(x.Title))
			}
		}
		return w.Flush()
	}
	x, err := attachedSession(all, words[1])
	if err != nil {
		return err
	}
	if err := requireAgentTerminal(); err != nil {
		return err
	}
	if !confirmAttached(x) {
		return nil
	}
	return api.attach(x)
}

func attachedSession(all []remoteSession, id string) (remoteSession, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	var target remoteSession
	matches := 0
	for _, x := range all {
		if id == "" || !strings.HasPrefix(strings.ToLower(x.ID), id) {
			continue
		}
		if strings.EqualFold(x.ID, id) {
			target = x
			matches = 1
			break
		}
		matches++
		target = x
	}
	switch {
	case matches > 1:
		return remoteSession{}, i18n.Errorf("session ID prefix %q is ambiguous", "会话 ID 前缀 %q 不唯一", id)
	case target.ID == "":
		return remoteSession{}, i18n.Errorf("session ID %q was not found", "找不到会话 ID %q", id)
	case target.Mode != "terminal":
		return remoteSession{}, i18n.Errorf("session %s is managed; open it in CC Remote", "会话 %s 是托管会话，请在 CC Remote 中打开", target.ID)
	case !target.Online:
		hint := "ccr " + target.agent() + " resume"
		if target.NativeID != "" {
			hint += " " + target.NativeID
		}
		return remoteSession{}, i18n.Errorf("session %s is offline; use %s to resume saved history", "会话 %s 已离线，可使用 %s 恢复已保存的历史", target.ID, hint)
	}
	return target, nil
}

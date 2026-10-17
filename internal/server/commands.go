package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"

	"cc-remote/internal/i18n"
	"cc-remote/internal/prefs"
	"cc-remote/internal/store"
)

type webCommand struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	TerminalOnly bool   `json:"terminalOnly,omitempty"`
}

func reservedCommand(name string) bool {
	for _, command := range commandList(i18n.EN, false) {
		if command.Name == name {
			return true
		}
	}
	return false
}

// commandList is built per request so the descriptions follow the page's language.
func commandList(l i18n.Lang, channel bool) []webCommand {
	return []webCommand{
		{"help", l.T("Show the commands the web supports", "显示网页支持的命令"), false},
		{"status", l.T("Show the current session's status", "查看当前会话状态"), false},
		{"model", l.T("List available models; /model opus sets the current managed session, /model --default opus sets the CC Remote default; inherit restores the default", "列出可用模型；/model opus 设置当前托管会话，/model --default opus 设置 CC Remote 默认；inherit 恢复默认"), channel},
		{"effort", l.T("Show the reasoning effort; /effort high sets the current managed session, /effort --default high sets the default", "查看推理强度；/effort high 设置当前托管会话，/effort --default high 设置默认"), channel},
		{"permission-mode", l.T("Set the current managed session's permission mode, e.g. /permission-mode bypassPermissions; inherit restores the default", "设置当前托管会话权限模式，例如 /permission-mode bypassPermissions；inherit 恢复默认"), channel},
		{"config", l.T("Open the settings panel", "打开设置面板"), false},
		{"permissions", l.T("Open the settings panel (permission mode and other defaults)", "打开设置面板（权限模式等默认值）"), false},
		{"clear", l.T("Reset the managed session's context and keep the web history; shared sessions must run it in the terminal", "重置托管会话上下文，保留网页历史；共享会话需在终端执行"), channel},
		{"new", l.T("Same as /clear", "同 /clear"), channel},
		{"reset", l.T("Same as /clear", "同 /clear"), channel},
		{"stop", l.T("Stop the current managed turn; shared sessions must run it in the terminal", "停止当前托管轮次；共享会话需在终端执行"), channel},
		{"compact", l.T("Compact the context, optionally with what to keep, e.g. /compact keep the API design; shared sessions must run it in the terminal", "压缩上下文，可附加保留重点，例如 /compact 保留接口设计；共享会话需在终端执行"), channel},
		{"resume", l.T("Switch native sessions; run it in the terminal or pick another web tab", "切换原生会话，请在终端执行或选择其它网页标签"), true},
	}
}

// claudeCommands returns the slash commands Claude Code listed in its latest
// init event for the session, else for any session (built-ins, plugins and
// user skills are shared). known is false until Claude has run once.
func (s *Server) claudeCommands(id string) (names []string, known bool) {
	for _, sid := range []string{id, ""} {
		raw, ok, err := s.store.LatestPayload(sid, "session", `"slash_commands"`)
		if err != nil || !ok {
			continue
		}
		var v struct {
			SlashCommands []string `json:"slash_commands"`
		}
		if json.Unmarshal(raw, &v) == nil && v.SlashCommands != nil {
			return v.SlashCommands, true
		}
	}
	return nil, false
}

// nativeCommand reports whether /name is sent to Claude verbatim: in managed
// sessions every command Claude reports (built-ins, plugins, skills) runs
// natively in print mode. Before Claude has ever run nothing is known, so the
// command is passed through and Claude answers it.
func (s *Server) nativeCommand(x store.Session, name string) bool {
	if x.Mode == "channel" || reservedCommand(name) || !skillName.MatchString(name) {
		return false
	}
	names, known := s.claudeCommands(x.ID)
	if !known || slices.Contains(names, name) {
		return true
	}
	_, found := s.discoveredSkills(x.Cwd)[name] // project skills of this cwd
	return found
}

// terminalCommands are offered in a terminal session's menu; Claude Code's
// terminal runs any other command typed too.
func terminalCommands(l i18n.Lang) []webCommand {
	return []webCommand{
		{"help", l.T("Show the commands the web supports", "显示网页支持的命令"), false},
		{"status", l.T("Show the current session's status", "查看当前会话状态"), false},
		{"stop", l.T("Stop the current turn (Esc); stopping again acts as Ctrl+C", "停止当前轮次（Esc）；再按一次相当于 Ctrl+C"), false},
		{"compact", l.T("Compact the context, optionally with what to keep", "压缩上下文，可附加保留重点"), false},
		{"clear", l.T("Start a new context (the web keeps the earlier messages)", "开始新的上下文（网页保留之前的记录）"), false},
		{"model", l.T("Switch models, e.g. /model opus", "切换模型，例如 /model opus"), false},
		{"effort", l.T("Set the reasoning effort, e.g. /effort high", "设置推理强度，例如 /effort high"), false},
		{"context", l.T("Show context usage", "查看上下文用量"), false},
		{"usage", l.T("Show usage", "查看用量"), false},
		{"agents", l.T("Manage subagents", "管理子 agent"), false},
		{"config", l.T("Open the settings panel", "打开设置面板"), false},
	}
}

// commandMenu lists the web commands plus, for managed sessions, Claude's own.
func (s *Server) commandMenu(l i18n.Lang, x store.Session) []webCommand {
	if x.Mode == "terminal" {
		return append(terminalCommands(l), s.skillCommands(l, x.Cwd)...)
	}
	menu := commandList(l, x.Mode == "channel")
	names, known := s.claudeCommands(x.ID)
	if x.Mode == "channel" || !known {
		return append(menu, s.skillCommands(l, x.Cwd)...)
	}
	skills := s.discoveredSkills(x.Cwd)
	seen := map[string]bool{}
	var native []webCommand
	add := func(name string) {
		if seen[name] || reservedCommand(name) || !skillName.MatchString(name) {
			return
		}
		seen[name] = true
		description := l.T("Claude Code command", "Claude Code 命令")
		if f, ok := skills[name]; ok {
			description = skillDescription(l, f)
		}
		native = append(native, webCommand{Name: name, Description: description})
	}
	for _, name := range names {
		add(name)
	}
	for name := range skills {
		add(name)
	}
	sort.Slice(native, func(i, j int) bool { return native[i].Name < native[j].Name })
	return append(menu, native...)
}

func (s *Server) commands(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	l := s.lang(r)
	x, err := s.store.Get(id)
	if err != nil {
		fail(w, 404, l.T("Session not found", "会话不存在"))
		return
	}
	if r.Method == http.MethodGet {
		respond(w, 200, s.commandMenu(l, x))
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	if !decode(w, r, &req) {
		return
	}
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(req.Command), "/"))
	name, args, _ := strings.Cut(text, " ")
	args = strings.TrimSpace(args)
	if len(text) > 1024 {
		fail(w, 400, l.T("The command is too long", "命令过长"))
		return
	}
	if x.Mode == "terminal" {
		switch name {
		case "stop":
			s.mu.Lock()
			s.terminalStopLocked(w, r, x.ID)
			s.mu.Unlock()
			return
		case "help", "status", "config", "permissions":
		default:
			respond(w, 200, map[string]any{"sendMessage": true}) // typed into Claude Code's terminal
			return
		}
	}
	result := map[string]any{}
	switch name {
	case "help":
		result["commands"] = commandList(l, x.Mode == "channel")
		if x.Mode == "terminal" {
			result["commands"] = terminalCommands(l)
			result["message"] = l.T("In a terminal session, every command except /help, /status, /config and /stop is typed into Claude Code's terminal as is (including plugin commands and skills).", "终端会话里，除 /help、/status、/config、/stop 外，所有命令都原样输入 Claude Code 的终端执行（包括插件命令和技能）。")
			break
		}
		result["message"] = l.T("The web supports /status, /model, /effort, /permission-mode, /config, /permissions, /clear (/new, /reset), /compact and /stop; in managed sessions, other commands Claude Code reports (such as /context, /usage, /init, plugin commands and skills) go to Claude as is. /model and /effort apply to the current managed session by default (from the next turn); add --default to change the CC Remote default, and the argument inherit restores the default (default is Claude Code's model alias and is passed through). These settings are passed to Claude as launch flags and do not change ~/.claude/settings.json. Shared sessions run native commands in the terminal.", "网页支持 /status、/model、/effort、/permission-mode、/config、/permissions、/clear（/new、/reset）、/compact、/stop；托管会话中 Claude Code 报告的其它命令（如 /context、/usage、/init 及插件命令和技能）原样交给 Claude 执行。/model 与 /effort 默认作用于当前托管会话（下一轮生效），加 --default 修改 CC Remote 默认，参数 inherit 恢复为默认值（default 是 Claude Code 的模型别名，会原样传递）；这些设置以启动参数传给 Claude，不修改 ~/.claude/settings.json。共享会话的原生命令需在终端执行。")
	case "status":
		s.mu.Lock()
		s.decorateChannel(&x)
		s.mu.Unlock()
		result["session"] = x
		result["message"] = x.Title + " · " + x.Mode + " · " + x.Status
	case "config", "permissions":
		result["openSettings"] = true
	case "model", "effort", "permission-mode":
		message, data, code := s.preferenceCommand(l, x, name, args)
		if code != 200 {
			fail(w, code, message)
			return
		}
		for k, v := range data {
			result[k] = v
		}
		result["message"] = message
	case "stop":
		if x.Mode == "channel" {
			fail(w, 409, l.T("In a shared terminal, press Esc to stop generating; Channels has no stop control", "共享终端请按 Esc 停止生成；Channels 没有停止控制接口"))
			return
		}
		s.stop(w, r)
		return
	case "clear", "new", "reset":
		s.mu.Lock()
		x, err = s.store.Get(id)
		if err != nil {
			s.mu.Unlock()
			fail(w, 404, l.T("Session not found", "会话不存在"))
			return
		}
		if x.Mode == "channel" {
			s.mu.Unlock()
			fail(w, 409, l.F("Run /%s in the native terminal; the old web binding disconnects, then connect the target session", "请在原生终端执行 /%s；旧网页绑定将断开，再连接目标会话", name))
			return
		}
		if _, active := s.runs[id]; active {
			s.mu.Unlock()
			fail(w, 409, l.T("Stop or wait for the current turn to finish first", "请先停止或等待当前轮次结束"))
			return
		}
		err = s.store.ResetContext(id)
		if err == nil {
			_, err = s.store.Append(id, "compact", "", json.RawMessage(`{"clear":true}`))
		}
		s.mu.Unlock()
		if err != nil {
			fail(w, 500, l.T("Could not reset the context", "无法重置上下文"))
			return
		}
		s.mu.Lock()
		rt := s.detachLocked(id) // the idle process belongs to the previous context
		s.mu.Unlock()
		closeRuntime(rt)
		result["message"] = l.T("The context was reset; the web history is kept.", "已重置上下文，网页历史已保留。")
	case "compact":
		if message, code := compactBlocked(l, x); code != 0 {
			fail(w, code, message)
			return
		}
		respond(w, 200, map[string]any{"sendMessage": true})
		return
	case "resume":
		fail(w, 400, l.T("/resume switches sessions in the terminal; on the web, switch from the session list or \"Terminal history\"", "/resume 用于在终端切换会话；网页请从会话列表或「终端历史」切换"))
		return
	default:
		if x.Mode != "channel" {
			if !s.nativeCommand(x, name) {
				fail(w, 400, l.F("Claude Code did not report a /%s command; it may only work in the interactive terminal. Type /help for the web's commands.", "Claude Code 没有报告 /%s 命令，它可能只能在终端交互界面使用。输入 /help 查看网页命令。", name))
				return
			}
			respond(w, 200, map[string]any{"sendMessage": true})
			return
		}
		if _, found, skillErr := s.skillPrompt(l, x.Cwd, name, args); found {
			if skillErr != nil {
				fail(w, 400, skillErr.Error())
				return
			}
			respond(w, 200, map[string]any{"sendMessage": true})
			return
		}
		fail(w, 409, l.F("A shared session is held by the terminal process; run the native command /%s in the terminal. Type /help for the web's commands.", "共享会话由终端进程持有，原生命令 /%s 请在终端执行。输入 /help 查看网页命令。", name))
		return
	}
	respond(w, 200, result)
}

// compactBlocked explains why /compact cannot run as a managed turn, or returns
// code 0. Claude Code accepts /compact in print mode on a resumed session; a
// shared terminal owns its process, so the command has to be typed there.
func compactBlocked(l i18n.Lang, x store.Session) (string, int) {
	if x.Mode == "channel" {
		return l.T("A shared session is held by the terminal process; run /compact in the native terminal", "共享会话由终端进程持有，请在原生终端执行 /compact"), 409
	}
	if !x.Resume {
		return l.T("The session has no context to compact yet", "会话还没有可压缩的上下文"), 400
	}
	return "", 0
}

func displayDefault(l i18n.Lang, x string) string {
	if x == "" {
		return l.T("Claude's default", "跟随 Claude 默认值")
	}
	return x
}

// preferenceCommand handles /model, /effort and /permission-mode.
// Without --default they change the managed session override; with it, cc-remote preferences.
func (s *Server) preferenceCommand(l i18n.Lang, x store.Session, name, args string) (string, map[string]any, int) {
	global := false
	fields := strings.Fields(args)
	kept := fields[:0]
	for _, f := range fields {
		if f == "--default" {
			global = true
		} else {
			kept = append(kept, f)
		}
	}
	value := strings.Join(kept, " ")
	data := map[string]any{}
	label := map[string]string{"model": l.T("model", "模型"), "effort": l.T("reasoning effort", "推理强度"), "permission-mode": l.T("permission mode", "权限模式")}[name]
	if name == "permission-mode" && global {
		data["openSettings"] = true
		return l.T("Change the CC Remote default permission mode in the settings panel (dangerous modes need confirming).", "CC Remote 默认权限模式请在设置面板修改（需要确认危险模式）。"), data, 200
	}
	// Model discovery reads files and may call the API, so it runs before taking s.mu.
	var models *modelList
	if value == "" && name == "model" {
		list := s.modelList(l, x.Cwd, false)
		models = &list
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	current, rev, err := prefs.Load(s.cfg.DataDir)
	if err != nil {
		return i18n.Text(l, err), nil, 500
	}
	if value == "" {
		effective := current.Over(sessionOverrides(x))
		pick := func(p prefs.Preferences) string {
			return map[string]string{"model": p.Model, "effort": p.Effort, "permission-mode": p.PermissionMode}[name]
		}
		lines := []string{l.F("Current session %s: %s", "当前会话%s：%s", label, displayDefault(l, pick(effective))), l.F("CC Remote default: %s", "CC Remote 默认：%s", displayDefault(l, pick(current)))}
		if x.Mode == "channel" {
			lines = append(lines, l.F("The shared terminal is held by the terminal process; run /%s in the terminal to switch.", "共享终端由终端进程持有，请在终端执行 /%s 切换。", strings.TrimSuffix(name, "-mode")))
		}
		switch name {
		case "model":
			data["models"] = *models
			ids := make([]string, 0, len(models.Models))
			for _, m := range models.Models {
				ids = append(ids, m.ID)
			}
			lines = append(lines, l.T("Available models: ", "可用模型：")+strings.Join(ids, l.T(", ", "、")), l.T("/model inherit restores the default; default is Claude Code's model alias and is passed to --model as is", "/model inherit 恢复默认；default 是 Claude Code 的模型别名，会原样传给 --model"))
		case "effort":
			lines = append(lines, l.T("Options: low, medium, high, xhigh, max; /effort inherit restores the default", "可选：low、medium、high、xhigh、max；/effort inherit 恢复默认"))
		default:
			lines = append(lines, l.T("Options: default, plan, acceptEdits, dontAsk, auto, bypassPermissions; inherit restores the default", "可选：default、plan、acceptEdits、dontAsk、auto、bypassPermissions；inherit 恢复默认"))
		}
		data["preferences"] = preferencesBody{Preferences: current, Revision: rev}
		return strings.Join(lines, "\n"), data, 200
	}
	// inherit clears the override for every setting; "default" is a real value
	// (a model alias, or the default permission mode) and is passed through.
	if value == "inherit" || value == "跟随" {
		value = ""
	}
	if global {
		next := current
		if name == "model" {
			next.Model = value
		} else {
			next.Effort = value
		}
		saved, err := s.savePreferences(l, next, rev, true)
		if err != nil {
			code := 400
			if errors.Is(err, prefs.ErrConflict) {
				code = 409
			}
			return i18n.Text(l, err), nil, code
		}
		data["preferences"] = saved
		return l.F("Saved the CC Remote default %s: %s. Managed sessions use it from the next turn; newly started shared terminals use this default; Claude's configuration is not changed.", "已保存 CC Remote 默认%s：%s。托管会话下一轮生效；新启动的共享终端使用该默认值，不修改 Claude 配置。", label, displayDefault(l, value)), data, 200
	}
	if x.Mode == "channel" {
		return channelOverrideMessage(l), nil, 409
	}
	fresh, err := s.store.Get(x.ID)
	if err != nil {
		return l.T("Session not found", "会话不存在"), nil, 404
	}
	req := overrideRequest{ConfirmDangerous: true} // typing the mode explicitly is the confirmation
	switch name {
	case "model":
		req.Model = &value
	case "effort":
		req.Effort = &value
	default:
		req.PermissionMode = &value
	}
	if err = req.apply(l, &fresh); err != nil {
		return i18n.Text(l, err), nil, 400
	}
	if err = s.store.SetOverrides(fresh.ID, fresh.PermissionMode, fresh.Model, fresh.Effort); err != nil {
		return l.T("Could not save session settings", "无法保存会话设置"), nil, 500
	}
	data["session"] = fresh
	message := l.F("The current session's %s is now %s, from the next turn.", "当前会话%s已设为：%s，下一轮生效。", label, displayDefault(l, value))
	if value == "" {
		message = l.F("The current session's %s is back to the CC Remote default (%s), from the next turn.", "当前会话%s已恢复为 CC Remote 默认（%s），下一轮生效。", label, displayDefault(l, map[string]string{"model": current.Model, "effort": current.Effort, "permission-mode": current.PermissionMode}[name]))
	}
	if name == "permission-mode" && prefs.Dangerous(value) {
		message += l.T(" Note: this mode skips or automatically handles tool approvals.", " 注意：此模式会跳过或自动处理工具审批。")
	}
	return message, data, 200
}

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"

	"cc-remote/internal/prefs"
	"cc-remote/internal/store"
)

type webCommand struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	TerminalOnly bool   `json:"terminalOnly,omitempty"`
}

func reservedCommand(name string) bool {
	for _, command := range commandList(false) {
		if command.Name == name {
			return true
		}
	}
	return false
}

func commandList(channel bool) []webCommand {
	return []webCommand{
		{"help", "显示网页支持的命令", false},
		{"status", "查看当前会话状态", false},
		{"model", "列出可用模型；/model opus 设置当前托管会话，/model --default opus 设置 CC Remote 默认；inherit 恢复默认", channel},
		{"effort", "查看推理强度；/effort high 设置当前托管会话，/effort --default high 设置默认", channel},
		{"permission-mode", "设置当前托管会话权限模式，例如 /permission-mode bypassPermissions；inherit 恢复默认", channel},
		{"config", "打开设置面板", false},
		{"permissions", "打开设置面板（权限模式等默认值）", false},
		{"clear", "重置托管会话上下文，保留网页历史；共享会话需在终端执行", channel},
		{"new", "同 /clear", channel},
		{"reset", "同 /clear", channel},
		{"stop", "停止当前托管轮次；共享会话需在终端执行", channel},
		{"compact", "压缩上下文，可附加保留重点，例如 /compact 保留接口设计；共享会话需在终端执行", channel},
		{"resume", "切换原生会话，请在终端执行或选择其它网页标签", true},
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

// commandMenu lists the web commands plus, for managed sessions, Claude's own.
func (s *Server) commandMenu(x store.Session) []webCommand {
	menu := commandList(x.Mode == "channel")
	names, known := s.claudeCommands(x.ID)
	if x.Mode == "channel" || !known {
		return append(menu, s.skillCommands(x.Cwd)...)
	}
	skills := s.discoveredSkills(x.Cwd)
	seen := map[string]bool{}
	var native []webCommand
	add := func(name string) {
		if seen[name] || reservedCommand(name) || !skillName.MatchString(name) {
			return
		}
		seen[name] = true
		description := "Claude Code 命令"
		if f, ok := skills[name]; ok && f.description != "" {
			description = f.description
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
	x, err := s.store.Get(id)
	if err != nil {
		fail(w, 404, "会话不存在")
		return
	}
	if r.Method == http.MethodGet {
		respond(w, 200, s.commandMenu(x))
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
		fail(w, 400, "命令过长")
		return
	}
	result := map[string]any{}
	switch name {
	case "help":
		result["commands"] = commandList(x.Mode == "channel")
		result["message"] = "网页支持 /status、/model、/effort、/permission-mode、/config、/permissions、/clear（/new、/reset）、/compact、/stop；托管会话中 Claude Code 报告的其它命令（如 /context、/usage、/init 及插件命令和技能）原样交给 Claude 执行。/model 与 /effort 默认作用于当前托管会话（下一轮生效），加 --default 修改 CC Remote 默认，参数 inherit 恢复为默认值（default 是 Claude Code 的模型别名，会原样传递）；这些设置以启动参数传给 Claude，不修改 ~/.claude/settings.json。共享会话的原生命令需在终端执行。"
	case "status":
		s.mu.Lock()
		s.decorateChannel(&x)
		s.mu.Unlock()
		result["session"] = x
		result["message"] = x.Title + " · " + x.Mode + " · " + x.Status
	case "config", "permissions":
		result["openSettings"] = true
	case "model", "effort", "permission-mode":
		message, data, code := s.preferenceCommand(x, name, args)
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
			fail(w, 409, "共享终端请按 Esc 停止生成；Channels 没有停止控制接口")
			return
		}
		s.stop(w, r)
		return
	case "clear", "new", "reset":
		s.mu.Lock()
		x, err = s.store.Get(id)
		if err != nil {
			s.mu.Unlock()
			fail(w, 404, "会话不存在")
			return
		}
		if x.Mode == "channel" {
			s.mu.Unlock()
			fail(w, 409, "请在原生终端执行 /"+name+"；旧网页绑定将断开，再连接目标会话")
			return
		}
		if _, active := s.runs[id]; active {
			s.mu.Unlock()
			fail(w, 409, "请先停止或等待当前轮次结束")
			return
		}
		err = s.store.ResetContext(id)
		if err == nil {
			_, err = s.store.Append(id, "assistant", "已开始新的 Claude 上下文；之前的网页消息仍保留供查看。", nil)
		}
		s.mu.Unlock()
		if err != nil {
			fail(w, 500, "无法重置上下文")
			return
		}
		result["message"] = "已重置上下文，网页历史已保留。"
	case "compact":
		if message, code := compactBlocked(x); code != 0 {
			fail(w, code, message)
			return
		}
		respond(w, 200, map[string]any{"sendMessage": true})
		return
	case "resume":
		fail(w, 400, "/resume 用于在终端切换会话；网页请从会话列表或「终端历史」切换")
		return
	default:
		if x.Mode != "channel" {
			if !s.nativeCommand(x, name) {
				fail(w, 400, "Claude Code 没有报告 /"+name+" 命令，它可能只能在终端交互界面使用。输入 /help 查看网页命令。")
				return
			}
			respond(w, 200, map[string]any{"sendMessage": true})
			return
		}
		if _, found, skillErr := s.skillPrompt(x.Cwd, name, args); found {
			if skillErr != nil {
				fail(w, 400, skillErr.Error())
				return
			}
			respond(w, 200, map[string]any{"sendMessage": true})
			return
		}
		fail(w, 409, "共享会话由终端进程持有，原生命令 /"+name+" 请在终端执行。输入 /help 查看网页命令。")
		return
	}
	respond(w, 200, result)
}

// compactBlocked explains why /compact cannot run as a managed turn, or returns
// code 0. Claude Code accepts /compact in print mode on a resumed session; a
// shared terminal owns its process, so the command has to be typed there.
func compactBlocked(x store.Session) (string, int) {
	if x.Mode == "channel" {
		return "共享会话由终端进程持有，请在原生终端执行 /compact", 409
	}
	if !x.Resume {
		return "会话还没有可压缩的上下文", 400
	}
	return "", 0
}

func displayDefault(x string) string {
	if x == "" {
		return "跟随 Claude 默认值"
	}
	return x
}

// preferenceCommand handles /model, /effort and /permission-mode.
// Without --default they change the managed session override; with it, cc-remote preferences.
func (s *Server) preferenceCommand(x store.Session, name, args string) (string, map[string]any, int) {
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
	label := map[string]string{"model": "模型", "effort": "推理强度", "permission-mode": "权限模式"}[name]
	if name == "permission-mode" && global {
		data["openSettings"] = true
		return "CC Remote 默认权限模式请在设置面板修改（需要确认危险模式）。", data, 200
	}
	// Model discovery reads files and may call the API, so it runs before taking s.mu.
	var models *modelList
	if value == "" && name == "model" {
		list := s.modelList(x.Cwd, false)
		models = &list
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	current, rev, err := prefs.Load(s.cfg.DataDir)
	if err != nil {
		return err.Error(), nil, 500
	}
	if value == "" {
		effective := current.Over(sessionOverrides(x))
		pick := func(p prefs.Preferences) string {
			return map[string]string{"model": p.Model, "effort": p.Effort, "permission-mode": p.PermissionMode}[name]
		}
		lines := []string{"当前会话" + label + "：" + displayDefault(pick(effective)), "CC Remote 默认：" + displayDefault(pick(current))}
		if x.Mode == "channel" {
			lines = append(lines, "共享终端由终端进程持有，请在终端执行 /"+strings.TrimSuffix(name, "-mode")+" 切换。")
		}
		switch name {
		case "model":
			data["models"] = *models
			ids := make([]string, 0, len(models.Models))
			for _, m := range models.Models {
				ids = append(ids, m.ID)
			}
			lines = append(lines, "可用模型："+strings.Join(ids, "、"), "/model inherit 恢复默认；default 是 Claude Code 的模型别名，会原样传给 --model")
		case "effort":
			lines = append(lines, "可选：low、medium、high、xhigh、max；/effort inherit 恢复默认")
		default:
			lines = append(lines, "可选：default、plan、acceptEdits、dontAsk、auto、bypassPermissions；inherit 恢复默认")
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
		saved, err := s.savePreferences(next, rev, true)
		if err != nil {
			code := 400
			if errors.Is(err, prefs.ErrConflict) {
				code = 409
			}
			return err.Error(), nil, code
		}
		data["preferences"] = saved
		return "已保存 CC Remote 默认" + label + "：" + displayDefault(value) + "。托管会话下一轮生效；新启动的共享终端使用该默认值，不修改 Claude 配置。", data, 200
	}
	if x.Mode == "channel" {
		return channelOverrideMessage, nil, 409
	}
	fresh, err := s.store.Get(x.ID)
	if err != nil {
		return "会话不存在", nil, 404
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
	if err = req.apply(&fresh); err != nil {
		return err.Error(), nil, 400
	}
	if err = s.store.SetOverrides(fresh.ID, fresh.PermissionMode, fresh.Model, fresh.Effort); err != nil {
		return "无法保存会话设置", nil, 500
	}
	data["session"] = fresh
	message := "当前会话" + label + "已设为：" + displayDefault(value) + "，下一轮生效。"
	if value == "" {
		message = "当前会话" + label + "已恢复为 CC Remote 默认（" + displayDefault(map[string]string{"model": current.Model, "effort": current.Effort, "permission-mode": current.PermissionMode}[name]) + "），下一轮生效。"
	}
	if name == "permission-mode" && prefs.Dangerous(value) {
		message += " 注意：此模式会跳过或自动处理工具审批。"
	}
	return message, data, 200
}

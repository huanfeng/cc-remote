package server

import (
	"errors"
	"net/http"
	"strings"
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
		{"model", "查看或修改新会话的默认模型，例如 /model sonnet", false},
		{"effort", "查看或修改默认推理强度，例如 /effort high", false},
		{"config", "打开 Claude 设置", false},
		{"permissions", "打开默认权限设置", false},
		{"clear", "重置托管会话上下文，保留网页历史；共享会话需在终端执行", channel},
		{"stop", "停止当前托管轮次；共享会话需在终端执行", channel},
		{"compact", "压缩上下文，请在原生终端执行", true},
		{"resume", "切换原生会话，请在终端执行或选择其它网页标签", true},
		{"mcp", "管理原生 MCP 服务，请在终端执行", true},
	}
}

func (s *Server) commands(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	x, err := s.store.Get(id)
	if err != nil {
		fail(w, 404, "会话不存在")
		return
	}
	if r.Method == http.MethodGet {
		respond(w, 200, append(commandList(x.Mode == "channel"), s.skillCommands(x.Cwd)...))
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
		result["message"] = "网页支持 /status、/model、/effort、/config、/permissions、/clear、/stop。其它 Claude 内置控制命令需在原生终端执行。"
	case "status":
		s.mu.Lock()
		s.decorateChannel(&x)
		s.mu.Unlock()
		result["session"] = x
		result["message"] = x.Title + " · " + x.Mode + " · " + x.Status
	case "config", "permissions":
		result["openSettings"] = true
	case "model", "effort":
		s.mu.Lock()
		v, b, readErr := s.readSettings()
		if readErr != nil {
			s.mu.Unlock()
			fail(w, 500, readErr.Error())
			return
		}
		prefs := s.settingsSnapshot(v, b)
		if args != "" {
			value := args
			if value == "default" {
				value = ""
			}
			if name == "model" {
				prefs.Model = value
			} else {
				prefs.Effort = value
			}
			prefs, err = s.saveSettings(prefs)
		}
		s.mu.Unlock()
		if err != nil {
			code := 400
			if errors.Is(err, errSettingsConflict) {
				code = 409
			}
			fail(w, code, err.Error())
			return
		}
		result["settings"] = prefs
		if args == "" {
			if name == "model" {
				result["message"] = "默认模型：" + displayDefault(prefs.Model)
			} else {
				result["message"] = "默认推理强度：" + displayDefault(prefs.Effort)
			}
		} else {
			result["message"] = "已保存 Claude 默认设置。托管会话下一轮读取；正在运行的共享终端需重启，或在终端用 /" + name + " 即时修改。项目及组织设置仍可能覆盖此默认值。"
		}
	case "stop":
		if x.Mode == "channel" {
			fail(w, 409, "共享终端请按 Esc 停止生成；Channels 没有停止控制接口")
			return
		}
		s.stop(w, r)
		return
	case "clear":
		s.mu.Lock()
		x, err = s.store.Get(id)
		if err != nil {
			s.mu.Unlock()
			fail(w, 404, "会话不存在")
			return
		}
		if x.Mode == "channel" {
			s.mu.Unlock()
			fail(w, 409, "请在原生终端执行 /clear；旧网页绑定将断开，再连接目标会话")
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
	case "compact", "resume", "mcp":
		fail(w, 400, "此原生控制命令请在 Claude Code 终端执行")
		return
	default:
		if _, found, skillErr := s.skillPrompt(x.Cwd, name, args); found {
			if skillErr != nil {
				fail(w, 400, skillErr.Error())
				return
			}
			respond(w, 200, map[string]any{"sendMessage": true})
			return
		}
		fail(w, 400, "此命令尚无网页控制接口，请在原生 Claude Code 终端执行。输入 /help 查看网页命令。")
		return
	}
	respond(w, 200, result)
}

func displayDefault(x string) string {
	if x == "" {
		return "跟随 Claude 默认值"
	}
	return x
}

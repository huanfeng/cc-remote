package server

import (
	"net/http"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
	"cc-remote/internal/i18n"
)

func providerName(provider string) string {
	if provider == "codex" {
		return "Codex"
	}
	return "Claude Code"
}

func codexCommandList(l i18n.Lang) []webCommand {
	menu := commandList(l)
	for i := range menu {
		switch menu[i].Name {
		case "model":
			menu[i].Description = l.T("Choose a Codex model; inherit restores Codex defaults", "选择 Codex 模型；inherit 恢复 Codex 默认值")
		case "permission-mode":
			menu[i].Description = l.T("Set the Codex sandbox: read-only, workspace-write or danger-full-access", "设置 Codex 沙箱：read-only、workspace-write 或 danger-full-access")
		case "compact":
			menu[i].Description = l.T("Compact the Codex thread context", "压缩 Codex 线程上下文")
		}
	}
	return menu
}

func requestProvider(w http.ResponseWriter, r *http.Request) (string, bool) {
	p := r.URL.Query().Get("provider")
	if p == "" {
		p = "claude"
	}
	if p != "claude" && p != "codex" {
		fail(w, http.StatusBadRequest, "Invalid provider")
		return "", false
	}
	return p, true
}

func (s *Server) nativeHistory(r *http.Request, id string) (claude.NativeSession, []claude.Event, error) {
	if r.URL.Query().Get("provider") == "codex" {
		return codex.NativeHistory(s.cfg.CodexConfigDir, id)
	}
	return claude.NativeHistory(s.cfg.ClaudeConfigDir, id)
}

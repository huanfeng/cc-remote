package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"cc-remote/internal/store"
	"cc-remote/internal/terminal"
)

// A menu is represented by one tool request for its entire visible lifetime.
// Ignore brief redraw gaps, and do not recreate a submitted request while the
// terminal is still painting the same menu.
func (s *Server) syncUsageMenuLocked(ts *terminalSession, menu *terminal.UsageLimitMenu, now time.Time) {
	if menu == nil {
		if ts.usageKey == "" {
			return
		}
		if ts.usageGone.IsZero() {
			ts.usageGone = now
			return
		}
		if now.Sub(ts.usageGone) < dialogGrace {
			return
		}
		if p := s.requests[ts.usageID]; p != nil {
			s.finishSharedLocked(ts.usageID, p, "terminal")
		}
		ts.usageKey, ts.usageID, ts.usageGone = "", "", time.Time{}
		return
	}
	ts.usageGone = time.Time{}
	raw, _ := json.Marshal(menu)
	key := string(raw)
	if ts.usageKey == key {
		return
	}
	if p := s.requests[ts.usageID]; p != nil {
		s.finishSharedLocked(ts.usageID, p, "expired")
	}
	id := store.ID()
	p := &pending{sessionID: ts.id, ctx: context.Background(), channel: make(chan answer, 1), connection: ts.conn, toolName: "UsageLimit", input: raw, noBackstop: true, usageMenu: menu}
	if err := s.store.AddInteraction(store.Interaction{ID: id, SessionID: ts.id, Kind: "question", ToolName: p.toolName, Input: raw}); err != nil {
		return
	}
	tool, _ := json.Marshal(map[string]any{"id": id, "name": p.toolName, "input": menu})
	if _, err := s.store.Append(ts.id, "tool", "", tool); err != nil {
		_ = s.store.Resolve(id, "expired")
		return
	}
	ts.usageKey, ts.usageID = key, id
	s.requests[id] = p
}

func (s *Server) usageResultLocked(id string, p *pending, outcome, option string) {
	content, _ := json.Marshal(map[string]string{"outcome": outcome, "option": option})
	raw, _ := json.Marshal(map[string]any{"tool_use_id": id, "content": string(content), "is_error": outcome == "expired"})
	_, _ = s.store.Append(p.sessionID, "tool_result", "", raw)
}

func (s *Server) resolveUsageLimitLocked(w http.ResponseWriter, r *http.Request, id string, p *pending, a answer) {
	// This is a fixed CLI choice, not an AskUserQuestion free-form answer.
	option := a.Answers["option"]
	valid := false
	for _, o := range p.usageMenu.Options {
		if option == o.ID {
			valid = true
		}
	}
	if a.Decision != "allow" || !valid || len(a.Answers) != 1 || a.Mode != "" || a.Message != "" {
		fail(w, 400, s.lang(r).T("Select one of the usage limit options", "请选择一个用量处理选项"))
		return
	}
	ts := s.terminals[p.sessionID]
	if ts == nil || ts.conn != p.connection {
		s.finishSharedLocked(id, p, "expired")
		fail(w, 409, s.lang(r).T("The usage limit menu has closed", "用量处理菜单已关闭"))
		return
	}
	// Validate and send under the runner's screen lock so an old card cannot
	// send a choice to a newer dialog. Arrow movement starts at the live selection.
	if err := ts.runner.ChooseUsageLimit(p.usageMenu, option); err != nil {
		s.finishSharedLocked(id, p, "expired")
		fail(w, 409, s.lang(r).T("The usage limit menu changed; refresh and try again", "用量处理菜单已变化，请刷新后重试"))
		return
	}
	if err := s.store.Resolve(id, "resolved"); err != nil {
		// Input was already delivered. Never permit a retry to send it twice.
		delete(s.requests, id)
		fail(w, 500, err.Error())
		return
	}
	delete(s.requests, id)
	if option == "2" {
		// Claude stays alive waiting for the reset. Its next tool or Stop hook
		// resumes the usual lifecycle; queued prompts must not interrupt it.
		_ = s.store.SetStatus(p.sessionID, "waiting")
	}
	s.interactionEventLocked(id, p, "web", a)
	s.usageResultLocked(id, p, "web", option)
	respond(w, 200, map[string]string{"status": "resolved"})
}

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"cc-remote/internal/store"
	"cc-remote/internal/terminal"
)

func (s *Server) syncResumeMenuLocked(ts *terminalSession, menu *terminal.ResumeMenu, now time.Time) {
	if menu == nil {
		if ts.resumeKey == "" {
			return
		}
		if ts.resumeGone.IsZero() {
			ts.resumeGone = now
			return
		}
		if now.Sub(ts.resumeGone) < dialogGrace {
			return
		}
		if p := s.requests[ts.resumeID]; p != nil {
			s.finishSharedLocked(ts.resumeID, p, "terminal")
		}
		ts.resumeKey, ts.resumeID, ts.resumeGone = "", "", time.Time{}
		return
	}
	ts.resumeGone = time.Time{}
	raw, _ := json.Marshal(menu)
	key := string(raw)
	if ts.resumeKey == key {
		return
	}
	if p := s.requests[ts.resumeID]; p != nil {
		s.finishSharedLocked(ts.resumeID, p, "expired")
	}
	id := store.ID()
	p := &pending{sessionID: ts.id, ctx: context.Background(), channel: make(chan answer, 1), connection: ts.conn, toolName: "ResumeConversation", input: raw, noBackstop: true, resumeMenu: menu}
	if err := s.store.AddInteraction(store.Interaction{ID: id, SessionID: ts.id, Kind: "question", ToolName: p.toolName, Input: raw}); err != nil {
		return
	}
	tool, _ := json.Marshal(map[string]any{"id": id, "name": p.toolName, "input": menu})
	if _, err := s.store.Append(ts.id, "tool", "", tool); err != nil {
		_ = s.store.Resolve(id, "expired")
		return
	}
	ts.resumeKey, ts.resumeID = key, id
	s.requests[id] = p
}

func (s *Server) resolveResumeMenuLocked(w http.ResponseWriter, r *http.Request, id string, p *pending, a answer) {
	option := a.Answers["option"]
	valid := false
	for _, o := range p.resumeMenu.Options {
		if o.ID == option {
			valid = true
		}
	}
	if a.Decision != "allow" || !valid || len(a.Answers) != 1 || a.Mode != "" || a.Message != "" {
		fail(w, 400, s.lang(r).T("Select a resume option", "请选择恢复对话的处理方式"))
		return
	}
	ts := s.terminals[p.sessionID]
	if ts == nil || ts.conn != p.connection {
		s.finishSharedLocked(id, p, "expired")
		fail(w, 409, s.lang(r).T("The resume menu has closed", "恢复对话菜单已关闭"))
		return
	}
	if err := ts.runner.ChooseResumeMenu(p.resumeMenu, option); err != nil {
		s.finishSharedLocked(id, p, "expired")
		fail(w, 409, s.lang(r).T("The resume menu changed; refresh and try again", "恢复对话菜单已变化，请刷新后重试"))
		return
	}
	if err := s.store.Resolve(id, "resolved"); err != nil {
		delete(s.requests, id)
		fail(w, 500, err.Error())
		return
	}
	delete(s.requests, id)
	s.interactionEventLocked(id, p, "web", a)
	s.usageResultLocked(id, p, "web", option)
	respond(w, 200, map[string]string{"status": "resolved"})
}

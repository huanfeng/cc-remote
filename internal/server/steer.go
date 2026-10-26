package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"cc-remote/internal/codex"
)

func steerFailure(w http.ResponseWriter, status int, delivery, message string) {
	respond(w, status, map[string]string{"error": message, "delivery": delivery})
}

// Confirmation is read-only: an expired or lost receipt must never deliver the
// instruction into a later turn, including after a host restart.
func (s *Server) steerStatus(w http.ResponseWriter, r *http.Request) {
	if !validSendID.MatchString(r.PathValue("sendId")) {
		fail(w, 400, "Invalid send ID")
		return
	}
	s.mu.Lock()
	status, ok := s.sent.seen(r.PathValue("id"), r.PathValue("sendId"))
	s.mu.Unlock()
	if ok && status == "steered" {
		respond(w, 202, map[string]any{"status": status, "duplicate": true})
		return
	}
	delivery := "unknown"
	if ok && status == "steering" {
		delivery = "pending"
	}
	steerFailure(w, 409, delivery, s.lang(r).T("Delivery is pending or unknown; this query will not resend the instruction", "送达状态尚未确认或记录已失效；查询不会重新发送指令"))
}

// steer is deliberately separate from queued messages. Rejection never starts a
// new turn. A required send ID also makes a lost HTTP reply safe to query again.
func (s *Server) steer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text   string `json:"text"`
		SendID string `json:"sendId"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" || len(req.Text) > 1<<20 || strings.HasPrefix(req.Text, "/") || !validSendID.MatchString(req.SendID) {
		fail(w, 400, s.lang(r).T("Additional instructions require ordinary text and a valid send ID", "追加指令需要普通文本和有效发送 ID"))
		return
	}
	id := r.PathValue("id")
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		fail(w, 503, s.lang(r).T("The host is shutting down", "宿主正在关闭"))
		return
	}
	if status, ok := s.sent.seen(id, req.SendID); ok {
		s.mu.Unlock()
		if status == "steered" {
			respond(w, 202, map[string]any{"status": status, "duplicate": true})
			return
		}
		delivery := "unknown"
		if status == "steering" {
			delivery = "pending"
		}
		steerFailure(w, 409, delivery, s.lang(r).T("This instruction is being delivered or its delivery is unknown; it will not be sent again", "该指令正在发送或送达状态不确定，不会重复发送"))
		return
	}
	x, err := s.store.Get(id)
	if err != nil {
		s.mu.Unlock()
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	rt := s.procs[id]
	if x.Provider != "codex" || x.Mode == "terminal" || rt == nil || rt.proc == nil || rt.stopping || !rt.turn || rt.run == nil || rt.interrupting || rt.steering || rt.approvals > 0 || len(rt.sent) > 0 {
		s.mu.Unlock()
		steerFailure(w, 409, "rejected", s.lang(r).T("Additional instructions require an active Codex turn without pending messages or approvals", "追加指令需要正在执行的 Codex 轮次，且没有排队消息或待处理审批"))
		return
	}
	proc, ok := rt.proc.(interface{ Steer(string) error })
	if !ok {
		s.mu.Unlock()
		steerFailure(w, 409, "rejected", s.lang(r).T("This process does not support additional instructions", "此进程不支持追加指令"))
		return
	}
	rt.steering = true
	s.sent.remember(id, req.SendID, "steering")
	s.mu.Unlock()
	// Never hold s.mu across an RPC: its notification callbacks need that mutex.
	err = proc.Steer(req.Text)
	s.mu.Lock()
	defer s.mu.Unlock()
	rt.steering = false
	if errors.Is(err, codex.ErrSteerRejected) {
		delete(s.sent, sendKey(id, req.SendID))
		s.settleLocked(rt)
		steerFailure(w, 409, "rejected", s.lang(r).F("Additional instruction was not accepted: %v", "追加指令未被接受：%v", err))
		return
	}
	status := "steered"
	if err != nil {
		status = "steer-unknown"
	}
	s.sent.remember(id, req.SendID, status)
	payload, _ := json.Marshal(map[string]string{"delivery": status})
	_, saveErr := s.store.Append(id, "user", req.Text, payload)
	if err != nil {
		_, _ = s.store.Append(id, "error", s.lang(r).T("The additional instruction's delivery is unknown. Check the conversation before resending.", "追加指令的送达状态不确定，请检查会话后再决定是否重发。"), nil)
	}
	s.settleLocked(rt)
	if err != nil {
		steerFailure(w, 409, "unknown", s.lang(r).T("Delivery is unknown; this send ID will not be retried", "送达状态不确定，此发送 ID 不会重复执行"))
		return
	}
	if saveErr != nil {
		fail(w, 500, s.lang(r).T("The instruction was delivered but could not be saved; retry with the same send ID", "指令已送达但无法保存，请使用相同发送 ID 重试查询"))
		return
	}
	respond(w, 202, map[string]string{"status": status})
}

package server

import (
	"cc-remote/internal/i18n"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"cc-remote/internal/codex"
	"cc-remote/internal/store"
	"cc-remote/internal/terminal"
)

func queueMessage(text string, payload json.RawMessage, sendID string, deferred bool) store.QueuedMessage {
	m := store.QueuedMessage{Text: text, Payload: payload}
	if deferred {
		m.ID = sendID
		if m.ID == "" {
			m.ID = store.ID()
		}
	}
	return m
}

// queuedItemLocked only returns messages still owned by the host. An item
// handed to the CLI cannot be recalled atomically and must never be edited.
func (s *Server) queuedItemLocked(id, messageID string) (*outgoing, *terminalQueued) {
	if rt := s.procs[id]; rt != nil {
		for i := len(rt.sent) - rt.unwritten; i < len(rt.sent); i++ {
			m := &rt.sent[i]
			if m.ID == messageID && m.deferred && m.Delivery == "" {
				return m, nil
			}
		}
	}
	if ts := s.terminals[id]; ts != nil {
		for i := range ts.queued {
			q := &ts.queued[i]
			if q.message.ID == messageID && q.held {
				return nil, q
			}
		}
	}
	return nil, nil
}

func (s *Server) removeQueuedItemLocked(id, messageID string) {
	if rt := s.procs[id]; rt != nil {
		for i := len(rt.sent) - rt.unwritten; i < len(rt.sent); i++ {
			if rt.sent[i].ID == messageID {
				rt.sent = append(rt.sent[:i:i], rt.sent[i+1:]...)
				rt.unwritten--
				s.settleLocked(rt)
				return
			}
		}
	}
	if ts := s.terminals[id]; ts != nil {
		for i, q := range ts.queued {
			if q.message.ID == messageID {
				ts.queued = append(ts.queued[:i:i], ts.queued[i+1:]...)
				return
			}
		}
	}
}

func queueConflict(w http.ResponseWriter, r *http.Request, s *Server) {
	fail(w, 409, s.lang(r).T("This message is already being delivered or consumed; refresh the queue", "此消息已开始投递或处理，请刷新待发送区"))
}

func (s *Server) withdrawQueueMessage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, mid := r.PathValue("id"), r.PathValue("messageId")
	m, q := s.queuedItemLocked(id, mid)
	// Unknown delivery is a dismissible warning, separate from work queued
	// for scheduling. It must never block later normal messages.
	if m == nil && q == nil && r.Method == http.MethodDelete {
		if rt := s.procs[id]; rt != nil {
			for i, warning := range rt.uncertain {
				if warning.ID == mid {
					rt.uncertain = append(rt.uncertain[:i:i], rt.uncertain[i+1:]...)
					respond(w, 200, map[string]any{"message": warning})
					return
				}
			}
		}
	}
	if m == nil && q == nil {
		queueConflict(w, r, s)
		return
	}
	var message store.QueuedMessage
	if m != nil {
		message = m.QueuedMessage
	} else {
		message = q.message
	}
	s.removeQueuedItemLocked(id, mid)
	if rt := s.procs[id]; rt != nil {
		s.writeLocked(rt)
	}
	respond(w, 200, map[string]any{"message": message})
}

// Edit in place so attachments and the ordering among other clients survive.
func (s *Server) updateQueueMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if len(req.Text) > 1<<20 || strings.HasPrefix(req.Text, "/") {
		fail(w, 400, s.lang(r).T("Edit requires ordinary text, at most 1 MiB", "编辑内容需要是普通文本，最多 1 MiB"))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, mid := r.PathValue("id"), r.PathValue("messageId")
	m, q := s.queuedItemLocked(id, mid)
	if m == nil && q == nil {
		queueConflict(w, r, s)
		return
	}
	var message *store.QueuedMessage
	if m != nil {
		message = &m.QueuedMessage
	} else {
		message = &q.message
	}
	// Commands and skills need their original conversion path, not a text edit.
	if strings.HasPrefix(message.Text, "/") {
		fail(w, 409, "Commands cannot be edited in the queue")
		return
	}
	var payload struct {
		Attachments []attachment `json:"attachments"`
	}
	if len(message.Payload) > 0 && json.Unmarshal(message.Payload, &payload) != nil {
		fail(w, 409, "Invalid queued attachment metadata")
		return
	}
	if req.Text == "" && len(payload.Attachments) == 0 {
		fail(w, 400, "Message is empty")
		return
	}
	ids := make([]string, len(payload.Attachments))
	for i, a := range payload.Attachments {
		ids[i] = a.ID
	}
	prompt, nextPayload, err := s.messageAttachments(s.lang(r), id, req.Text, ids)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	message.Text, message.Payload = req.Text, nextPayload
	if m != nil {
		m.prompt = prompt
	} else {
		q.text = terminal.CleanText(prompt)
	}
	respond(w, 200, map[string]any{"message": message})
}

// Deliver one held terminal item. The terminal writer serializes the actual
// paste; once queued to it, that item is no longer editable.
func (s *Server) drainTerminalQueueLocked(ts *terminalSession) {
	for i := range ts.queued {
		q := &ts.queued[i]
		if !q.held {
			return
		} // a preceding paste is still awaiting consumption
		select {
		case ts.send <- q.text:
			q.held = false
			q.message.Editable, q.message.CanSendNow = false, false
		default:
		}
		return
	}
}

func (s *Server) sendQueueMessage(w http.ResponseWriter, r *http.Request) {
	id, mid := r.PathValue("id"), r.PathValue("messageId")
	s.mu.Lock()
	// Queue ID is the operation ID: a lost reply may be queried by repeating
	// this endpoint, without sending into a new turn.
	operation := "queue:" + mid
	if status, ok := s.sent.seen(id, operation); ok {
		s.mu.Unlock()
		if status == "queue-delivered" {
			respond(w, 202, map[string]any{"status": status, "duplicate": true})
			return
		}
		steerFailure(w, 409, "unknown", s.lang(r).T("Delivery is pending or unknown; it will not be repeated", "投递尚未确认，不会重复发送"))
		return
	}
	m, q := s.queuedItemLocked(id, mid)
	if m == nil && q == nil {
		s.mu.Unlock()
		queueConflict(w, r, s)
		return
	}
	if q != nil {
		ts := s.terminals[id]
		if x, err := s.store.Get(id); err != nil || x.Status == "waiting" {
			s.mu.Unlock()
			fail(w, 409, "Answer the open dialog first")
			return
		}
		for _, pending := range s.requests {
			if pending.connection == ts.conn {
				s.mu.Unlock()
				fail(w, 409, "Answer the open question first")
				return
			}
		}
		select {
		case ts.send <- q.text:
			q.held = false
			q.message.Editable, q.message.CanSendNow = false, false
			s.sent.remember(id, operation, "queue-delivered")
			s.mu.Unlock()
			respond(w, 202, map[string]string{"status": "queue-delivered"})
		default:
			s.mu.Unlock()
			fail(w, 409, "The terminal writer is busy")
		}
		return
	}
	rt := s.procs[id]
	if rt.proc == nil || rt.steering || rt.stopping || rt.approvals > 0 || !rt.turn {
		s.mu.Unlock()
		queueConflict(w, r, s)
		return
	}
	if rt.provider != "codex" {
		// Claude's input stream is its native delivery path. Keep the entry
		// until the replay confirms consumption, just as with native typing.
		// Move only the selected item ahead of the host-held tail. The
		// remaining deferred messages retain their relative order.
		start := len(rt.sent) - rt.unwritten
		for i := start; i < len(rt.sent); i++ {
			if rt.sent[i].ID == mid {
				next := rt.sent[i]
				next.deferred = false
				copy(rt.sent[start+1:i+1], rt.sent[start:i])
				rt.sent[start] = next
				break
			}
		}
		s.sent.remember(id, operation, "queue-delivered")
		s.writeLocked(rt)
		s.mu.Unlock()
		respond(w, 202, map[string]string{"status": "queue-delivered"})
		return
	}
	proc, ok := rt.proc.(interface{ Steer(string) error })
	if !ok || len(m.Payload) > 0 || strings.HasPrefix(m.Text, "/") {
		s.mu.Unlock()
		fail(w, 409, "This message cannot be added to the current task")
		return
	}
	text := m.Text
	m.Delivery = "pending"
	rt.steering = true
	s.sent.remember(id, operation, "queue-pending")
	s.mu.Unlock()
	err := proc.Steer(text)
	s.mu.Lock()
	defer s.mu.Unlock()
	rt.steering = false
	if s.procs[id] != rt || rt.stopping {
		// processExit already preserved the input and owns its settlement.
		// An acknowledgement must not record it again or mutate a replacement.
		status := "queue-unknown"
		if err == nil {
			status = "queue-delivered"
		}
		s.sent.remember(id, operation, status)
		s.settleLocked(rt)
		if err == nil {
			respond(w, 202, map[string]string{"status": status})
		} else {
			steerFailure(w, 409, "unknown", s.lang(r).T("The process exited before delivery could be confirmed", "进程已退出，无法确认投递结果"))
		}
		return
	}
	if errors.Is(err, codex.ErrSteerRejected) {
		delete(s.sent, sendKey(id, operation))
		for i := range rt.sent {
			if rt.sent[i].ID == mid {
				rt.sent[i].Delivery = ""
			}
		}
		s.writeLocked(rt)
		steerFailure(w, 409, "rejected", s.lang(r).T("The current task did not accept this instruction", "当前任务未接受此指令"))
		return
	}
	if err != nil {
		for i := range rt.sent {
			if rt.sent[i].ID == mid {
				warning := rt.sent[i].QueuedMessage
				warning.Delivery = "unknown"
				warning.Editable, warning.CanSendNow = false, false
				rt.uncertain = append(rt.uncertain, warning)
				break
			}
		}
		s.removeQueuedItemLocked(id, mid)
		s.sent.remember(id, operation, "queue-unknown")
		_, _ = s.store.Append(id, "error", s.lang(r).T("An additional instruction's delivery was not confirmed; it will not be resent automatically.", "一条追加指令的送达未确认，不会自动重发。"), nil)
		s.writeLocked(rt)
		s.settleLocked(rt)
		steerFailure(w, 409, "unknown", s.lang(r).T("Delivery is unknown; this item will not be resent automatically", "送达状态未知，此消息不会自动重复发送"))
		return
	}
	s.removeQueuedItemLocked(id, mid)
	payload, _ := json.Marshal(map[string]string{"delivery": "steered"})
	_, saveErr := s.store.Append(id, "user", text, payload)
	s.sent.remember(id, operation, "queue-delivered")
	s.writeLocked(rt)
	s.settleLocked(rt)
	if saveErr != nil {
		fail(w, 500, "Instruction delivered but could not be saved; retry the same queue ID")
		return
	}
	respond(w, 202, map[string]string{"status": "queue-delivered"})
}

// Keep attachment metadata when recording an uncertain instruction.
func queueDeliveryPayload(raw json.RawMessage, delivery string) json.RawMessage {
	payload := map[string]any{}
	_ = json.Unmarshal(raw, &payload)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["delivery"] = delivery
	out, _ := json.Marshal(payload)
	return out
}

func (s *Server) preserveQueueWarningsLocked(rt *sessionRuntime) {
	if len(rt.uncertain) == 0 {
		return
	}
	for _, m := range rt.uncertain {
		_, _ = s.store.Append(rt.id, "user", m.Text, queueDeliveryPayload(m.Payload, "unknown"))
	}
	rt.uncertain = nil
	_, _ = s.store.Append(rt.id, "error", i18n.T("An additional instruction's delivery was not confirmed; it will not be resent automatically.", "一条追加指令的送达未确认，不会自动重发。"), nil)
}

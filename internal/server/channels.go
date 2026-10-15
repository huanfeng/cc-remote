package server

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/store"
)

const channelTTL = 45 * time.Second

type channelCommand struct {
	Seq    int64  `json:"seq"`
	Method string `json:"method"`
	Params any    `json:"params"`
}
type channelConnection struct {
	sessionID, key string
	seen           time.Time
	next           int64
	queue          []channelCommand
}
type channelApproval struct {
	connection *channelConnection
	requestID  string
	toolName   string
	created    time.Time
}

var channelUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var permissionID = regexp.MustCompile(`^[a-km-z]{5}$`)

func (s *Server) channelLive(c *channelConnection) bool {
	return c != nil && time.Since(c.seen) < channelTTL
}
func (s *Server) decorateChannel(x *store.Session) {
	if x.Mode != "channel" {
		return
	}
	x.Online = s.channelLive(s.channels[x.ClaudeSessionID])
	if !x.Online {
		x.Status = "offline"
	}
}

// Bridge registration and control are local-only, in addition to bearer authentication.
func (s *Server) channelLocal(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		fail(w, 403, "会话桥接仅允许本机连接")
		return false
	}
	return true
}
func (s *Server) channelRegister(w http.ResponseWriter, r *http.Request) {
	if !s.channelLocal(w, r) {
		return
	}
	var req struct {
		ID    string `json:"id"`
		Cwd   string `json:"cwd"`
		Title string `json:"title"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.ID = strings.ToLower(req.ID)
	if !channelUUID.MatchString(req.ID) {
		fail(w, 400, "无效的 Claude 会话 ID")
		return
	}
	cwd, err := validateCwd(req.Cwd)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		fail(w, 503, "宿主正在关闭")
		return
	}
	if s.channelLive(s.channels[req.ID]) {
		fail(w, 409, "此原生会话已经连接，请勿启动第二个进程")
		return
	}
	x, err := s.store.FindClaude(req.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fail(w, 500, "读取会话失败")
		return
	}
	if err == nil && s.runs[x.ID] != nil {
		fail(w, 409, "此会话仍由宿主执行，请先停止托管轮次")
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		title := strings.TrimSpace(req.Title)
		if title == "" {
			title = "Claude Code"
		}
		if n, _, e := claude.NativeHistory(s.cfg.ClaudeConfigDir, req.ID); e == nil && n.Title != "Claude session" {
			title = n.Title
		}
		x, err = s.store.Create(title, cwd, req.ID, true)
	}
	if err != nil {
		fail(w, 500, "创建会话失败")
		return
	}
	if x.Cwd != cwd {
		fail(w, 409, "会话项目目录与原记录不一致")
		return
	}
	if err = s.store.SetMode(x.ID, "channel"); err != nil {
		fail(w, 500, "保存会话模式失败")
		return
	}
	if old := s.channels[req.ID]; old != nil && len(old.queue) > 0 {
		_, _ = s.store.Append(x.ID, "error", "终端连接已重建，未确认送达的消息不会自动重发。请检查会话后重新发送。", nil)
	}
	c := &channelConnection{sessionID: x.ID, key: store.ID() + store.ID(), seen: time.Now(), queue: []channelCommand{}}
	if old := s.channels[req.ID]; old != nil {
		// The bridge re-registers the same Claude session (e.g. after a missed
		// heartbeat); a hook still long-polling keeps its question on the new
		// connection. Questions whose hook is gone expire with the old one.
		for _, p := range s.requests {
			if p.connection == old && p.ctx.Err() == nil {
				p.connection = c
			}
		}
	}
	s.expireChannel(s.channels[req.ID])
	s.channels[req.ID] = c
	_ = s.store.SetStatus(x.ID, "idle")
	s.syncTranscript(c)
	x, _ = s.store.Get(x.ID)
	s.decorateChannel(&x)
	respond(w, 200, struct {
		store.Session
		ConnectionKey string `json:"connectionKey"`
	}{x, c.key})
}
func (s *Server) channelConnection(w http.ResponseWriter, r *http.Request) *channelConnection {
	c := s.channels[r.PathValue("id")]
	if !s.channelLive(c) || !equalToken("Bearer "+r.Header.Get("X-Channel-Key"), c.key) {
		fail(w, 409, "终端桥接连接已失效，请重新连接")
		return nil
	}
	return c
}
func (s *Server) channelHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !s.channelLocal(w, r) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channelConnection(w, r)
	if c == nil {
		return
	}
	c.seen = time.Now()
	s.syncTranscript(c)
	for id, p := range s.channelRequests {
		if p.connection == c && time.Since(p.created) > 30*time.Minute {
			_ = s.store.Resolve(id, "expired")
			delete(s.channelRequests, id)
		}
	}
	respond(w, 200, map[string]bool{"online": true})
}
func (s *Server) channelDisconnect(w http.ResponseWriter, r *http.Request) {
	if !s.channelLocal(w, r) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channelConnection(w, r)
	if c == nil {
		return
	}
	s.syncTranscript(c)
	s.expireChannel(c)
	delete(s.channels, r.PathValue("id"))
	_ = s.store.SetStatus(c.sessionID, "offline")
	respond(w, 200, map[string]bool{"online": false})
}
func (s *Server) expireChannel(c *channelConnection) {
	if c == nil {
		return
	}
	for id, p := range s.channelRequests {
		if p.connection == c {
			_ = s.store.Resolve(id, "expired")
			delete(s.channelRequests, id)
		}
	}
	for id, p := range s.requests {
		if p.connection == c {
			s.finishSharedLocked(id, p, "expired")
		}
	}
}

// sharedQuestionTimeout ends a waiting question before the hook gives up
// (askTimeout, 29 min), so a long wait is recorded as expired, not as answered in
// the terminal. A variable for tests.
var sharedQuestionTimeout = 28 * time.Minute

// channelInteraction is the long poll of a shared terminal's PermissionRequest
// hook for AskUserQuestion / ExitPlanMode. The native dialog stays open while it
// waits; the web answers through POST /api/requests/{id} like a managed question.
// A canceled request means the hook gave up because the terminal answered.
func (s *Server) channelInteraction(w http.ResponseWriter, r *http.Request) {
	if !s.channelLocal(w, r) {
		return
	}
	var req struct {
		Kind     string          `json:"kind"`
		ToolName string          `json:"toolName"`
		Input    json.RawMessage `json:"input"`
	}
	if !decode(w, r, &req) {
		return
	}
	var object map[string]any
	if !(req.Kind == "question" && req.ToolName == "AskUserQuestion") && !(req.Kind == "plan" && req.ToolName == "ExitPlanMode") || json.Unmarshal(req.Input, &object) != nil || object == nil {
		fail(w, 400, "无效的交互请求")
		return
	}
	claudeID := strings.ToLower(r.PathValue("id"))
	// Parse the transcript before taking s.mu; applying it under the lock records
	// earlier results first so they cannot expire this new question.
	n, events, historyErr := claude.NativeHistory(s.cfg.ClaudeConfigDir, claudeID)
	s.mu.Lock()
	c := s.channels[claudeID]
	if s.closing || !s.channelLive(c) {
		s.mu.Unlock()
		fail(w, 409, "终端桥接未连接")
		return
	}
	if historyErr == nil {
		s.applyTranscriptLocked(c, n, events)
	}
	x := store.Interaction{ID: store.ID(), SessionID: c.sessionID, Kind: req.Kind, ToolName: req.ToolName, Input: req.Input}
	p := &pending{sessionID: c.sessionID, ctx: r.Context(), channel: make(chan answer, 1), connection: c, toolName: req.ToolName, input: req.Input}
	if err := s.store.AddInteraction(x); err != nil {
		s.mu.Unlock()
		fail(w, 500, "无法保存交互请求")
		return
	}
	s.requests[x.ID] = p
	s.mu.Unlock()
	timer := time.NewTimer(sharedQuestionTimeout)
	defer timer.Stop()
	var a answer
	got := false
	status := "terminal"
	select {
	case a = <-p.channel:
		got = true
	case <-r.Context().Done():
	case <-timer.C:
		status = "expired"
	}
	s.mu.Lock()
	if _, open := s.requests[x.ID]; open {
		s.finishSharedLocked(x.ID, p, status)
		a = answer{Decision: "terminal"}
	} else if !got {
		// Already resolved (and its answer sent) while the timer or context fired.
		select {
		case a = <-p.channel:
		default:
			a = answer{Decision: "terminal"}
		}
	}
	s.mu.Unlock()
	web := a.Decision == "allow" || a.Decision == "deny"
	// The tool result lands in the transcript right after the decision; show it soon.
	// A web answer that arrived after the terminal had already answered was ignored
	// by Claude: the result then disagrees with it and the outcome becomes terminal.
	time.AfterFunc(1500*time.Millisecond, func() {
		n, events, err := claude.NativeHistory(s.cfg.ClaudeConfigDir, claudeID)
		if err != nil {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		conn := s.channels[claudeID]
		if s.closing || !s.channelLive(conn) {
			return
		}
		s.applyTranscriptLocked(conn, n, events)
		if web && sharedAnswerOverridden(events, p, a) {
			s.interactionEventLocked(x.ID, p, "terminal", answer{})
		}
	})
	respond(w, 200, a)
}

// sharedAnswerOverridden reports whether the transcript result of the latest call
// matching p contradicts the web answer a, i.e. the terminal answered first. An
// unknown or missing result is not a contradiction.
func sharedAnswerOverridden(events []claude.Event, p *pending, a answer) bool {
	want := questionTexts(p.input)
	useID := ""
	var result *claude.Event
	for i, ev := range events {
		var b struct {
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			ToolUseID string          `json:"tool_use_id"`
			Input     json.RawMessage `json:"input"`
		}
		if json.Unmarshal(ev.Payload, &b) != nil {
			continue
		}
		if ev.Kind == "tool" && b.Name == p.toolName && (p.toolName != "AskUserQuestion" || slices.Equal(questionTexts(b.Input), want)) {
			useID, result = b.ID, nil
		} else if ev.Kind == "tool_result" && useID != "" && b.ToolUseID == useID && result == nil {
			result = &events[i]
		}
	}
	if result == nil {
		return false
	}
	var b struct {
		IsError bool            `json:"is_error"`
		Content json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(result.Payload, &b)
	if b.IsError != (a.Decision == "deny") {
		return true
	}
	if p.toolName != "AskUserQuestion" || a.Decision != "allow" {
		return false
	}
	got := parseAnswers(contentText(b.Content))
	for q, ans := range a.Answers {
		if v, ok := got[q]; ok && v != ans {
			return true
		}
	}
	return false
}

// contentText is the text of tool_result content: a string, or its text blocks.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &blocks)
	parts := []string{}
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

var answerPair = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"="((?:[^"\\]|\\.)*)"`)
var answerEscape = regexp.MustCompile(`\\(.)`)

// parseAnswers reads `"question"="answer", ...` from an AskUserQuestion result.
func parseAnswers(text string) map[string]string {
	out := map[string]string{}
	for _, m := range answerPair.FindAllStringSubmatch(text, -1) {
		out[answerEscape.ReplaceAllString(m[1], "$1")] = answerEscape.ReplaceAllString(m[2], "$1")
	}
	return out
}

// finishSharedLocked withdraws a shared question without a web answer; the hook
// then prints no decision and the native dialog (or its result) stands.
func (s *Server) finishSharedLocked(id string, p *pending, status string) {
	delete(s.requests, id)
	if s.store.Resolve(id, status) == nil {
		s.interactionEventLocked(id, p, status, answer{})
	}
	select {
	case p.channel <- answer{Decision: "terminal"}:
	default:
	}
}

// interactionEventLocked records where a shared question ended (web, handoff,
// terminal, expired) so the transcript card can say so. The web ignores this
// event kind in the message list.
func (s *Server) interactionEventLocked(id string, p *pending, outcome string, a answer) {
	payload := map[string]any{"id": id, "toolName": p.toolName, "outcome": outcome, "decision": a.Decision}
	if p.toolName == "AskUserQuestion" {
		var input struct {
			Questions json.RawMessage `json:"questions"`
		}
		_ = json.Unmarshal(p.input, &input)
		payload["input"] = map[string]any{"questions": input.Questions}
		if len(a.Answers) > 0 {
			payload["answers"] = a.Answers
		}
	} else if a.Mode != "" {
		payload["mode"] = a.Mode
	}
	raw, _ := json.Marshal(payload)
	if _, err := s.store.Append(p.sessionID, "interaction", outcome, raw); err != nil {
		log.Printf("interaction event: %v", err)
	}
}

var planModes = map[string]bool{"default": true, "acceptEdits": true, "auto": true, "bypassPermissions": true}

// checkSharedAnswer validates a web answer for a hook-held question.
func checkSharedAnswer(p *pending, a *answer) string {
	if len(a.Message) > 64<<10 {
		return "反馈内容过长"
	}
	if a.Decision != "allow" {
		a.Answers, a.Mode = nil, ""
		return ""
	}
	if p.toolName == "ExitPlanMode" {
		a.Answers, a.Message = nil, ""
		if a.Mode == "" {
			a.Mode = "default"
		}
		if !planModes[a.Mode] {
			return "无效的权限模式"
		}
		return ""
	}
	a.Mode, a.Message = "", ""
	// Only answers to this call's questions travel to the hook.
	kept := map[string]string{}
	for _, q := range questionTexts(p.input) {
		v := a.Answers[q]
		if strings.TrimSpace(v) == "" {
			return "请回答全部问题"
		}
		if len(v) > 64<<10 {
			return "回答内容过长"
		}
		kept[q] = v
	}
	a.Answers = kept
	return ""
}

// questionTexts identifies an AskUserQuestion call by its question strings.
func questionTexts(input json.RawMessage) []string {
	var v struct {
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
	}
	_ = json.Unmarshal(input, &v)
	out := make([]string, 0, len(v.Questions))
	for _, q := range v.Questions {
		out = append(out, q.Question)
	}
	return out
}
func (s *Server) channelCommands(w http.ResponseWriter, r *http.Request) {
	if !s.channelLocal(w, r) {
		return
	}
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if err != nil || after < 0 {
		fail(w, 400, "无效游标")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channelConnection(w, r)
	if c == nil {
		return
	}
	if after > c.next {
		fail(w, 400, "游标超出命令范围")
		return
	}
	for len(c.queue) > 0 && c.queue[0].Seq <= after {
		c.queue = c.queue[1:]
	}
	// Polls are also a heartbeat; messages stay queued until the next acknowledged cursor.
	c.seen = time.Now()
	respond(w, 200, c.queue)
}
func (s *Server) channelMessageLocked(w http.ResponseWriter, x store.Session, text, prompt string, payload json.RawMessage) {
	c := s.channels[x.ClaudeSessionID]
	if !s.channelLive(c) {
		fail(w, 409, "原生终端未连接，请在项目目录运行连接命令")
		return
	}
	if len(c.queue) >= 256 {
		fail(w, 429, "消息队列已满，请等待终端接收")
		return
	}
	if _, err := s.store.Append(x.ID, "user", text, payload); err != nil {
		fail(w, 500, "保存消息失败")
		return
	}
	c.next++
	c.queue = append(c.queue, channelCommand{c.next, "notifications/claude/channel", map[string]any{"content": prompt, "meta": map[string]string{"source": "cc-remote", "chat_id": x.ClaudeSessionID}}})
	respond(w, 202, map[string]string{"status": "queued"})
}
func (s *Server) channelEvent(w http.ResponseWriter, r *http.Request) {
	if !s.channelLocal(w, r) {
		return
	}
	var req struct {
		Kind    string          `json:"kind"`
		Text    string          `json:"text"`
		Payload json.RawMessage `json:"payload"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channelConnection(w, r)
	if c == nil {
		return
	}
	switch req.Kind {
	case "assistant":
		if strings.TrimSpace(req.Text) == "" {
			fail(w, 400, "回复为空")
			return
		}
		if _, err := s.store.Append(c.sessionID, "assistant", req.Text, req.Payload); err != nil {
			fail(w, 500, "保存回复失败")
			return
		}
	case "channel_permission":
		var p struct {
			RequestID   string `json:"request_id"`
			ToolName    string `json:"tool_name"`
			Description string `json:"description"`
			Preview     string `json:"input_preview"`
		}
		if json.Unmarshal(req.Payload, &p) != nil || !permissionID.MatchString(p.RequestID) || p.ToolName == "" {
			fail(w, 400, "无效审批请求")
			return
		}
		// Consume prior progress before registering the new prompt, so a previous
		// call to the same tool cannot expire this newly received approval.
		s.syncTranscript(c)
		for _, old := range s.channelRequests {
			if old.connection == c && old.requestID == p.RequestID {
				respond(w, 200, map[string]bool{"ok": true})
				return
			}
		}
		id := store.ID()
		input, _ := json.Marshal(map[string]string{"description": p.Description, "input_preview": p.Preview})
		if err := s.store.AddInteraction(store.Interaction{ID: id, SessionID: c.sessionID, Kind: "approval", ToolName: p.ToolName, Input: input}); err != nil {
			fail(w, 500, "保存审批失败")
			return
		}
		s.channelRequests[id] = &channelApproval{c, p.RequestID, p.ToolName, time.Now()}
	default:
		fail(w, 400, "不支持的桥接事件")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) channelResolveLocked(w http.ResponseWriter, id string, p *channelApproval, a answer) {
	if !s.channelLive(p.connection) || time.Since(p.created) > 30*time.Minute {
		_ = s.store.Resolve(id, "expired")
		delete(s.channelRequests, id)
		fail(w, 409, "原生审批已失效")
		return
	}
	c := p.connection
	if len(c.queue) >= 256 {
		fail(w, 429, "终端队列已满")
		return
	}
	if err := s.store.Resolve(id, "resolved"); err != nil {
		fail(w, 409, err.Error())
		return
	}
	c.next++
	c.queue = append(c.queue, channelCommand{c.next, "notifications/claude/channel/permission", map[string]string{"request_id": p.requestID, "behavior": a.Decision}})
	delete(s.channelRequests, id)
	respond(w, 200, map[string]string{"status": "forwarded"})
}
func (s *Server) syncTranscript(c *channelConnection) {
	var claudeID string
	for id, x := range s.channels {
		if x == c {
			claudeID = id
			break
		}
	}
	if claudeID == "" {
		return
	}
	n, events, err := claude.NativeHistory(s.cfg.ClaudeConfigDir, claudeID)
	if err != nil {
		return
	}
	s.applyTranscriptLocked(c, n, events)
}

// applyTranscriptLocked records a parsed transcript. It may be parsed without
// s.mu: a snapshot is a prefix of any later one and recording is idempotent, so
// an older snapshot applied late adds nothing.
func (s *Server) applyTranscriptLocked(c *channelConnection, n claude.NativeSession, events []claude.Event) {
	if n.Title != "Claude session" {
		_ = s.store.SetTitle(c.sessionID, n.Title)
	}
	occurrences := map[string]int{}
	toolNames := map[string]string{}
	toolInputs := map[string]json.RawMessage{}
	for _, ev := range events {
		var tool struct {
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			ToolUseID string          `json:"tool_use_id"`
			Input     json.RawMessage `json:"input"`
		}
		_ = json.Unmarshal(ev.Payload, &tool)
		if ev.Kind == "tool" {
			toolNames[tool.ID] = tool.Name
			toolInputs[tool.ID] = tool.Input
		}
		// Remote prompts have already been recorded by the message endpoint.
		if ev.Kind == "user" && strings.HasPrefix(strings.TrimSpace(ev.Text), "<channel ") {
			continue
		}
		hash := sha256.Sum256(append([]byte(ev.Kind+"\x00"+ev.Text+"\x00"), ev.Payload...))
		key := hex.EncodeToString(hash[:])
		occurrences[key]++
		key = fmt.Sprintf("%s:%d", key, occurrences[key])
		added, e := s.store.RecordTranscript(c.sessionID, key, ev.Kind, ev.Text, ev.Payload)
		// A completed tool result after a prompt means the terminal has progressed.
		// Claude remains the authority on whether a relayed verdict is still valid.
		if added && e == nil && ev.Kind == "tool_result" {
			matches := 0
			for _, p := range s.channelRequests {
				if p.connection == c && toolNames[tool.ToolUseID] == p.toolName {
					matches++
				}
			}
			for id, p := range s.channelRequests {
				if matches == 1 && p.connection == c && toolNames[tool.ToolUseID] == p.toolName {
					_ = s.store.Resolve(id, "expired")
					delete(s.channelRequests, id)
				}
			}
			// Backstop for a hook that missed the terminal answer: the question is over.
			for id, p := range s.requests {
				if p.connection == c && toolNames[tool.ToolUseID] == p.toolName && (p.toolName != "AskUserQuestion" || slices.Equal(questionTexts(toolInputs[tool.ToolUseID]), questionTexts(p.input))) {
					s.finishSharedLocked(id, p, "terminal")
				}
			}
		}
	}
}

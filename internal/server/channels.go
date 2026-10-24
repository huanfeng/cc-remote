package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

const channelTTL = 45 * time.Second

// channelConnection binds a running terminal session to its current Claude
// session ID for the ask hook and transcript sync.
type channelConnection struct {
	sessionID string
	seen      time.Time
	// keyPrefix scopes transcript keys by Claude session ID, so /clear's new
	// transcript is not mistaken for the old one.
	keyPrefix string
	// synced: the first sync checked whether the session was synced by a
	// host without task events (store.LegacyTasksKey).
	synced, legacy bool
}

var channelUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (s *Server) channelLive(c *channelConnection) bool {
	return c != nil && time.Since(c.seen) < channelTTL
}
func (s *Server) decorate(x *store.Session) {
	if rt := s.procs[x.ID]; rt != nil && len(rt.sent) > 0 {
		x.Queued = rt.queuedMessages()
	}
	if x.Mode == "terminal" {
		s.decorateTerminalLocked(x)
	}
}

// Hook callbacks are local-only, in addition to bearer authentication.
func (s *Server) channelLocal(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		fail(w, 403, s.lang(r).T("The session bridge only accepts local connections", "会话桥接仅允许本机连接"))
		return false
	}
	return true
}

// sharedQuestionTimeout ends a waiting question before the hook gives up
// (claude.AskWait), so a long wait is recorded as expired, not as answered in
// the terminal. A variable for tests.
var sharedQuestionTimeout = claude.AskWait

// channelInteraction is the long poll of a terminal session's PermissionRequest
// hook: a tool approval, AskUserQuestion or ExitPlanMode. The native dialog stays
// open while it waits; the web answers through POST /api/requests/{id} like a
// managed request.
// A canceled request means the hook gave up because the terminal answered.
func (s *Server) channelInteraction(w http.ResponseWriter, r *http.Request) {
	if !s.channelLocal(w, r) {
		return
	}
	var req struct {
		Kind     string          `json:"kind"`
		ToolName string          `json:"toolName"`
		Input    json.RawMessage `json:"input"`
		AgentID  string          `json:"agentId"` // a subagent's call: not in the session's transcript
	}
	if !decode(w, r, &req) {
		return
	}
	var object map[string]any
	special := req.ToolName == "AskUserQuestion" || req.ToolName == "ExitPlanMode"
	valid := req.Kind == "question" && req.ToolName == "AskUserQuestion" || req.Kind == "plan" && req.ToolName == "ExitPlanMode" || req.Kind == "approval" && !special && req.ToolName != "" && len(req.ToolName) <= 256
	if !valid || json.Unmarshal(req.Input, &object) != nil || object == nil {
		fail(w, 400, s.lang(r).T("Invalid interaction request", "无效的交互请求"))
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
		fail(w, 409, s.lang(r).T("The terminal session is not running", "终端会话没有运行"))
		return
	}
	if historyErr == nil {
		s.applyTranscriptLocked(c, n, events)
	}
	x := store.Interaction{ID: store.ID(), SessionID: c.sessionID, Kind: req.Kind, ToolName: req.ToolName, Input: req.Input}
	p := &pending{sessionID: c.sessionID, ctx: r.Context(), channel: make(chan answer, 1), connection: c, toolName: req.ToolName, input: req.Input, noBackstop: req.AgentID != ""}
	p.onScreen = req.Kind == "approval" && req.AgentID == ""
	if historyErr == nil && !p.noBackstop {
		p.useID, p.noBackstop = askedCall(p, events)
	}
	if err := s.store.AddInteraction(x); err != nil {
		s.mu.Unlock()
		fail(w, 500, s.lang(r).T("Could not save the interaction request", "无法保存交互请求"))
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
	web := a.Decision == "allow" || a.Decision == "deny"
	if web {
		s.dialogsAnsweredLocked(c.sessionID)
	}
	s.mu.Unlock()
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
	if p.toolName != "AskUserQuestion" && p.toolName != "ExitPlanMode" {
		// An allowed tool can still fail, so its result does not tell who answered.
		return false
	}
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
		if ev.Kind == "tool" && b.Name == p.toolName && sameCall(p, b.Input) {
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
	if status == "terminal" { // expired or handed off, the dialog is still open
		s.dialogsAnsweredLocked(p.sessionID)
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
func checkSharedAnswer(l i18n.Lang, p *pending, a *answer) string {
	if len(a.Message) > 64<<10 {
		return l.T("The feedback is too long", "反馈内容过长")
	}
	if a.Decision != "allow" {
		a.Answers, a.Mode = nil, ""
		return ""
	}
	if p.toolName != "AskUserQuestion" && p.toolName != "ExitPlanMode" {
		a.Answers, a.Mode, a.Message = nil, "", ""
		return ""
	}
	if p.toolName == "ExitPlanMode" {
		a.Answers, a.Message = nil, ""
		if a.Mode == "" {
			a.Mode = "default"
		}
		if !planModes[a.Mode] {
			return l.T("Invalid permission mode", "无效的权限模式")
		}
		return ""
	}
	a.Mode, a.Message = "", ""
	// Only answers to this call's questions travel to the hook.
	kept := map[string]string{}
	for _, q := range questionTexts(p.input) {
		v := a.Answers[q]
		if strings.TrimSpace(v) == "" {
			return l.T("Answer every question", "请回答全部问题")
		}
		if len(v) > 64<<10 {
			return l.T("The answer is too long", "回答内容过长")
		}
		kept[q] = v
	}
	a.Answers = kept
	return ""
}

// sameCall reports whether a transcript tool_use input is the call p waits on.
// askedCall finds the transcript call p asks about: the one matching call that
// has no result yet. Several such calls (identical calls in flight) are
// ambiguous: then no transcript result may end p.
func askedCall(p *pending, events []claude.Event) (useID string, ambiguous bool) {
	var open []string
	done := map[string]bool{}
	for _, ev := range events {
		var b struct {
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			ToolUseID string          `json:"tool_use_id"`
			Input     json.RawMessage `json:"input"`
		}
		if json.Unmarshal(ev.Payload, &b) != nil {
			continue
		}
		if ev.Kind == "tool" && b.Name == p.toolName && sameCall(p, b.Input) {
			open = append(open, b.ID)
		} else if ev.Kind == "tool_result" {
			done[b.ToolUseID] = true
		}
	}
	open = slices.DeleteFunc(open, func(id string) bool { return done[id] })
	if len(open) == 1 {
		return open[0], false
	}
	return "", len(open) > 1
}

// dialogsAnsweredLocked ends "waiting" once a session's last open dialog was
// answered: a denied tool has no PostToolUse to clear it, and a session still
// waiting takes no new prompt.
func (s *Server) dialogsAnsweredLocked(sessionID string) {
	for _, p := range s.requests {
		if p.sessionID == sessionID {
			return
		}
	}
	if x, err := s.store.Get(sessionID); err == nil && x.Status == "waiting" {
		_ = s.store.SetStatus(sessionID, "running")
	}
}

func sameCall(p *pending, input json.RawMessage) bool {
	var a, b map[string]any
	_ = json.Unmarshal(p.input, &a)
	_ = json.Unmarshal(input, &b)
	return claude.SameCall(p.toolName, a, b)
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

// applyTranscriptLocked records a parsed transcript. It may be parsed without
// s.mu: a snapshot is a prefix of any later one and recording is idempotent, so
// an older snapshot applied late adds nothing.
func (s *Server) applyTranscriptLocked(c *channelConnection, n claude.NativeSession, events []claude.Event) {
	_ = s.store.SetConversationTime(c.sessionID, n.LastConversationAt, n.ConversationTimeEstimated)
	if n.Title != "Claude session" {
		_ = s.store.SetTitle(c.sessionID, n.Title)
	}
	if !c.synced {
		c.synced = true
		c.legacy = s.store.TranscriptFlag(c.sessionID, store.LegacyTasksKey)
		_ = s.store.MarkTranscript(c.sessionID, store.TranscriptV2Key)
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
		// Transcripts of sessions migrated from the retired shared mode carry web
		// prompts as <channel> tags; the web already recorded those messages.
		if ev.Kind == "user" && strings.HasPrefix(strings.TrimSpace(ev.Text), "<channel ") {
			continue
		}
		hash := sha256.Sum256(append([]byte(ev.Kind+"\x00"+ev.Text+"\x00"), ev.Payload...))
		key := hex.EncodeToString(hash[:])
		occurrences[key]++
		key = fmt.Sprintf("%s%s:%d", c.keyPrefix, key, occurrences[key])
		if c.legacy && (ev.Kind == "status" || ev.Kind == "teammate") {
			// Old history the earlier host showed without them (teammate
			// messages as user turns, which the web splits).
			_ = s.store.MarkTranscript(c.sessionID, key)
			continue
		}
		added, e := s.store.RecordTranscriptAt(c.sessionID, key, ev.Kind, ev.Text, ev.Payload, ev.CreatedAt)
		if added && e == nil && ev.Kind == "tool_result" {
			// Backstop for a hook that missed the terminal answer: the question is over.
			for id, p := range s.requests {
				if p.connection == c && !p.noBackstop && (p.useID == "" || p.useID == tool.ToolUseID) && toolNames[tool.ToolUseID] == p.toolName && sameCall(p, toolInputs[tool.ToolUseID]) {
					s.finishSharedLocked(id, p, "terminal")
				}
			}
		}
	}
	if c.legacy {
		c.legacy = false
		_ = s.store.ClearTranscriptFlag(c.sessionID, store.LegacyTasksKey)
	}
}

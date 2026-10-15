package server

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
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
	s.expireChannel(s.channels[req.ID])
	c := &channelConnection{sessionID: x.ID, key: store.ID() + store.ID(), seen: time.Now(), queue: []channelCommand{}}
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
	if n.Title != "Claude session" {
		_ = s.store.SetTitle(c.sessionID, n.Title)
	}
	occurrences := map[string]int{}
	toolNames := map[string]string{}
	for _, ev := range events {
		var tool struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			ToolUseID string `json:"tool_use_id"`
		}
		_ = json.Unmarshal(ev.Payload, &tool)
		if ev.Kind == "tool" {
			toolNames[tool.ID] = tool.Name
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
		}
	}
}

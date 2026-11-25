package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

type forkRequest struct {
	AnchorSeq     int64    `json:"anchorSeq"`
	AnchorToken   string   `json:"anchorToken"`
	Before        bool     `json:"before"`
	AttachmentIDs []string `json:"attachmentIds"`
	RequestID     string   `json:"requestId"`
}

type forkResponse struct {
	Session       store.Session `json:"session"`
	AttachmentIDs []string      `json:"attachmentIds,omitempty"`
	Attachments   []attachment  `json:"attachments,omitempty"`
}

func canForkSession(x store.Session) bool {
	return x.Resume && x.ClaudeSessionID != "" && (x.Provider == "claude" || x.Mode == "managed")
}

func (s *Server) forkSession(w http.ResponseWriter, r *http.Request) {
	s.fork(w, r, "")
}

func (s *Server) forkNativeSession(w http.ResponseWriter, r *http.Request) {
	if provider, ok := requestProvider(w, r); ok {
		s.fork(w, r, provider)
	}
}

func (s *Server) fork(w http.ResponseWriter, r *http.Request, provider string) {
	l := s.lang(r)
	var req forkRequest
	if !decode(w, r, &req) {
		return
	}
	anchored := req.AnchorSeq > 0
	if req.AnchorSeq < 0 || anchored && len(req.AnchorToken) != 64 || !anchored && req.AnchorToken != "" ||
		req.Before && !anchored || len(req.AttachmentIDs) > maxAttachments || len(req.AttachmentIDs) > 0 && !req.Before ||
		req.RequestID != "" && !validSendID.MatchString(req.RequestID) {
		fail(w, 400, l.T("Invalid branch request", "无效的分支请求"))
		return
	}
	if !channelUUID.MatchString(r.PathValue("id")) {
		fail(w, 404, l.T("Session not found", "会话不存在"))
		return
	}
	id := r.PathValue("id")
	sourceKey := provider + ":" + id
	// Serialize retries until their durable receipt is committed. Other host
	// operations keep using s.mu and do not wait for provider tooling.
	s.forkMu.Lock()
	defer s.forkMu.Unlock()
	data, _ := json.Marshal(req)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(data))
	if req.RequestID != "" {
		receipt, err := s.store.ForkReceipt(sourceKey, req.RequestID)
		if err == nil {
			if receipt.Fingerprint != fingerprint {
				fail(w, 409, l.T("This branch request was already used with different input", "此分支请求已用于其他输入"))
				return
			}
			s.respondFork(w, r, 200, receipt)
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			fail(w, 500, l.T("Could not read the branch request", "无法读取分支请求"))
			return
		}
	}
	if r.Context().Err() != nil {
		return
	}
	var source store.Session
	var release func()
	s.mu.Lock()
	var err error
	if provider == "" {
		source, err = s.store.Get(id)
	} else {
		source, err = s.store.FindNative(provider, id)
		if errors.Is(err, sql.ErrNoRows) {
			source, err = store.Session{Provider: provider, ClaudeSessionID: id}, nil
		}
	}
	if err != nil {
		s.mu.Unlock()
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, l.T("Session not found", "会话不存在"))
		} else {
			fail(w, 500, l.T("Could not read the session", "无法读取会话"))
		}
		return
	}
	if s.closing {
		s.mu.Unlock()
		fail(w, 503, l.T("The host is shutting down", "宿主正在关闭"))
		return
	}
	if s.updatingProvider != "" && s.updatingProvider == source.Provider {
		s.mu.Unlock()
		fail(w, 409, l.T("This provider is updating. Try again in a moment.", "该组件正在更新，请稍后重试。"))
		return
	}
	if provider == "" && !canForkSession(source) || source.Mode == "terminal" && source.Provider == "codex" && s.terminals[source.ID] != nil {
		s.mu.Unlock()
		fail(w, 409, l.T("Open the saved native conversation to branch from this session", "请打开已保存的原生会话并从那里创建分支"))
		return
	}
	if source.ID != "" {
		if s.forkSourceBusyLocked(source) {
			s.mu.Unlock()
			fail(w, 409, l.T("Wait for the current turn to finish before branching", "请等当前轮次结束后再创建分支"))
			return
		}
		s.switching[source.ID] = true
		defer func() {
			s.mu.Lock()
			delete(s.switching, source.ID)
			s.mu.Unlock()
		}()
	}
	if isCodexAccountID(source.ProfileID) {
		release = s.holdCodexAccountLocked(source.ProfileID)
	}
	s.mu.Unlock()
	if release != nil {
		defer release()
	}
	native, nativeHistory, err := s.forkHistory(r.Context(), source.Provider, source.ClaudeSessionID)
	if err != nil || len(nativeHistory) == 0 {
		fail(w, 404, l.T("Could not read the native conversation", "无法读取原生会话"))
		return
	}
	if source.Provider == "codex" && native.ProfileID != source.ProfileID {
		if source.ID != "" && (native.ProfileID != "" || isCodexAccountID(source.ProfileID)) {
			fail(w, 409, l.T("This Codex conversation belongs to another account", "此 Codex 会话属于另一个账号"))
			return
		}
		if source.ID == "" && native.ProfileID != "" {
			s.mu.Lock()
			release = s.holdCodexAccountLocked(native.ProfileID)
			s.mu.Unlock()
			defer release()
		}
	}
	child := store.Session{
		ID: store.ID(), Provider: source.Provider, Cwd: source.Cwd, Title: source.Title,
		ProfileID: source.ProfileID, ProfileJSON: source.ProfileJSON, PermissionMode: source.PermissionMode,
		Model: source.Model, Effort: source.Effort, ForkedFromID: source.ID, Mode: "managed",
	}
	if provider == "" && source.Provider == "claude" && source.Mode == "terminal" && !req.Before && claude.CheckTerminalArgs(source.ClaudeArgs) == "" {
		child.Mode, child.ClaudeArgs = "terminal", source.ClaudeArgs
	}
	if provider != "" {
		child.Cwd, child.Title = native.Cwd, native.Title
	}
	if source.ID == "" {
		profile, err := s.nativeImportProfile(source.Provider, native.ProfileID, nil)
		if err != nil {
			fail(w, 409, i18n.Text(l, err))
			return
		}
		if profile != nil {
			child.ProfileID, child.ProfileJSON = profile.ID, profile.snapshot()
		}
	}
	child.Cwd, err = validateCwd(l, child.Cwd)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(child.Title) == "" {
		child.Title = l.T("Session", "会话")
	}
	child.Title = forkTitle(child.Title, l.T(" (branch)", "（分支）"))

	boundary, fresh := "", false
	if anchored {
		projected := nativeEvents(native, nativeHistory)
		index := -1
		if provider == "" {
			index = trackedForkAnchors(s, source.ID, projected)[req.AnchorSeq] - 1
		} else if req.AnchorSeq <= int64(len(projected)) {
			index = int(req.AnchorSeq - 1)
		}
		if index < 0 || !projected[index].Branchable || projected[index].BranchAnchor != req.AnchorToken ||
			req.Before && !projected[index].EditableInBranch {
			fail(w, 409, l.T("This conversation point changed or is unavailable; reload the history", "此分支位置已变化或不可用，请重新加载历史"))
			return
		}
		var ok bool
		boundary, fresh, ok = forkBoundary(native, nativeHistory, index, req.Before)
		if !ok {
			fail(w, 409, l.T("The earlier conversation point is unavailable", "此前的会话位置不可用"))
			return
		}
	}
	transfer := forkUploadTransfer{s: s, lang: l, source: source.ID, child: child.ID, copied: map[string]attachment{}}
	defer transfer.cleanup()
	if err = transfer.prepare(req.AttachmentIDs); err != nil {
		fail(w, 400, err.Error())
		return
	}
	var childHistory []store.Event
	var transcriptKeys []string
	if fresh {
		child.ClaudeSessionID = store.ID()
	} else {
		opts := claude.Options{Provider: child.Provider, Executable: s.cfg.ClaudeExecutable, SessionID: native.ID, Cwd: child.Cwd, ConfigDir: s.cfg.ClaudeConfigDir, Resume: true}
		p := s.effectivePreferences(child)
		opts.Model, opts.Effort, opts.PermissionMode = p.Model, p.Effort, p.PermissionMode
		fork := claude.ForkNativeSession
		if child.Provider == "codex" {
			opts.Executable, opts.ConfigDir = s.cfg.CodexExecutable, s.cfg.CodexConfigDir
			fork = codex.ForkNativeSession
		}
		if err := s.profileOptions(child, &opts); err != nil {
			fail(w, 409, i18n.Text(l, err))
			return
		}
		if child.Provider == "codex" && opts.ModelProvider == "" {
			opts.ModelProvider = native.ModelProvider
		}
		if s.cfg.ForkNativeSession != nil {
			fork = s.cfg.ForkNativeSession
		}
		// Once provider creation starts, finish recording its result even if the
		// HTTP client disconnects. A retry can then recover the same child.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 35*time.Second)
		defer cancel()
		child.ClaudeSessionID, err = fork(ctx, opts, boundary)
		if err != nil || !channelUUID.MatchString(child.ClaudeSessionID) || strings.EqualFold(child.ClaudeSessionID, native.ID) {
			if err != nil {
				fail(w, 502, l.T("Could not branch the native conversation: ", "无法创建原生会话分支：")+i18n.Text(l, err))
			} else {
				fail(w, 502, l.T("The provider did not return a new conversation", "服务商未返回新会话"))
			}
			return
		}
		childNative, events, err := s.forkHistory(ctx, child.Provider, child.ClaudeSessionID)
		if err != nil {
			fail(w, 502, l.T("Could not read the new branch history", "无法读取新分支的历史"))
			return
		}
		child.Resume = true
		childHistory = nativeEvents(childNative, events)
		if child.Mode == "terminal" {
			transcriptKeys = []string{store.TranscriptV2Key}
			occur := map[string]int{}
			for _, ev := range events {
				transcriptKeys = append(transcriptKeys, transcriptEventKey(strings.ToLower(child.ClaudeSessionID)+":", ev, occur))
			}
		}
		if err = transfer.rewrite(childHistory); err != nil {
			fail(w, 500, l.T("Could not save branch attachments", "无法保存分支附件"))
			return
		}
	}
	receipt := store.ForkReceipt{Source: sourceKey, RequestID: req.RequestID, Fingerprint: fingerprint, SessionID: child.ID, AttachmentIDs: req.AttachmentIDs}
	if err := s.store.CreateFork(child, childHistory, transcriptKeys, receipt); err != nil {
		fail(w, 500, l.T("Could not save the new branch", "无法保存新分支"))
		return
	}
	transfer.keep = true
	s.respondFork(w, r, 201, receipt)
}

func (s *Server) respondFork(w http.ResponseWriter, r *http.Request, status int, receipt store.ForkReceipt) {
	x, err := s.store.Get(receipt.SessionID)
	if err != nil {
		fail(w, 500, s.lang(r).T("Could not open the new branch", "无法打开新分支"))
		return
	}
	s.mu.Lock()
	s.decorate(&x)
	s.mu.Unlock()
	res := forkResponse{Session: x, AttachmentIDs: receipt.AttachmentIDs}
	for _, id := range receipt.AttachmentIDs {
		a, _, err := s.loadAttachment(s.lang(r), x.ID, id)
		if err != nil {
			fail(w, 500, s.lang(r).T("Could not open a branch attachment", "无法读取分支附件"))
			return
		}
		res.Attachments = append(res.Attachments, a)
	}
	respond(w, status, res)
}

func (s *Server) forkHistory(ctx context.Context, provider, id string) (claude.NativeSession, []claude.Event, error) {
	if provider == "codex" {
		return s.codexNativeHistory(ctx, id)
	}
	return claude.NativeHistory(s.cfg.ClaudeConfigDir, id)
}

func (s *Server) forkSourceBusyLocked(source store.Session) bool {
	if s.switching[source.ID] || s.runs[source.ID] != nil || source.Status == "running" || source.Status == "waiting" {
		return true
	}
	if rt := s.procs[source.ID]; rt != nil {
		if rt.turn || rt.steering || rt.approvals > 0 || len(rt.sent) > 0 || rt.proc != nil && rt.proc.BackgroundTasks() > 0 {
			return true
		}
	}
	if ts := s.terminals[source.ID]; ts != nil {
		if ts.closing || len(ts.send) > 0 || len(ts.queued) > 0 || s.requests[ts.resumeID] != nil || s.requests[ts.usageID] != nil {
			return true
		}
		if ts.runner != nil {
			activity, retry := ts.runner.ActivityState()
			return activity != "" || retry != nil
		}
	}
	return false
}

func forkTitle(title, suffix string) string {
	for len(title)+len(suffix) > 300 {
		_, size := utf8.DecodeLastRuneInString(title)
		title = title[:len(title)-size]
	}
	return title + suffix
}

func eventBranchAnchor(x claude.NativeSession, ev claude.Event) string {
	if !forkMessage(ev.Kind) || ev.NativeAnchor == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(x.Provider + "\x00" + x.ID + "\x00" + ev.NativeAnchor + "\x00" + ev.Kind + "\x00" + ev.Text))
	return hex.EncodeToString(digest[:])
}

func forkMessage(kind string) bool { return kind == "user" || kind == "assistant" }

func forkBoundary(session claude.NativeSession, history []claude.Event, at int, before bool) (string, bool, bool) {
	selected := history[at]
	if !before {
		if session.Provider != "codex" && selected.Kind == "user" {
			// A branch from a user message keeps the rest of its current round.
			for _, ev := range history[at+1:] {
				if ev.Kind == "user" {
					break
				}
				if ev.NativeAnchor != "" {
					selected = ev
				}
			}
		}
		return selected.NativeAnchor, false, selected.NativeAnchor != ""
	}
	if !selected.NativeParentKnown || selected.NativeParentAnchor == "" && session.Truncated {
		return "", false, false
	}
	return selected.NativeParentAnchor, selected.NativeParentAnchor == "", true
}

func (s *Server) markForkEvents(ctx context.Context, source store.Session, events []store.Event) {
	if !canForkSession(source) {
		return
	}
	for _, ev := range events {
		if forkMessage(ev.Kind) {
			native, history, err := s.forkHistory(ctx, source.Provider, source.ClaudeSessionID)
			if err == nil {
				projected := nativeEvents(native, history)
				anchors := trackedForkAnchors(s, source.ID, projected)
				for i := range events {
					if index := anchors[events[i].Seq]; index > 0 {
						events[i].Branchable = true
						events[i].EditableInBranch = projected[index-1].EditableInBranch
						events[i].BranchAnchor = projected[index-1].BranchAnchor
					}
				}
			}
			return
		}
	}
}

func forkMessageKey(ev store.Event) string {
	var p struct{ Attachments []attachment }
	_ = json.Unmarshal(ev.Payload, &p)
	var key strings.Builder
	key.WriteString(ev.Kind + "\x00" + strings.ReplaceAll(ev.Text, "\r\n", "\n"))
	for _, a := range p.Attachments {
		key.WriteString("\x00" + a.ID)
	}
	return key.String()
}

// A projected message must be identifiable in the current native conversation.
// Older tracked logs have no provider IDs. Use exact matching of the entire
// ordered message list when possible, otherwise only unambiguous matches.
func trackedForkAnchors(s *Server, id string, projected []store.Event) map[int64]int {
	local, older, err := s.store.EventsPage(id, 0, 10000, 0)
	anchors := map[int64]int{}
	if err != nil || older {
		return anchors
	}
	var localMessages, nativeMessages []store.Event
	for _, ev := range local {
		if ev.Kind == "compact" {
			var p struct{ Clear bool }
			if json.Unmarshal(ev.Payload, &p) == nil && p.Clear {
				localMessages = localMessages[:0]
			}
		}
		if forkMessage(ev.Kind) {
			localMessages = append(localMessages, ev)
		}
	}
	for _, ev := range projected {
		if forkMessage(ev.Kind) {
			nativeMessages = append(nativeMessages, ev)
		}
	}
	if len(localMessages) == len(nativeMessages) {
		match := true
		for i, ev := range localMessages {
			if forkMessageKey(ev) != forkMessageKey(nativeMessages[i]) || !forkMessageTimeMatches(ev, nativeMessages[i]) {
				match = false
				break
			}
		}
		if match {
			for i, ev := range localMessages {
				if nativeMessages[i].Branchable {
					anchors[ev.Seq] = int(nativeMessages[i].Seq)
				}
			}
			return anchors
		}
	}
	localCount, nativeCount := map[string]int{}, map[string]int{}
	nativeIndex := map[string]int{}
	for _, ev := range localMessages {
		localCount[forkMessageKey(ev)]++
	}
	for _, ev := range nativeMessages {
		key := forkMessageKey(ev)
		nativeCount[key]++
		if ev.Branchable {
			nativeIndex[key] = int(ev.Seq)
		}
	}
	for _, ev := range localMessages {
		key := forkMessageKey(ev)
		if index := nativeIndex[key]; localCount[key] == 1 && nativeCount[key] == 1 && index > 0 && forkMessageTimeMatches(ev, projected[index-1]) {
			anchors[ev.Seq] = index
		}
	}
	return anchors
}

func forkMessageTimeMatches(local, native store.Event) bool {
	localAt, localErr := time.Parse(time.RFC3339Nano, local.CreatedAt)
	nativeAt, nativeErr := time.Parse(time.RFC3339Nano, native.CreatedAt)
	if localErr != nil || nativeErr != nil {
		return false
	}
	const tolerance = 10 * time.Second
	return localAt.Sub(nativeAt).Abs() <= tolerance
}

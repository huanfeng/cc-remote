// An in-memory host. It behaves like the real one closely enough to design
// against: replies stream in as assistant_delta events, tools run and finish,
// approvals unblock the turn, queued messages are delivered after it, and the
// terminal is a small fake TUI that echoes and answers.
import { nanoid } from 'nanoid';
import { BackendError, type Backend, type ClaudeSettings, type TranscriptUpdate, type Unsubscribe, type UploadOptions } from '../backend';
import type {
  Attachment, FileContent, NewSessionRequest, PendingRequest, Preferences, Provider, RequestAnswer, SendRequest,
  Session, SessionEvent, SessionPatch, TerminalHandlers, TerminalStream, UsageReply,
} from '../types';
import * as fx from './fixtures';
import { dangerousPermission } from '@/domain/providers';

const clone = <T,>(v: T): T => structuredClone(v);
const wait = (ms: number) => new Promise(r => setTimeout(r, ms));
const latency = () => wait(120 + Math.random() * 180);
const now = () => new Date().toISOString();

type Watcher = (u: TranscriptUpdate) => void;

export class MockBackend implements Backend {
  readonly kind = 'mock' as const;
  private sessions = clone(fx.sessions);
  private events: Record<string, SessionEvent[]> = clone(fx.transcripts);
  private requests: PendingRequest[] = clone(fx.requests);
  private listWatchers = new Set<(s: Session[]) => void>();
  private watchers = new Map<string, Set<Watcher>>();
  private prefs: Record<Provider, Preferences> = {
    claude: { provider: 'claude', permissionMode: 'default', model: '', effort: '', revision: 'r1' },
    codex: { provider: 'codex', permissionMode: 'workspace-write', model: '', effort: '', revision: 'r1' },
  };
  private oauth = [{ id: 'oauth:yjw', name: 'yjw' }];
  private identityValue = {
    name: 'Cartech 工作站', color: '#3b82f6', icon: 'laptop', shownName: 'Cartech 工作站', shownColor: '#3b82f6',
    defaultName: 'CARTECH-PC', defaultColor: '#3b82f6',
    palette: ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#14b8a6', '#64748b'].map((hex, i) => ({ key: `c${i}`, hex })),
  };

  constructor() {
    // s1 is mid-turn: its test run finishes shortly after the page opens.
    setTimeout(() => this.finishS1(), 6000);
  }

  // ---------- plumbing ----------
  private session(id: string): Session {
    const s = this.sessions.find(x => x.id === id);
    if (!s) throw new BackendError(`session ${id} not found`, 404);
    return s;
  }
  private nextSeq(id: string) { return (this.events[id]?.at(-1)?.seq ?? 0) + 1; }
  private emitList() { const list = clone(this.sessions); for (const w of this.listWatchers) w(list); }
  private emit(id: string, events: SessionEvent[], reset = false) {
    const ws = this.watchers.get(id);
    if (!ws?.size) return;
    const u: TranscriptUpdate = {
      session: clone(this.session(id)), events: clone(events), reset, hasMore: false,
      requests: clone(this.requests.filter(r => r.sessionId === id && r.status === 'pending')),
    };
    for (const w of ws) w(u);
  }
  private append(id: string, kind: string, fields: Partial<SessionEvent> = {}): SessionEvent {
    const event: SessionEvent = { seq: this.nextSeq(id), kind, createdAt: now(), ...fields };
    (this.events[id] ??= []).push(event);
    const s = this.session(id);
    s.updatedAt = event.createdAt;
    if (kind === 'user' || kind === 'assistant') s.lastConversationAt = event.createdAt;
    this.emit(id, [event]);
    return event;
  }
  private setStatus(id: string, status: Session['status'], activity?: string) {
    const s = this.session(id);
    s.status = status;
    if (s.live) s.live.activity = activity;
    this.emitList();
    this.emit(id, []);
  }

  /** Streams a reply in chunks, as the host does with assistant_delta. */
  private async stream(id: string, text: string) {
    const parts = text.match(/[\s\S]{1,6}/g) ?? [];
    let acc = '';
    for (const p of parts) {
      acc += p;
      this.append(id, 'assistant_delta', { text: p });
      await wait(35);
    }
    this.append(id, 'assistant', { text: acc });
  }

  private async runTurn(id: string, prompt: string) {
    const s = this.session(id);
    this.setStatus(id, 'running', '正在思考…');
    await wait(900);
    if (/测试|test|构建|build/i.test(prompt)) {
      const tid = nanoid(6);
      this.setStatus(id, 'running', '正在运行 Bash');
      this.append(id, 'tool', { payload: { type: 'tool_use', id: tid, name: 'Bash', input: { command: 'pnpm test', description: '运行测试' } } });
      await wait(2200);
      this.append(id, 'tool_result', { payload: { tool_use_id: tid, content: '# tests 54\n# pass 54\n# fail 0', is_error: false } });
    } else {
      const tid = nanoid(6);
      this.append(id, 'tool', { payload: { type: 'tool_use', id: tid, name: 'Grep', input: { pattern: prompt.slice(0, 12) || 'TODO', path: '.' } } });
      await wait(700);
      this.append(id, 'tool_result', { payload: { tool_use_id: tid, content: 'src/App.tsx:12\nsrc/lib/i18n.ts:4', is_error: false } });
    }
    await this.stream(id, `收到：「${prompt.slice(0, 40)}」。这是模拟后端的回复，用来检查**流式渲染**、\`行内代码\`和列表：\n\n- 第一点\n- 第二点\n\n完成。`);
    if (s.live) { s.live.costUSD += 0.12; s.live.contextTokens += 3200; s.live.contextPercent = Math.min(100, s.live.contextTokens / (s.live.contextSize || 200000) * 100); }
    this.setStatus(id, 'completed');
    // Deliver what was queued during the turn.
    const next = s.queued?.shift();
    if (next) { this.emitList(); this.append(id, 'user', { text: next.text }); void this.runTurn(id, next.text); }
  }

  private async finishS1() {
    if (this.session('s1').status !== 'running') return;
    this.append('s1', 'tool_result', { payload: { tool_use_id: 't7', content: '✓ typecheck\n# tests 6\n# pass 6\n# fail 0', is_error: false } });
    await this.stream('s1', '类型检查和用量测试都通过了。改动要点：\n\n1. 页面隐藏时轮询间隔放宽到 5 分钟；\n2. 下一次成功读取会清除 `stale` 标记。');
    this.setStatus('s1', 'completed');
    const next = this.session('s1').queued?.shift();
    if (next) { this.emitList(); this.append('s1', 'user', { text: next.text }); void this.runTurn('s1', next.text); }
  }

  // ---------- Backend ----------
  async info() { await latency(); return clone(fx.hostInfo); }

  watchSessions(onChange: (s: Session[]) => void): Unsubscribe {
    this.listWatchers.add(onChange);
    queueMicrotask(() => onChange(clone(this.sessions)));
    return () => this.listWatchers.delete(onChange);
  }

  async createSession(body: NewSessionRequest): Promise<Session> {
    await latency();
    if (body.mode === 'terminal' && !body.trustFolder) {
      throw new BackendError('该文件夹尚未被 Claude Code 信任，请勾选「信任此文件夹」。', 428, 'trust-required');
    }
    const id = nanoid(8);
    const s: Session = {
      id, provider: body.provider ?? 'claude', title: body.title || body.cwd.split(/[\\/]/).filter(Boolean).pop() || '新会话',
      cwd: body.cwd, status: 'idle', mode: body.mode ?? 'managed', online: true, createdAt: now(), updatedAt: now(),
      permissionMode: body.permissionMode, model: body.model, effort: body.effort,
      live: { costUSD: 0, durationMs: 0, contextTokens: 0, contextSize: 200_000, contextPercent: 0 },
    };
    this.sessions.unshift(s);
    this.events[id] = [];
    this.emitList();
    return clone(s);
  }

  async patchSession(id: string, patch: SessionPatch) {
    await latency();
    const s = this.session(id);
    Object.assign(s, { ...patch, confirmDangerous: undefined });
    this.emitList(); this.emit(id, []);
    return clone(s);
  }
  async deleteSession(id: string, opts?: { stop?: boolean }) {
    await latency();
    if (this.session(id).status === 'running' && !opts?.stop) throw new BackendError('会话正在运行，请先停止。', 409);
    this.sessions = this.sessions.filter(s => s.id !== id);
    delete this.events[id];
    this.emitList();
  }
  async stop(id: string) { await latency(); this.append(id, 'error', { text: '已被用户中断' }); this.setStatus(id, 'interrupted'); }
  async exit(id: string) { await latency(); this.session(id).online = false; this.setStatus(id, 'idle'); }
  async switchProfile(id: string, profileId: string, force?: boolean) {
    await latency();
    const s = this.session(id);
    if ((s.status === 'running' || (s.activeTasks ?? 0) > 0) && !force) throw new BackendError('会话正在运行，需要确认后中断。', 409);
    s.profileId = profileId; s.profileName = fx.profiles.profiles.find(p => p.id === profileId)?.name;
    this.emitList(); return clone(s);
  }

  watchTranscript(id: string, onUpdate: Watcher, opts?: { native?: Provider }): Unsubscribe {
    if (opts?.native) {
      const events: SessionEvent[] = [
        { seq: 1, kind: 'user', text: '（终端历史）这是一段只读记录。', createdAt: fx.ago(300) },
        { seq: 2, kind: 'assistant', text: '原生历史会话只能查看；可以**以终端会话继续**，或**导入为托管会话**。', createdAt: fx.ago(299) },
      ];
      const native = [...fx.nativeClaude, ...fx.nativeCodex].find(n => n.id === id);
      const session: Session = { id, title: native?.title ?? id, cwd: native?.cwd ?? '', provider: opts.native, updatedAt: native?.updatedAt ?? now() };
      setTimeout(() => onUpdate({ session, events, hasMore: false, requests: [] }), 150);
      return () => {};
    }
    let set = this.watchers.get(id);
    if (!set) this.watchers.set(id, (set = new Set()));
    set.add(onUpdate);
    setTimeout(() => {
      onUpdate({
        session: clone(this.session(id)), events: clone(this.events[id] ?? []), reset: true, hasMore: id === 's5',
        requests: clone(this.requests.filter(r => r.sessionId === id && r.status === 'pending')),
      });
    }, 180);
    return () => { set!.delete(onUpdate); };
  }

  async loadOlder(_id: string, beforeSeq: number) {
    await wait(500);
    const base = new Date(Date.now() - 60 * 60_000 * 30).getTime();
    const events: SessionEvent[] = [
      { seq: beforeSeq - 2, kind: 'user', text: '（更早的消息）先确认一下 ConPTY 在 Windows 10 上的可用性。', createdAt: new Date(base).toISOString() },
      { seq: beforeSeq - 1, kind: 'assistant', text: 'ConPTY 需要 Windows 10 1809 及以上，cc-remote 启动时会检测版本。', createdAt: new Date(base + 9000).toISOString() },
    ].filter(e => e.seq > 0);
    return { events, hasMore: false };
  }

  async fullEvent(id: string, seq: number) {
    await latency();
    const e = this.events[id]?.find(x => x.seq === seq);
    if (!e) throw new BackendError('event not found', 404);
    return clone({ ...e, truncated: false });
  }

  async send(id: string, body: SendRequest) {
    await latency();
    const s = this.session(id);
    if (s.status === 'running' || s.status === 'waiting') {
      (s.queued ??= []).push({ id: nanoid(6), text: body.text, editable: true, canSendNow: true, delivery: 'pending' });
      this.emitList(); this.emit(id, []);
      return { status: 'queued' as const };
    }
    const attachments = (body.attachmentIds ?? []).map(a => this.uploads.get(a)).filter(Boolean);
    this.append(id, 'user', { text: body.text, payload: attachments.length ? { attachments } : undefined });
    void this.runTurn(id, body.text);
    return { status: 'running' as const };
  }

  private steered = new Set<string>();
  async steer(id: string, text: string, sendId: string) {
    await latency();
    if (this.steered.has(sendId)) return { status: 'steered', duplicate: true };
    this.steered.add(sendId);
    this.append(id, 'user', { text, payload: { steer: true } });
    return { status: 'steered' };
  }
  async steerStatus(_id: string, sendId: string) {
    await latency();
    return this.steered.has(sendId) ? { status: 'steered' } : { status: 'unknown', delivery: 'unknown' as const };
  }

  async commands() {
    await latency();
    return [
      { name: '/compact', description: '压缩对话上下文' },
      { name: '/clear', description: '清空对话' },
      { name: '/model', description: '切换模型' },
      { name: '/effort', description: '调整推理强度' },
      { name: '/review', description: '审查当前改动' },
      { name: '/init', description: '生成 CLAUDE.md' },
      { name: '/status', description: '查看会话状态' },
      { name: '/config', description: '打开设置' },
      { name: '/resume', description: '恢复其它会话', terminalOnly: true },
      { name: '/doctor', description: '诊断安装问题', terminalOnly: true },
    ];
  }

  async runCommand(id: string, command: string) {
    await latency();
    if (command.startsWith('/config')) return { openSettings: true };
    if (command.startsWith('/status')) return { message: `会话 ${id}：${this.session(id).status}` };
    if (command.startsWith('/compact')) {
      this.append(id, 'compact', { payload: { compact_metadata: { trigger: 'manual', pre_tokens: 96_400, post_tokens: 18_200 } } });
      return { message: '已压缩上下文' };
    }
    this.append(id, 'user', { text: command });
    void this.runTurn(id, command);
    return { sendMessage: false };
  }

  private paths = [
    'web/', 'web/src/', 'web/src/App.tsx', 'web/src/Usage.tsx', 'web/src/TerminalView.tsx', 'web-next/', 'web-next/src/App.tsx',
    'internal/', 'internal/server/server.go', 'internal/server/terminal.go', 'internal/store/store.go', 'docs/architecture.md',
    'docs/redesign-plan.md', 'README.md', 'go.mod',
  ];
  async mentions(_id: string, q: string) {
    await wait(80);
    const needle = q.toLowerCase();
    return this.paths.filter(p => p.toLowerCase().includes(needle)).slice(0, 8);
  }

  private uploads = new Map<string, Attachment>();
  async upload(_id: string, file: File, opts?: UploadOptions): Promise<Attachment> {
    for (const f of [0.25, 0.5, 0.75, 1]) {
      if (opts?.signal?.aborted) throw new DOMException('Upload cancelled', 'AbortError');
      await wait(150);
      opts?.onProgress?.(f);
    }
    const a: Attachment = {
      id: nanoid(8), kind: file.type.startsWith('image/') ? 'image' : 'file', name: file.name, mime: file.type, size: file.size,
      url: URL.createObjectURL(file),
    };
    this.uploads.set(a.id, a);
    return a;
  }

  async assetUrl(url: string) { return url; }

  async editQueued(id: string, mid: string, text: string) {
    await latency();
    const m = this.session(id).queued?.find(q => q.id === mid);
    if (m) m.text = text;
    this.emitList(); this.emit(id, []);
  }
  async removeQueued(id: string, mid: string) {
    await latency();
    const s = this.session(id);
    s.queued = s.queued?.filter(q => q.id !== mid);
    this.emitList(); this.emit(id, []);
  }
  async sendQueuedNow(id: string, mid: string) {
    await latency();
    const s = this.session(id);
    const m = s.queued?.find(q => q.id === mid);
    if (!m) return;
    s.queued = s.queued?.filter(q => q.id !== mid);
    this.append(id, 'user', { text: m.text, payload: { steer: true } });
    this.emitList();
  }

  async answer(requestId: string, a: RequestAnswer) {
    await latency();
    const r = this.requests.find(x => x.id === requestId);
    if (!r || r.status !== 'pending') throw new BackendError('该请求已处理', 409);
    r.status = 'resolved';
    const id = r.sessionId!;
    const call = [...(this.events[id] ?? [])].reverse().find(e => e.kind === 'tool' && e.payload?.name === r.toolName);
    const callId = call?.payload?.id;
    this.append(id, 'interaction', { payload: { id: r.id, toolName: r.toolName, outcome: a.decision === 'terminal' ? 'handoff' : 'web', decision: a.decision, answers: a.answers, mode: a.mode } });
    if (a.decision === 'terminal') { this.emit(id, []); return; }
    if (r.kind === 'question' && callId) {
      const text = Object.entries(a.answers ?? {}).map(([q, v]) => `"${q}"="${v}"`).join(', ');
      this.append(id, 'tool_result', { payload: { tool_use_id: callId, content: a.decision === 'allow' ? `User has answered your questions: ${text}. You can now continue with the user's answers in mind.` : a.message || 'User declined to answer.', is_error: a.decision !== 'allow' } });
    } else if (callId) {
      this.append(id, 'tool_result', { payload: { tool_use_id: callId, content: a.decision === 'allow' ? '' : `The user doesn't want to proceed with this tool use. ${a.message ?? ''}`, is_error: a.decision !== 'allow' } });
    }
    if (a.decision === 'allow') void this.continueAfterAnswer(id);
    else this.setStatus(id, 'idle');
  }
  private async continueAfterAnswer(id: string) {
    this.setStatus(id, 'running', '继续执行…');
    await wait(800);
    await this.stream(id, '好的，已按你的选择继续。');
    this.setStatus(id, 'completed');
  }

  async tasks(id: string) { await latency(); return clone(fx.tasks[id] ?? []); }
  async stats(id: string) {
    await latency();
    const s = this.session(id);
    return {
      sessionId: id, turns: 14, results: 14, costUSD: s.live?.costUSD ?? 0, costKnown: true, inputTokens: 412_000, outputTokens: 38_100,
      cacheReadTokens: 1_820_000, cacheCreationTokens: 96_000, durationMs: s.live?.durationMs ?? 0, toolCalls: 63, errors: 2, lastActivityAt: s.updatedAt,
    };
  }

  async readFile(_id: string, path: string): Promise<FileContent> {
    await wait(250);
    const name = path.split(/[\\/]/).pop() ?? path;
    if (/\.(png|jpe?g|gif|webp|svg)$/i.test(name)) {
      const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360"><rect width="100%" height="100%" fill="#e5e7eb"/><text x="50%" y="50%" font-size="28" text-anchor="middle" fill="#6b7280" font-family="sans-serif">${name}</text></svg>`;
      return { path, mime: 'image/svg+xml', size: svg.length, url: `data:image/svg+xml;utf8,${encodeURIComponent(svg)}` };
    }
    if (/\.md$/i.test(name)) return { path, mime: 'text/markdown', size: 900, text: `# ${name}\n\n这是模拟的 Markdown 文件内容。\n\n- 支持**渲染**与源码切换\n- 代码高亮：\n\n\`\`\`go\nfunc main() { fmt.Println("hi") }\n\`\`\`\n` };
    const text = Array.from({ length: 80 }, (_, i) => i === 40 ? `export function useUsage(sessionId?: string) { // line ${i + 1}` : `// ${name} line ${i + 1}`).join('\n');
    return { path, mime: 'text/plain', size: text.length, text };
  }

  async fileLink(_id: string, path: string) {
    await latency();
    const name = path.split(/[\\/]/).pop() ?? path;
    const blob = new Blob([`mock download of ${path}\n`], { type: 'application/octet-stream' });
    return { url: URL.createObjectURL(blob), name, size: blob.size };
  }

  async nativeSessions(provider: Provider) { await latency(); return clone(provider === 'codex' ? fx.nativeCodex : fx.nativeClaude); }
  async importNative(id: string, provider: Provider) {
    const n = [...fx.nativeClaude, ...fx.nativeCodex].find(x => x.id === id);
    return this.createSession({ provider, title: n?.title ?? id, cwd: n?.cwd ?? '' });
  }

  openTerminal(id: string, h: TerminalHandlers): TerminalStream {
    return new FakeTerminal(this.session(id), h);
  }

  async models({ provider }: { provider: Provider }) { await latency(); return clone(fx.models[provider]); }
  async profiles() { await latency(); return clone(fx.profiles); }
  async setDefaultProfile() { await latency(); }
  async preferences(provider: Provider) { await latency(); return clone(this.prefs[provider]); }
  async savePreferences(provider: Provider, p: Preferences & { confirmDangerous?: boolean }) {
    await latency();
    if (p.revision !== this.prefs[provider].revision) throw new BackendError('默认值已在别处修改，请刷新后重试。', 409);
    // Like the host: a dangerous mode is refused unless the request confirms it.
    if (dangerousPermission(p.permissionMode, provider) && !p.confirmDangerous) throw new BackendError('危险权限模式需要确认。', 400, 'confirm-dangerous');
    this.prefs[provider] = { ...p, revision: `r${Number(p.revision.slice(1)) + 1}`, confirmDangerous: undefined } as Preferences;
    return clone(this.prefs[provider]);
  }
  private claude: ClaudeSettings = { model: 'opus', effortLevel: 'high', permissionMode: 'default', revision: 'r3', path: 'C:/Users/Cartech/.claude/settings.json' };
  async claudeSettings() { await latency(); return clone(this.claude); }
  async saveClaudeSettings(v: Omit<ClaudeSettings, 'path'> & { confirmDangerous?: boolean }) {
    await latency();
    if (v.revision !== this.claude.revision) throw new BackendError('settings.json 已在别处修改，请刷新后重试。', 409);
    if (dangerousPermission(v.permissionMode, 'claude') && !v.confirmDangerous) throw new BackendError('危险权限模式需要确认。', 400, 'confirm-dangerous');
    this.claude = { ...this.claude, model: v.model, effortLevel: v.effortLevel, permissionMode: v.permissionMode, revision: `r${Number(v.revision.slice(1)) + 1}` };
    return clone(this.claude);
  }
  async identity() { await latency(); return clone(this.identityValue); }
  async saveIdentity(v: { name: string; color: string; icon: string }) {
    await latency();
    Object.assign(this.identityValue, v, { shownName: v.name || this.identityValue.defaultName, shownColor: v.color || this.identityValue.defaultColor });
    return clone(this.identityValue);
  }
  async oauthProfiles() { await latency(); return clone(this.oauth); }
  async addOAuthProfile(name: string) { await latency(); this.oauth.push({ id: `oauth:${nanoid(4)}`, name }); }
  async replaceOAuthProfile(id: string) { await latency(); if (!this.oauth.some(o => o.id === id)) throw new BackendError('token not found', 404); }
  async removeOAuthProfile(id: string) { await latency(); this.oauth = this.oauth.filter(o => o.id !== id); }

  watchUsage({ provider }: { provider: Provider }, onChange: (u: UsageReply) => void): Unsubscribe {
    const t = setTimeout(() => onChange(clone(fx.usage[provider])), 300);
    return () => clearTimeout(t);
  }
  async usageHistory(hours: number) {
    await latency();
    const n = Math.min(hours, 48);
    return {
      hours,
      points: Array.from({ length: n }, (_, i) => ({
        at: new Date(Date.now() - (n - i) * (hours / n) * 3600_000).toISOString(),
        fiveHour: Math.round(20 + 18 * Math.sin(i / 3) + i % 7),
        weekly: Math.round(30 + i * (34 / n)),
      })),
    };
  }
  async status() { await latency(); return clone(fx.hostStatus); }
}

// ---------- A fake Claude Code TUI for terminal mode ----------
const ESC = '\x1b[';
class FakeTerminal implements TerminalStream {
  private line = '';
  private cols = 100;
  private rows = 30;
  private closed = false;
  private held: boolean;
  constructor(private session: Session, private h: TerminalHandlers) {
    this.held = (session.attached ?? 0) > 0;
    h.onState('connecting');
    setTimeout(() => {
      if (this.closed) return;
      h.onState('open');
      h.onSize({ cols: this.cols, rows: this.rows, held: this.held });
      this.banner();
    }, 400);
  }
  private out(s: string) { if (!this.closed) this.h.onData(s.replace(/\n/g, '\r\n')); }
  private banner() {
    const w = Math.min(this.cols - 2, 72);
    const edge = (s: string) => `${ESC}38;5;209m${s}${ESC}0m`;
    // Pads by visible width; the styled text is wrapped after padding.
    const row = (plain: string, style: string) => `${edge('│')}${style}${plain}${ESC}0m${' '.repeat(Math.max(0, w - plain.length))}${edge('│')}\n`;
    this.out(`${ESC}2J${ESC}H`);
    this.out(`${edge(`╭${'─'.repeat(w)}╮`)}\n`);
    this.out(row(' ✻ Welcome to Claude Code', `${ESC}1m`));
    this.out(row(`   cwd: ${this.session.cwd}`, `${ESC}2m`));
    this.out(`${edge(`╰${'─'.repeat(w)}╯`)}\n\n`);
    this.out(`${ESC}2m> 试试输入 /help，或者任意文字后回车（这是模拟终端）${ESC}0m\n\n`);
    this.prompt();
  }
  private prompt() { this.out(`${ESC}1;36m❯${ESC}0m `); }
  send(data: string) {
    for (const ch of data) {
      if (ch === '\r') { this.submit(); continue; }
      if (ch === '\x7f') { if (this.line) { this.line = this.line.slice(0, -1); this.out('\b \b'); } continue; }
      if (ch === '\x03') { this.line = ''; this.out(`^C\n`); this.prompt(); continue; }
      if (ch === '\x1b') continue;
      if (ch >= ' ') { this.line += ch; this.out(ch); }
    }
  }
  private submit() {
    const text = this.line.trim();
    this.line = '';
    this.out('\n');
    if (!text) { this.prompt(); return; }
    if (text === '/exit') {
      // Claude Code exiting: the host closes the stream with 4001.
      this.out(`${ESC}2mBye!${ESC}0m\n`);
      setTimeout(() => { if (!this.closed) { this.closed = true; this.h.onState('exited'); } }, 300);
      return;
    }
    if (text === '/drop') {
      // A dropped connection: the view should back off and reconnect.
      setTimeout(() => { if (!this.closed) { this.closed = true; this.h.onState('failed', 'connection lost'); } }, 200);
      return;
    }
    if (text === '/help') {
      this.out(`${ESC}1mCommands${ESC}0m\n  /compact   压缩上下文\n  /model     切换模型\n  /resume    恢复会话\n  /exit      退出（模拟 4001）\n  /drop      断开连接（模拟断线）\n\n`);
      this.prompt(); return;
    }
    const steps = ['✻ Thinking…', '● Read(web/src/App.tsx)', `  ⎿  Read 1040 lines`];
    let i = 0;
    const tick = () => {
      if (this.closed) return;
      if (i < steps.length) { this.out(`${ESC}38;5;209m${steps[i]}${ESC}0m\n`); i++; setTimeout(tick, 450); return; }
      this.out(`\n${ESC}1m●${ESC}0m 收到「${text}」。这是模拟终端的回复。\n\n`);
      this.prompt();
    };
    tick();
  }
  resize(cols: number, rows: number) {
    if (this.held) return; // a local ccr owns the size
    this.cols = cols; this.rows = rows;
    setTimeout(() => this.h.onSize({ cols, rows, held: false }), 60);
  }
  redraw() { setTimeout(() => this.banner(), 150); }
  close() { this.closed = true; }
}

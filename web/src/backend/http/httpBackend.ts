// The real host behind the Backend interface. Every endpoint, body and reply
// shape follows internal/server (the host refuses unknown body fields, so
// bodies carry only fields the host's info.features declare). Polling lives
// here: the session list every 3 s, a watched transcript every 1.5 s (events
// after the last seq, plus pending requests), usage every 60 s.
//
// Plain TypeScript with explicit .ts imports and no React: node --test loads it.
import type {
  Backend, ClaudeSettings, TranscriptUpdate, Unsubscribe, UploadOptions,
} from '../backend.ts';
import { BackendError } from '../errors.ts';
import type {
  Attachment, CommandReply, FileContent, HostFeature, HostIdentity, HostInfo, HostStatus, ModelList, NativeSession,
  NewSessionRequest, PendingRequest, Preferences, ProfileCatalog, Provider, RequestAnswer, SendRequest, Session,
  SessionEvent, SessionPatch, SessionStats, SlashCommand, TaskInfo, TerminalHandlers, TerminalStream, UsageHistory,
  UsageReply,
} from '../types.ts';
import { HttpClient, defined, providerQuery, withQuery, type Lang } from './client.ts';
import { poll, type Poller } from './poll.ts';
import { HttpTerminalStream, type WebSocketCtor } from './terminal.ts';

/** Events per page: the first tail and each older page. */
export const PAGE = 200;

export interface HttpBackendOptions {
  /** '' for the page's own origin, or 'https://host:port' for a standalone page. */
  baseUrl: string;
  token: string;
  lang: Lang;
  /** The host refused the token (401). Called once; the shell signs out. */
  onUnauthorized?: () => void;
  // ---- injected in tests ----
  fetch?: typeof fetch;
  WebSocket?: WebSocketCtor;
  /** null: upload with fetch (the desktop app, whose fetch goes through Rust). */
  XMLHttpRequest?: typeof XMLHttpRequest | null;
  /** Polling periods in ms. */
  intervals?: Partial<{ sessions: number; transcript: number; usage: number }>;
}

const enc = encodeURIComponent;
const EMPTY: PendingRequest[] = [];

type Native = Provider | undefined;

export class HttpBackend implements Backend {
  readonly kind = 'http' as const;
  private readonly client: HttpClient;
  private readonly o: HttpBackendOptions;
  private readonly intervals: { sessions: number; transcript: number; usage: number };
  private disposed = false;
  private infoValue?: HostInfo;
  private infoLoad?: Promise<HostInfo>;

  // The shared session list poll.
  private listPoller?: Poller;
  private listWatchers = new Set<{ onChange: (s: Session[]) => void; onError?: (e: BackendError) => void }>();
  private list?: Session[];
  /** Watched transcripts, told when a new list arrives so their session record follows at once. */
  private listFollowers = new Set<() => void>();
  /** Watched transcripts, for a poll right after an action on their session. */
  private transcriptPollers = new Map<string, Set<Poller>>();
  private usagePollers = new Set<Poller>();
  private streams = new Set<TerminalStream>();
  private assets = new Map<string, Promise<string>>();
  /** Pending requests filtered from the host's reply, keyed by that reply (identity survives a 304). */
  private pendingOf = new WeakMap<PendingRequest[], PendingRequest[]>();

  constructor(o: HttpBackendOptions) {
    this.o = o;
    this.client = new HttpClient({ baseUrl: o.baseUrl, token: o.token, lang: o.lang, onUnauthorized: o.onUnauthorized, fetch: o.fetch });
    this.intervals = { sessions: 3000, transcript: 1500, usage: 60_000, ...o.intervals };
  }

  dispose() {
    this.disposed = true;
    this.listPoller?.stop();
    this.listPoller = undefined;
    this.listWatchers.clear();
    this.listFollowers.clear();
    for (const set of this.transcriptPollers.values()) for (const p of set) p.stop();
    this.transcriptPollers.clear();
    for (const p of this.usagePollers) p.stop();
    this.usagePollers.clear();
    for (const s of this.streams) s.close();
    this.streams.clear();
    for (const a of this.assets.values()) void a.then(revoke, () => {});
    this.assets.clear();
    this.client.clearCache();
  }

  // ---------- plumbing ----------
  private get<T>(path: string, signal?: AbortSignal) { return this.client.json<T>(path, { signal }); }
  private call<T>(method: string, path: string, body?: unknown) { return this.client.json<T>(path, { method, body }); }
  private session(id: string) { return `/api/sessions/${enc(id)}`; }
  private t(en: string, zh: string) { return this.client.t(en, zh); }

  /** The host's features: from the last info(), fetched once if none was read yet. */
  private async features(): Promise<Set<HostFeature>> {
    const info = this.infoValue ?? await (this.infoLoad ??= this.info().finally(() => { this.infoLoad = undefined; }));
    return new Set(info.features ?? []);
  }

  /** Polls the list and the session's transcripts at once, so an action shows without waiting for a tick. */
  private refresh(id?: string) {
    if (this.disposed) return;
    this.listPoller?.now();
    if (id) for (const p of this.transcriptPollers.get(id) ?? []) p.now();
    else for (const set of this.transcriptPollers.values()) for (const p of set) p.now();
  }

  private async pending(id: string): Promise<PendingRequest[]> {
    const all = await this.get<PendingRequest[]>(`/api/requests?sessionId=${enc(id)}`);
    if (!Array.isArray(all)) return EMPTY;
    let mine = this.pendingOf.get(all);
    if (!mine) { mine = all.filter(r => r.status === 'pending'); this.pendingOf.set(all, mine); }
    return mine;
  }

  // ---------- host ----------
  async info(): Promise<HostInfo> {
    const info = await this.get<HostInfo>('/api/info');
    this.infoValue = info;
    return info;
  }

  // ---------- sessions ----------
  watchSessions(onChange: (s: Session[]) => void, onError?: (e: BackendError) => void): Unsubscribe {
    const w = { onChange, onError };
    this.listWatchers.add(w);
    const known = this.list;
    if (known) queueMicrotask(() => { if (this.listWatchers.has(w) && this.list === known) onChange(known); });
    this.ensureList();
    return () => {
      this.listWatchers.delete(w);
      this.releaseList();
    };
  }

  /** Watched transcripts take their session record from the list, so it polls while any watch is held. */
  private listUsers = 0;
  private ensureList() {
    this.listUsers++;
    if (this.listPoller || this.disposed) return;
    this.listPoller = poll(async () => {
      const next = await this.get<Session[]>('/api/sessions');
      if (this.disposed || !Array.isArray(next)) return;
      if (next === this.list) return; // 304: nothing changed
      this.list = next;
      for (const w of [...this.listWatchers]) w.onChange(next);
      for (const f of [...this.listFollowers]) f();
    }, this.intervals.sessions, {
      onError: e => { for (const w of [...this.listWatchers]) w.onError?.(asBackendError(e)); },
    });
  }
  private releaseList() {
    if (--this.listUsers > 0) return;
    this.listUsers = 0;
    this.listPoller?.stop();
    this.listPoller = undefined;
  }

  async createSession(body: NewSessionRequest): Promise<Session> {
    const { provider, profileId, title, cwd, mode, trustFolder, resume, claudeArgs, permissionMode, model, effort, confirmDangerous } = body;
    const s = await this.call<Session>('POST', '/api/sessions', defined({
      // Claude is the default; an older host may not know the field.
      provider: provider === 'codex' ? provider : undefined,
      profileId, title, cwd, mode, trustFolder, resume, claudeArgs, permissionMode, model, effort, confirmDangerous,
    }));
    this.refresh();
    return s;
  }

  async patchSession(id: string, patch: SessionPatch): Promise<Session> {
    const { permissionMode, model, effort, confirmDangerous, archived } = patch;
    const s = await this.call<Session>('PATCH', this.session(id), defined({ permissionMode, model, effort, confirmDangerous, archived }));
    this.refresh(id);
    return s;
  }

  async deleteSession(id: string, opts?: { stop?: boolean }) {
    await this.call('DELETE', `${this.session(id)}${opts?.stop ? '?stop=1' : ''}`);
    this.refresh();
  }
  async stop(id: string) { await this.call('POST', `${this.session(id)}/stop`); this.refresh(id); }
  async exit(id: string) { await this.call('POST', `${this.session(id)}/exit`); this.refresh(id); }
  async switchProfile(id: string, profileId: string, force?: boolean): Promise<Session> {
    const s = await this.call<Session>('POST', `${this.session(id)}/profile`, { profileId, ...(force ? { force: true } : {}) });
    this.refresh(id);
    return s;
  }

  // ---------- transcript ----------
  private transcriptBase(id: string, native: Native) {
    return native ? `/api/native/sessions/${enc(id)}` : this.session(id);
  }

  watchTranscript(id: string, onUpdate: (u: TranscriptUpdate) => void, opts?: { native?: Provider }): Unsubscribe {
    const native = opts?.native;
    const base = this.transcriptBase(id, native);
    const q = (path: string) => providerQuery(path, native);
    // The page on screen tells the host someone is looking (terminal sessions: focus for recaps).
    const viewing = () => (typeof document === 'undefined' || !document.hidden ? '&viewing=1' : '');
    let closed = false;
    let loaded = false;
    let lastSeq = 0;
    let hasMore = false;
    let session: Session | undefined;
    let requests: PendingRequest[] = EMPTY;

    const listed = () => (native ? undefined : this.list?.find(s => s.id === id));
    const load = async () => {
      const data = await this.get<{ session: Session; events: SessionEvent[]; hasMore?: boolean }>(q(`${base}?tail=${PAGE}&compact=1${viewing()}`));
      const pending = native ? EMPTY : await this.pending(id);
      if (closed) return;
      const events = data.events ?? [];
      loaded = true;
      lastSeq = events.at(-1)?.seq ?? 0;
      hasMore = !!data.hasMore;
      session = native ? { ...data.session, provider: data.session.provider ?? native } : data.session;
      requests = pending;
      onUpdate({ session, events, reset: true, hasMore, requests });
    };
    const tick = async () => {
      if (!loaded) { await load(); return; }
      let events: SessionEvent[];
      try {
        events = await this.get<SessionEvent[]>(q(`${base}/events?after=${lastSeq}&compact=1${viewing()}`));
      } catch (e) {
        // 409: the transcript was rewritten and renumbered; load it again from the end.
        if (e instanceof BackendError && e.status === 409) { loaded = false; if (!closed) await load(); return; }
        throw e;
      }
      const pending = native ? EMPTY : await this.pending(id);
      if (closed) return;
      const fresh = (Array.isArray(events) ? events : []).filter(e => e.seq > lastSeq);
      if (fresh.length) lastSeq = fresh[fresh.length - 1].seq;
      const nextSession = listed() ?? session!;
      // Unchanged replies keep their identity (ETag), so a quiet tick delivers nothing.
      // The session record (status, queue, live) comes with the list: when the
      // transcript moves, poll the list now instead of up to 3 s later.
      if (!native && (fresh.length || pending !== requests)) this.listPoller?.now();
      if (!fresh.length && pending === requests && nextSession === session) return;
      session = nextSession;
      requests = pending;
      onUpdate({ session, events: fresh, hasMore, requests });
    };
    // A new list delivers the refreshed session record right away.
    const follow = () => {
      if (closed || !loaded) return;
      const next = listed();
      if (!next || next === session) return;
      session = next;
      onUpdate({ session, events: [], hasMore, requests });
    };
    if (!native) this.listFollowers.add(follow);

    if (!native) this.ensureList();
    const poller = poll(tick, this.intervals.transcript, { pauseHidden: true });
    let set = this.transcriptPollers.get(id);
    if (!set) this.transcriptPollers.set(id, (set = new Set()));
    set.add(poller);
    return () => {
      if (closed) return;
      closed = true;
      poller.stop();
      set!.delete(poller);
      if (!set!.size && this.transcriptPollers.get(id) === set) this.transcriptPollers.delete(id);
      this.listFollowers.delete(follow);
      if (!native) this.releaseList();
    };
  }

  async loadOlder(id: string, beforeSeq: number, opts?: { native?: Provider }) {
    const path = providerQuery(`${this.transcriptBase(id, opts?.native)}/events?before=${beforeSeq}&limit=${PAGE}&compact=1`, opts?.native);
    const data = await this.get<{ events: SessionEvent[]; hasMore: boolean }>(path);
    return { events: data.events ?? [], hasMore: !!data.hasMore };
  }

  fullEvent(id: string, seq: number, opts?: { native?: Provider }) {
    return this.get<SessionEvent>(providerQuery(`${this.transcriptBase(id, opts?.native)}/events/${seq}`, opts?.native));
  }

  // ---------- composer ----------
  async send(id: string, body: SendRequest): Promise<{ status: 'running' | 'queued'; duplicate?: boolean }> {
    const f = await this.features();
    const r = await this.call<{ status: 'running' | 'queued'; duplicate?: boolean }>('POST', `${this.session(id)}/messages`, {
      text: body.text,
      ...(body.attachmentIds?.length ? { attachmentIds: body.attachmentIds } : {}),
      // An older host refuses fields it does not know (DisallowUnknownFields).
      ...(body.defer && f.has('queueControls') ? { defer: true } : {}),
      ...(body.sendId && f.has('sendId') ? { sendId: body.sendId } : {}),
    });
    this.refresh(id);
    return r;
  }

  async steer(id: string, text: string, sendId: string) {
    const f = await this.features();
    if (!f.has('codexSteer')) throw new BackendError(this.t('This host cannot add instructions to a running turn', '此宿主不支持向运行中的轮次追加指令'), 400, 'unsupported', 'rejected');
    const r = await this.call<{ status: string; duplicate?: boolean }>('POST', `${this.session(id)}/steer`, { text, sendId });
    this.refresh(id);
    return r;
  }

  async steerStatus(id: string, sendId: string) {
    try {
      return await this.get<{ status: string }>(`${this.session(id)}/steer/${enc(sendId)}`);
    } catch (e) {
      // 409 is the answer, not a failure: the delivery is pending or unknown (or was rejected).
      if (e instanceof BackendError && e.status === 409 && e.delivery) return { status: 'unconfirmed', delivery: e.delivery };
      throw e;
    }
  }

  async commands(id: string) {
    const list = await this.get<SlashCommand[]>(`${this.session(id)}/commands`);
    return Array.isArray(list) ? list : [];
  }

  async runCommand(id: string, command: string) {
    const r = await this.call<CommandReply>('POST', `${this.session(id)}/commands`, { command });
    this.refresh(id);
    return r ?? {};
  }

  async mentions(id: string, q: string) {
    if (!(await this.features()).has('mentions')) return [];
    const r = await this.get<{ paths?: string[] }>(`${this.session(id)}/mentions?q=${enc(q)}`);
    return r?.paths ?? [];
  }

  upload(id: string, file: File, opts?: UploadOptions): Promise<Attachment> {
    const path = `${this.session(id)}/uploads`;
    const XHR = this.o.XMLHttpRequest === null ? undefined : this.o.XMLHttpRequest ?? (typeof XMLHttpRequest !== 'undefined' ? XMLHttpRequest : undefined);
    if (!XHR) return this.uploadByFetch(path, file, opts);
    return new Promise<Attachment>((resolve, reject) => {
      if (opts?.signal?.aborted) { reject(abortError()); return; }
      const xhr = new XHR();
      xhr.open('POST', this.client.url(path));
      for (const [k, v] of Object.entries(this.client.authHeaders())) xhr.setRequestHeader(k, v);
      xhr.upload.onprogress = e => { if (e.lengthComputable && e.total > 0) opts?.onProgress?.(Math.min(1, e.loaded / e.total)); };
      xhr.onload = () => {
        const response = new Response(xhr.status === 204 ? null : xhr.responseText, { status: xhr.status, headers: { 'Content-Type': 'application/json' } });
        if (xhr.status >= 200 && xhr.status < 300) {
          try { resolve(JSON.parse(xhr.responseText) as Attachment); } catch { reject(new BackendError(this.t('Invalid upload reply', '上传回复无效'), xhr.status)); }
          return;
        }
        void this.client.error(response).then(reject);
      };
      xhr.onerror = () => reject(new BackendError(this.t('Could not reach the host', '无法连接宿主'), 0));
      xhr.onabort = () => reject(abortError());
      opts?.signal?.addEventListener('abort', () => xhr.abort(), { once: true });
      const form = new FormData();
      form.append('file', file, file.name);
      xhr.send(form);
    });
  }

  private async uploadByFetch(path: string, file: File, opts?: UploadOptions): Promise<Attachment> {
    const form = new FormData();
    form.append('file', file, file.name);
    const fetchImpl = this.o.fetch ?? globalThis.fetch;
    let response: Response;
    try {
      response = await fetchImpl(this.client.url(path), { method: 'POST', headers: this.client.authHeaders(), body: form, cache: 'no-store', signal: opts?.signal });
    } catch (err) {
      if (opts?.signal?.aborted) throw err;
      throw new BackendError(this.t('Could not reach the host', '无法连接宿主'), 0);
    }
    if (!response.ok) throw await this.client.error(response);
    opts?.onProgress?.(1);
    return await response.json() as Attachment;
  }

  assetUrl(url: string): Promise<string> {
    if (!url.startsWith('/')) return Promise.resolve(url);
    let p = this.assets.get(url);
    if (!p) {
      p = this.client.raw(url).then(r => r.blob()).then(b => URL.createObjectURL(b));
      p.catch(() => this.assets.delete(url));
      this.assets.set(url, p);
      // Bounded: the oldest object URLs go first.
      while (this.assets.size > 200) {
        const [k, v] = this.assets.entries().next().value!;
        this.assets.delete(k);
        void v.then(revoke, () => {});
      }
    }
    return p;
  }

  private queuePath(id: string, messageId: string) { return `${this.session(id)}/queue/${enc(messageId)}`; }
  async editQueued(id: string, messageId: string, text: string) { await this.call('PATCH', this.queuePath(id, messageId), { text }); this.refresh(id); }
  async removeQueued(id: string, messageId: string) { await this.call('DELETE', this.queuePath(id, messageId)); this.refresh(id); }
  async sendQueuedNow(id: string, messageId: string) { await this.call('POST', `${this.queuePath(id, messageId)}/send`); this.refresh(id); }

  // ---------- requests ----------
  async answer(requestId: string, a: RequestAnswer) {
    const { decision, answers, mode, message } = a;
    await this.call('POST', `/api/requests/${enc(requestId)}`, defined({ decision, answers, mode, message: message || undefined }));
    this.refresh();
  }

  // ---------- details ----------
  async tasks(id: string) {
    const list = await this.get<TaskInfo[]>(`${this.session(id)}/tasks`);
    return Array.isArray(list) ? list : [];
  }
  stats(id: string) { return this.get<SessionStats>(`${this.session(id)}/stats`); }

  private fileQuery(path: string, base?: string[]) {
    const q = new URLSearchParams({ path });
    for (const b of base ?? []) q.append('base', b);
    return q.toString();
  }

  async readFile(id: string, path: string, base?: string[]): Promise<FileContent> {
    const response = await this.client.raw(`${this.session(id)}/file?${this.fileQuery(path, base)}`, { okStatuses: [413] });
    if (response.status === 413) return { path, mime: '', size: 0, tooLarge: true };
    let shown = path;
    try { shown = decodeURIComponent(response.headers.get('X-CC-Path') || '') || path; } catch { /* keep the requested path */ }
    const mime = (response.headers.get('Content-Type') || 'application/octet-stream').split(';')[0].trim().toLowerCase();
    const blob = await response.blob();
    const svg = mime === 'image/svg+xml' || /\.svg$/i.test(shown);
    // SVG stays text: it is shown through a data: URL, never as a blob: document.
    if (svg) return { path: shown, mime: 'image/svg+xml', size: blob.size, text: await blob.text() };
    if (mime.startsWith('image/') || mime === 'application/pdf') return { path: shown, mime, size: blob.size, url: URL.createObjectURL(blob) };
    const bytes = new Uint8Array(await blob.arrayBuffer());
    // A NUL byte early on marks a binary file: offered as a download.
    if (bytes.subarray(0, 8000).includes(0)) return { path: shown, mime, size: blob.size, url: URL.createObjectURL(blob) };
    return { path: shown, mime, size: blob.size, text: new TextDecoder().decode(bytes) };
  }

  async fileLink(id: string, path: string, base?: string[]) {
    const r = await this.call<{ url: string; name: string; size: number }>('POST', `${this.session(id)}/file/link?${this.fileQuery(path, base)}`);
    // The ticket in the path is the credential: the browser downloads it without the token.
    return { ...r, url: this.client.url(r.url) };
  }

  // ---------- native history ----------
  async nativeSessions(provider: Provider) {
    const list = await this.get<NativeSession[]>(providerQuery('/api/native/sessions', provider));
    return Array.isArray(list) ? list : [];
  }

  async importNative(id: string, provider: Provider, profileId?: string): Promise<Session> {
    const s = await this.call<Session>('POST', providerQuery(`/api/native/sessions/${enc(id)}/import`, provider), defined({ confirmStopped: true, profileId }));
    this.refresh();
    return s;
  }

  // ---------- terminal ----------
  openTerminal(id: string, handlers: TerminalHandlers): TerminalStream {
    const Socket = this.o.WebSocket ?? (globalThis.WebSocket as unknown as WebSocketCtor | undefined);
    if (!Socket) throw new BackendError(this.t('This browser has no WebSocket', '此浏览器不支持 WebSocket'), 0);
    const streams = this.streams;
    const wrapped: TerminalHandlers = {
      onData: c => handlers.onData(c),
      onSize: s => handlers.onSize(s),
      onState: (state, detail, opts) => {
        if (state === 'exited' || state === 'failed') streams.delete(stream);
        handlers.onState(state, detail, opts);
      },
    };
    const inner = new HttpTerminalStream(this.client, id, wrapped, Socket);
    const stream: TerminalStream = {
      send: d => inner.send(d),
      resize: (c, r) => inner.resize(c, r),
      redraw: () => inner.redraw(),
      close: () => { streams.delete(stream); inner.close(); },
    };
    streams.add(stream);
    return stream;
  }

  // ---------- configuration ----------
  models(opts: { provider: Provider; sessionId?: string; refresh?: boolean }) {
    const q = new URLSearchParams();
    if (opts.sessionId) q.set('sessionId', opts.sessionId);
    if (opts.refresh) q.set('refresh', '1');
    return this.get<ModelList>(providerQuery(withQuery('/api/models', q.toString()), opts.provider));
  }

  profiles() { return this.get<ProfileCatalog>('/api/profiles'); }

  async setDefaultProfile(app: Provider, profileId: string) {
    await this.call('PUT', '/api/profiles/default', { app, profileId });
  }

  preferences(provider: Provider) { return this.get<Preferences>(providerQuery('/api/preferences', provider)); }

  savePreferences(provider: Provider, p: Preferences & { confirmDangerous?: boolean }) {
    const { permissionMode, model, effort, revision, confirmDangerous } = p;
    return this.call<Preferences>('PUT', providerQuery('/api/preferences', provider), defined({ permissionMode, model, effort, revision, confirmDangerous }));
  }

  async claudeSettings(): Promise<ClaudeSettings> {
    return withPath(await this.get<ClaudeSettings>('/api/claude/settings'));
  }

  async saveClaudeSettings(v: Omit<ClaudeSettings, 'path'> & { confirmDangerous?: boolean }): Promise<ClaudeSettings> {
    const { model, effortLevel, permissionMode, revision, confirmDangerous } = v;
    return withPath(await this.call<ClaudeSettings>('PUT', '/api/claude/settings', defined({ model, effortLevel, permissionMode, revision, confirmDangerous })));
  }

  async identity() { return withIdentity(await this.get<HostIdentity>('/api/identity')); }

  async saveIdentity(v: { name: string; color: string; icon: string }) {
    return withIdentity(await this.call<HostIdentity>('PUT', '/api/identity', { name: v.name, color: v.color, icon: v.icon }));
  }

  async oauthProfiles() {
    const r = await this.get<{ profiles?: { id: string; name: string }[] }>('/api/profiles/oauth');
    return (r?.profiles ?? []).map(p => ({ id: p.id, name: p.name }));
  }
  // The list names entries "oauth:<id>"; the paths take the bare id.
  private oauthPath(id: string) { return `/api/profiles/oauth/${enc(id.replace(/^oauth:/, ''))}`; }
  async addOAuthProfile(name: string, token: string) { await this.call('POST', '/api/profiles/oauth', { name, token }); }
  async replaceOAuthProfile(id: string, token: string) { await this.call('PATCH', this.oauthPath(id), { token }); }
  async removeOAuthProfile(id: string) { await this.call('DELETE', this.oauthPath(id)); }

  // ---------- usage and status ----------
  watchUsage(opts: { provider: Provider; sessionId?: string }, onChange: (u: UsageReply) => void): Unsubscribe {
    const path = providerQuery(opts.sessionId ? `/api/usage?sessionId=${enc(opts.sessionId)}` : '/api/usage', opts.provider);
    let closed = false;
    let last: UsageReply | undefined;
    let shown: UsageReply | undefined;
    const deliver = (u: UsageReply) => { if (!closed && u !== shown) { shown = u; onChange(u); } };
    const poller = poll(async () => {
      let usage: UsageReply;
      try {
        usage = await this.get<UsageReply>(path);
      } catch (e) {
        if (closed) return;
        // Keep showing the last reading, marked out of date.
        deliver({ ...(last ?? { available: true }), stale: true, error: 'request-failed' });
        throw e;
      }
      if (closed) return;
      last = usage;
      deliver(usage);
    }, this.intervals.usage, { pauseHidden: true });
    this.usagePollers.add(poller);
    return () => { closed = true; poller.stop(); this.usagePollers.delete(poller); };
  }

  usageHistory(hours: number) { return this.get<UsageHistory>(`/api/usage/history?hours=${hours}`); }

  status() { return this.get<HostStatus>('/api/status'); }

  statusSummary() { return this.get<HostStatus>('/api/status/summary'); }
}

function asBackendError(e: unknown): BackendError {
  return e instanceof BackendError ? e : new BackendError(e instanceof Error ? e.message : String(e), 0);
}

function abortError() {
  return typeof DOMException !== 'undefined' ? new DOMException('Upload cancelled', 'AbortError') : Object.assign(new Error('Upload cancelled'), { name: 'AbortError' });
}

function revoke(url: string) { if (url.startsWith('blob:')) URL.revokeObjectURL(url); }

/** path is left out when the host does not know it. */
function withPath(v: ClaudeSettings): ClaudeSettings { return { ...v, path: v.path ?? '' }; }

/** Unset name, colour and icon are left out of the host's reply. */
function withIdentity(v: HostIdentity): HostIdentity {
  return { ...v, name: v.name ?? '', color: v.color ?? '', icon: v.icon ?? '', palette: v.palette ?? [] };
}

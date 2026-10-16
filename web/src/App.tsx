import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type FormEvent } from 'react';
import Markdown from './Markdown';
import { ClaudeSettings } from './ClaudeSettings';
import { StatusPanel } from './StatusPanel';
import { AttachButton, AttachmentList, uploadFiles, type Attachment } from './Attachments';
import { copyText } from './clipboard';
import { DeleteSessionDialog, SessionChips, SessionSettings, type Defaults } from './SessionSettings';
import { SidebarResizer, useSidebarWidth } from './SidebarResizer';
import { ModelPicker, confirmMode, dangerousMode, effortOptions, permissionOptions } from './ModelPicker';
import { Select } from './Select';
import { CloseIcon, CollapseAllIcon, ExpandAllIcon, GearIcon, LogoutIcon, MenuIcon, PlusIcon, RefreshIcon, StatusIcon, TerminalIcon, TrashIcon } from './icons';
import { ToolGroup } from './ToolGroup';
import { QuestionCard, type QuestionDraft } from './QuestionCard';
import { compactSummary, groupTranscript, isCompacting, linkOutcomes, linkRequests, requestCall, requestSettled, stabilize, type ToolCall, type TranscriptItem } from './toolGroups';
import { MAX_UI_CHARS, UI_KEY, UI_PREFIX, VIEW_KEY, deserialize, draftEmpty, emptyUI, forget, formatHash, mergeUI, parseHash, parseKey, parseViews, serialize, serializeViews, type Route, type SessionDraft, type UIState, type ViewState } from './persist';

type Session = { id: string; title: string; cwd: string; status?: string; createdAt?: string; updatedAt: string; mode?: string; online?: boolean; claudeSessionId?: string; permissionMode?: string; model?: string; effort?: string; queued?: { text: string; payload?: any }[] };
type Event = { seq: number; kind: string; text?: string; payload?: any; createdAt: string; truncated?: boolean };
type Request = { id: string; kind: string; toolName: string; input: any; status: string; createdAt?: string };
type Info = { version?: string; cwd: string; claudeAvailable: boolean; claudeVersion: string; terminalCommand?: string; channelExperimental?: boolean };
type ConversationTab = { key: string; id: string; native: boolean; session?: Session; events: Event[]; requests: Request[]; draft: string; attachments: Attachment[]; notice?: string; scrollTop: number; nearBottom: boolean; loaded: boolean; hasOlder: boolean; loadingOlder: boolean; expandedTools: number[]; expandedGroups: Record<number, boolean>; answers: Record<string, Record<string, string>>; questionDrafts: Record<string, QuestionDraft> };
type Command = { name: string; description: string; terminalOnly?: boolean };
const SEND_KEYS = [{ value: 'ctrl-enter', label: 'Ctrl / ⌘ + Enter', description: 'Enter 换行，组合键发送' }, { value: 'enter', label: 'Enter 发送', description: 'Shift + Enter 换行' }];
const transcriptPath = (tab: { id: string; native: boolean }) => `/api/${tab.native ? 'native/sessions' : 'sessions'}/${encodeURIComponent(tab.id)}`;
const tabKey = (id: string, native: boolean) => `${native ? 'native' : 'session'}:${id}`;
const statusLabel = (status?: string) => ({ running: '执行中', waiting: '等待回答', idle: '就绪', completed: '已完成', interrupted: '已停止', failed: '执行失败', stopped: '已停止', offline: '离线', error: '执行失败' }[status || ''] || status || '历史');
const sessionLabel = (session?: Session) => session?.mode === 'channel' && !session.online ? '终端离线' : ['running', 'waiting'].includes(session?.status || '') ? statusLabel(session?.status) : session?.mode === 'channel' ? '终端在线' : statusLabel(session?.status || 'idle');

function blankTab(key: string, id: string, native: boolean, session?: Session, saved?: SessionDraft, view?: ViewState): ConversationTab {
  return { key, id, native, session, events: [], requests: [], draft: saved?.draft || '', attachments: saved?.attachments || [], scrollTop: view?.scrollTop || 0, nearBottom: view?.nearBottom ?? true, loaded: false, hasOlder: false, loadingOlder: false, expandedTools: view?.expandedTools || [], expandedGroups: view?.expandedGroups || {}, answers: saved?.answers || {}, questionDrafts: saved?.questionDrafts || {} };
}
const pickDraft = (tab: Pick<ConversationTab, 'draft' | 'attachments' | 'questionDrafts' | 'answers'>) => ({ draft: tab.draft, attachments: tab.attachments, questionDrafts: tab.questionDrafts, answers: tab.answers });
const cleanURL = () => location.pathname + location.search;
// Storage can be missing or throw (private mode, quota, blocked site data); UI state is a convenience.
function loadUI(): UIState { try { return deserialize(localStorage.getItem(UI_KEY), Date.now()); } catch { return emptyUI(); } }
function saveUI(state: UIState) { for (const max of [MAX_UI_CHARS, MAX_UI_CHARS / 8]) { try { localStorage.setItem(UI_KEY, serialize(state, Date.now(), max)); return; } catch {} } }
function loadViews(): Record<string, ViewState> { try { return parseViews(sessionStorage.getItem(VIEW_KEY)); } catch { return {}; } }
function saveViews(text: string) { try { sessionStorage.setItem(VIEW_KEY, text); } catch {} }
// Transcripts load from the end, PAGE events at a time; older pages load on scroll-up.
const PAGE = 200;
// Session lists render LIST_RENDER rows at a time and keep the newest LIST_CACHE in storage,
// so the list shows at once after a reload and refreshes in the background.
const LIST_RENDER = 200, LIST_CACHE = 300;
const listKey = (native: boolean) => `${UI_PREFIX}-list-${native ? 'native' : 'session'}`;
function loadList(native: boolean): Session[] { try { const items = JSON.parse(localStorage.getItem(listKey(native)) || '[]'); return Array.isArray(items) ? items : []; } catch { return []; } }
function saveList(native: boolean, items: Session[]) { try { localStorage.setItem(listKey(native), JSON.stringify(items.slice(0, LIST_CACHE))); } catch {} }
function clearUI() { for (const name of ['localStorage', 'sessionStorage'] as const) { try { const storage = window[name]; for (const key of Object.keys(storage)) if (key.startsWith(UI_PREFIX)) storage.removeItem(key); } catch {} } }

const NO_CALLS: number[] = [];
const NO_DRAFT: QuestionDraft = {};
type SharedQuestionProps = { request: Request; call?: ToolCall; busy: boolean; draft?: QuestionDraft; onAnswer: (request: Request, body: object) => void; onDraft: (id: string, update: (old: QuestionDraft) => QuestionDraft) => void };

/**
 * Interactive card for a shared-terminal question; "交给终端" leaves it to the native dialog.
 * Its unsent input lives in the tab (by request id), so moving between the transcript and
 * the pending list, or switching sessions, keeps it. A request's content never changes, and
 * polling recreates its object, so it is compared by id.
 */
const SharedQuestion = memo(function SharedQuestion({ request, call, busy, draft, onAnswer, onDraft }: SharedQuestionProps) {
  const shown = useMemo(() => call ? (request.kind === 'plan' ? { ...call, input: request.input } : call) : requestCall(request), [call, request.id]);
  const setDraft = useCallback((update: (old: QuestionDraft) => QuestionDraft) => onDraft(request.id, update), [onDraft, request.id]);
  return <QuestionCard call={shown} waitingHint="等待回答（网页或终端均可）" busy={busy} draft={draft || NO_DRAFT} setDraft={setDraft} onAnswer={answers => onAnswer(request, { decision: 'allow', answers })} onDecide={decision => onAnswer(request, decision)} onHandoff={() => onAnswer(request, { decision: 'terminal' })} />;
}, (a, b) => a.request.id === b.request.id && a.call === b.call && a.busy === b.busy && a.draft === b.draft && a.onAnswer === b.onAnswer && a.onDraft === b.onDraft);

function messages(events: Event[]) {
  const items: Event[] = [];
  let stream: Event | undefined;
  for (const event of events) {
    if (event.kind === 'assistant_delta') {
      if (!stream) { stream = { ...event, kind: 'assistant', text: '' }; items.push(stream); }
      stream.text = (stream.text || '') + (event.text || '');
    } else if (event.kind === 'assistant') {
      if (stream) { stream.text = event.text; stream = undefined; }
      else items.push(event);
    } else {
      if (['user', 'tool', 'tool_result', 'error', 'compact'].includes(event.kind)) items.push(event);
      if (['user', 'result', 'error', 'compact'].includes(event.kind)) stream = undefined;
    }
  }
  return items;
}

export default function App() {
  const [token, setToken] = useState(() => localStorage.getItem('cc-token') || sessionStorage.getItem('cc-token') || '');
  const [credential, setCredential] = useState('');
  const [connected, setConnected] = useState(false);
  const [info, setInfo] = useState<Info>();
  const [sessions, setSessions] = useState<Session[]>([]);
  const [search, setSearch] = useState('');
  const [native, setNative] = useState(false);
  const [selected, setSelected] = useState('');
  const listView = useRef(native);
  listView.current = native;
  const [tabs, setTabs] = useState<ConversationTab[]>([]);
  const tabsRef = useRef(tabs);
  tabsRef.current = tabs;
  const activeKey = tabKey(selected, native);
  const activeTab = tabs.find(tab => tab.key === activeKey);
  const session = activeTab?.session;
  const events = activeTab?.events || [];
  const requests = activeTab?.requests || [];
  const draft = activeTab?.draft || '';
  const loading = !!selected && !activeTab?.loaded;
  const updateTab = useCallback((key: string, update: (tab: ConversationTab) => ConversationTab) => { setTabs(old => old.map(tab => tab.key === key ? update(tab) : tab)); }, []);
  const setDraft = (value: string) => updateTab(activeKey, tab => ({ ...tab, draft: value }));
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => localStorage.getItem('cc-sidebar-collapsed') === 'true');
  const [sendKey, setSendKey] = useState(() => localStorage.getItem('cc-send-key') || 'ctrl-enter');
  const composing = useRef(false);
  const [drawer, setDrawer] = useState(false);
  const [create, setCreate] = useState(false);
  const [title, setTitle] = useState('');
  const [cwd, setCwd] = useState('');
  const [newOverrides, setNewOverrides] = useState({ permissionMode: '', model: '', effort: '' });
  const [importing, setImporting] = useState(false);
  const [stopped, setStopped] = useState(false);
  const [sharing, setSharing] = useState(false);
  const [copyStatus, setCopyStatus] = useState<'idle' | 'copied' | 'failed'>('idle');
  const [copying, setCopying] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [statusOpen, setStatusOpen] = useState(false);
  const [sessionSettingsOpen, setSessionSettingsOpen] = useState(false);
  const [deleting, setDeleting] = useState<Session>();
  const [defaults, setDefaults] = useState<Defaults>();
  const [dragging, setDragging] = useState(false);
  const sidebar = useSidebarWidth();
  // The bearer token travels in clear text when this page is served over plain http from another machine.
  const [plainHttp, setPlainHttp] = useState(() => {
    const host = location.hostname.replace(/^\[|\]$/g, '');
    const local = host === 'localhost' || host === '::1' || /^127\./.test(host);
    try { if (sessionStorage.getItem('cc-plain-http-dismissed') === '1') return false; } catch {}
    return location.protocol === 'http:' && !local;
  });
  const dismissPlainHttp = () => { setPlainHttp(false); try { sessionStorage.setItem('cc-plain-http-dismissed', '1'); } catch {} };
  const [uploading, setUploading] = useState(false);
  const [commands, setCommands] = useState<Command[]>([]);
  const attachments = activeTab?.attachments || [];
  const bottom = useRef<HTMLDivElement>(null);
  const nearBottom = useRef(true);
  const scroll = useRef<HTMLDivElement>(null);
  const selectedRef = useRef(activeKey);
  selectedRef.current = activeKey;
  // Polls revalidate with the last ETag: an unchanged reply (304) returns the earlier object,
  // so nothing re-renders and nothing is transferred. Cursor queries change every time; skip them.
  const etags = useRef(new Map<string, { tag: string; data: unknown }>());
  const api = useCallback(async <T,>(path: string, options?: RequestInit): Promise<T> => {
    const cacheable = (options?.method || 'GET').toUpperCase() === 'GET' && !/[?&](after|before)=/.test(path);
    const cached = cacheable ? etags.current.get(path) : undefined;
    const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}`, ...(cached ? { 'If-None-Match': cached.tag } : {}), ...options?.headers }, cache: 'no-store' });
    if (response.status === 304 && cached) return cached.data as T;
    if (!response.ok) {
      let message = `请求失败 (${response.status})`;
      try { const body = await response.json(); message = body.error || body.message || message; } catch {}
      if (response.status === 401) { setConnected(false); localStorage.removeItem('cc-token'); sessionStorage.removeItem('cc-token'); for (const view of [false, true]) try { localStorage.removeItem(listKey(view)); } catch {} }
      throw Object.assign(new Error(message), { status: response.status });
    }
    if (response.status === 204) return undefined as T;
    const data = await response.json();
    const tag = response.headers.get('ETag');
    if (cacheable && tag) {
      etags.current.delete(path); etags.current.set(path, { tag, data });
      if (etags.current.size > 64) etags.current.delete(etags.current.keys().next().value!);
    }
    return data;
  }, [token]);
  useEffect(() => { etags.current.clear(); }, [token]);
  const list = useCallback(async () => {
    const result = await api<Session[]>(native ? '/api/native/sessions' : '/api/sessions');
    if (listView.current === native) setSessions(result);
    setTabs(old => old.map(tab => tab.native === native ? { ...tab, session: result.find(item => item.id === tab.id) || tab.session } : tab));
  }, [api, native]);
  useEffect(() => {
    if (!token) return;
    let active = true;
    api<Info>('/api/info').then(data => { if (active) { setInfo(data); setConnected(true); setCwd(data.cwd); setError(''); localStorage.setItem('cc-token', token); sessionStorage.removeItem('cc-token'); } }).catch(err => { if (active) setError(err.message); });
    return () => { active = false; };
  }, [api, token]);
  useEffect(() => {
    if (!connected) return;
    let active = true;
    let refreshing = false;
    setSessions(loadList(native));
    const refresh = async () => {
      if (document.hidden || refreshing) return;
      refreshing = true;
      const views = native ? [false, true] : [false];
      const results = await Promise.allSettled(views.map(async view => ({ view, data: await api<Session[]>(view ? '/api/native/sessions' : '/api/sessions') })));
      refreshing = false;
      if (!active) return;
      for (const result of results) {
        if (result.status === 'rejected') { setError((result.reason as Error).message); continue; }
        const { view, data } = result.value;
        if (view === native) setSessions(data);
        if (savedLists.current[String(view)] !== data) { savedLists.current[String(view)] = data; saveList(view, data); }
        setTabs(old => old.map(tab => tab.native === view ? { ...tab, session: data.find(item => item.id === tab.id) || tab.session } : tab));
      }
    };
    refresh(); const timer = window.setInterval(refresh, 3000);
    return () => { active = false; clearInterval(timer); };
  }, [connected, api, native]);
  const savedLists = useRef<Record<string, Session[]>>({});
  const openKeys = tabs.map(tab => tab.key).join('|');
  useEffect(() => {
    if (!connected || !openKeys) return;
    let active = true;
    const inFlight = new Set<string>();
    const refreshTab = async (snapshot: ConversationTab) => {
      if (inFlight.has(snapshot.key)) return;
      inFlight.add(snapshot.key);
      try {
        let nextSession: Session | undefined;
        let nextEvents: Event[] | undefined;
        let hasOlder: boolean | undefined;
        const base = transcriptPath(snapshot);
        if (!snapshot.loaded) {
          const data = await api<{ session: Session; events: Event[]; hasMore?: boolean }>(`${base}?tail=${PAGE}&compact=1`);
          nextSession = data.session;
          nextEvents = data.events;
          hasOlder = !!data.hasMore;
        } else {
          nextEvents = await api<Event[]>(`${base}/events?after=${snapshot.events.at(-1)?.seq || 0}&compact=1`);
        }
        const pending = snapshot.native ? [] : await api<Request[]>(`/api/requests?sessionId=${encodeURIComponent(snapshot.id)}`);
        if (!active) return;
        updateTab(snapshot.key, tab => ({ ...tab, session: nextSession || tab.session, loaded: true, hasOlder: hasOlder ?? tab.hasOlder,
          events: !snapshot.loaded ? nextEvents! : nextEvents!.length ? [...tab.events, ...nextEvents!.filter(event => event.seq > (tab.events.at(-1)?.seq || 0))] : tab.events,
          requests: pending.filter(request => request.status === 'pending') }));
      } catch (err) {
        // 409: a native transcript was rewritten and renumbered; load it again from the end.
        if (active && (err as { status?: number }).status === 409) updateTab(snapshot.key, tab => ({ ...tab, loaded: false, events: [], hasOlder: false }));
        else if (active && selectedRef.current === snapshot.key) setError((err as Error).message);
      }
      finally { inFlight.delete(snapshot.key); }
    };
    const refresh = () => { if (!document.hidden) for (const tab of tabsRef.current) void refreshTab(tab); };
    refresh();
    const timer = window.setInterval(refresh, 1500);
    document.addEventListener('visibilitychange', refresh);
    return () => { active = false; clearInterval(timer); document.removeEventListener('visibilitychange', refresh); };
  }, [openKeys, connected, api, updateTab]);
  // Refresh persistence: the hash routes (#/s/<id>, #/native/<id>), localStorage keeps the
  // opened tabs and per-session drafts, sessionStorage keeps scroll and tool expansion.
  const [restored, setRestored] = useState(false);
  const [restoreTry, setRestoreTry] = useState(0);
  const [routeNotice, setRouteNotice] = useState('');
  const savedDrafts = useRef(new Map<string, string>());
  const draftChanges = useRef<Record<string, SessionDraft | null>>({});
  const savedLayout = useRef(''); const pendingLayout = useRef<Pick<UIState, 'open' | 'selected' | 'sidebarTab'> | undefined>(undefined);
  const savedViews = useRef(''); const pendingViews = useRef('');
  const persistTimer = useRef(0);
  const replaceRoute = useRef(true);
  const flush = useCallback(() => {
    clearTimeout(persistTimer.current); persistTimer.current = 0;
    if (pendingLayout.current || Object.keys(draftChanges.current).length) saveUI(mergeUI(loadUI(), pendingLayout.current, draftChanges.current));
    if (pendingViews.current) saveViews(pendingViews.current);
    pendingLayout.current = undefined; draftChanges.current = {}; pendingViews.current = '';
  }, []);
  const forgetDraft = (id: string) => { draftChanges.current[id] = null; savedDrafts.current.delete(id); };
  useEffect(() => {
    if (!connected || restored) return;
    let active = true;
    const stored = loadUI(); const views = loadViews();
    const hash = location.hash === '#' ? '' : location.hash;
    const target = hash ? parseHash(hash) : parseKey(stored.selected);
    const wanted = [...stored.open.map(parseKey), target].filter((route): route is Route => !!route);
    Promise.all([api<Session[]>('/api/sessions'), wanted.some(route => route.native) ? api<Session[]>('/api/native/sessions').catch(() => undefined) : undefined]).then(([managed, nativeList]) => {
      if (!active) return;
      // null: known to be gone; undefined: the list could not be read, keep it.
      const lookup = (route: Route) => route.native ? nativeList && (nativeList.find(item => item.id === route.id) || null) : managed.find(item => item.id === route.id) || null;
      let state = stored;
      for (const id of Object.keys(state.drafts)) if (!managed.some(item => item.id === id)) state = forget(state, { id, native: false });
      const restoredTabs: ConversationTab[] = [];
      for (const route of wanted) {
        const key = tabKey(route.id, route.native); const found = lookup(route);
        if (found === null) { state = forget(state, route); continue; }
        if (!restoredTabs.some(tab => tab.key === key)) restoredTabs.push(blankTab(key, route.id, route.native, found, route.native ? undefined : state.drafts[route.id], views[key]));
      }
      if (state !== stored) saveUI(state);
      if (hash && (!target || lookup(target) === null)) setRouteNotice('会话不存在或已删除');
      for (const tab of restoredTabs) if (!tab.native) savedDrafts.current.set(tab.id, JSON.stringify(pickDraft(tab)));
      const fresh = !tabsRef.current.length;
      setTabs(old => [...restoredTabs, ...old.filter(tab => !restoredTabs.some(item => item.key === tab.key))]);
      if (fresh) {
        const open = !!target && restoredTabs.some(tab => tab.key === tabKey(target.id, target.native));
        setNative(open ? target!.native : state.sidebarTab === 'native'); setSelected(open ? target!.id : '');
      }
      replaceRoute.current = true; setRestored(true);
    }).catch(err => { if (active) { setError((err as Error).message); window.setTimeout(() => setRestoreTry(n => n + 1), 3000); } });
    return () => { active = false; };
  }, [connected, restored, restoreTry, api]);
  useEffect(() => {
    if (!restored) return;
    for (const tab of tabs) {
      if (tab.native) continue;
      const value = pickDraft(tab); const text = JSON.stringify(value);
      if (savedDrafts.current.get(tab.id) === text) continue;
      savedDrafts.current.set(tab.id, text);
      draftChanges.current[tab.id] = draftEmpty(value) ? null : { ...value, updatedAt: Date.now() };
    }
    const layout = { open: tabs.map(tab => tab.key), selected: selected ? tabKey(selected, native) : '', sidebarTab: native ? 'native' as const : 'session' as const };
    const layoutText = JSON.stringify(layout);
    if (layoutText !== savedLayout.current) { savedLayout.current = layoutText; pendingLayout.current = layout; }
    const viewsText = serializeViews(Object.fromEntries(tabs.map(tab => [tab.key, { scrollTop: tab.scrollTop, nearBottom: tab.nearBottom, expandedTools: tab.expandedTools, expandedGroups: tab.expandedGroups }])));
    if (viewsText !== savedViews.current) { savedViews.current = viewsText; pendingViews.current = viewsText; }
    if (!persistTimer.current && (pendingLayout.current || pendingViews.current || Object.keys(draftChanges.current).length)) persistTimer.current = window.setTimeout(flush, 300);
  }, [restored, tabs, selected, native, flush]);
  useEffect(() => {
    const hidden = () => { if (document.visibilityState === 'hidden') flush(); };
    window.addEventListener('pagehide', flush); document.addEventListener('visibilitychange', hidden);
    return () => { window.removeEventListener('pagehide', flush); document.removeEventListener('visibilitychange', hidden); };
  }, [flush]);
  // Another browser tab wrote drafts: adopt them for open sessions without unsaved edits here,
  // so a later write from this page never brings back stale text. Selection is not synced.
  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key !== UI_KEY || !event.newValue) return;
      const stored = deserialize(event.newValue, Date.now());
      for (const tab of tabsRef.current) {
        if (tab.native || tab.id in draftChanges.current) continue;
        const mine = JSON.stringify(pickDraft(tab));
        if (savedDrafts.current.get(tab.id) !== mine) continue;
        const theirs = pickDraft(stored.drafts[tab.id] || { draft: '', attachments: [], questionDrafts: {}, answers: {} });
        const text = JSON.stringify(theirs);
        if (text === mine) continue;
        savedDrafts.current.set(tab.id, text);
        updateTab(tab.key, old => ({ ...old, ...theirs }));
      }
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, [updateTab]);
  useEffect(() => {
    if (!restored) return;
    const next = formatHash(selected, native);
    if ((location.hash === '#' ? '' : location.hash) !== next) {
      if (replaceRoute.current) history.replaceState(null, '', cleanURL() + next); else history.pushState(null, '', cleanURL() + next);
    }
    replaceRoute.current = false;
  }, [restored, selected, native]);
  const routeTo = useRef(() => {});
  routeTo.current = () => {
    if (!restored) return;
    const route = parseHash(location.hash);
    if (!route) { if (selected) setSelected(''); return; }
    if (route.id === selected && route.native === native) return;
    if (tabs.some(tab => tab.key === tabKey(route.id, route.native))) { choose(route.id, route.native); return; }
    api<Session[]>(route.native ? '/api/native/sessions' : '/api/sessions').then(items => {
      const found = items.find(item => item.id === route.id);
      if (found) { choose(route.id, route.native, found); return; }
      setRouteNotice('会话不存在或已删除');
      if (!route.native) forgetDraft(route.id);
      setTabs(old => old.filter(tab => tab.key !== tabKey(route.id, route.native)));
      setSelected(''); history.replaceState(null, '', cleanURL());
    }).catch(err => setError((err as Error).message));
  };
  useEffect(() => {
    const handler = () => routeTo.current();
    window.addEventListener('hashchange', handler); window.addEventListener('popstate', handler);
    return () => { window.removeEventListener('hashchange', handler); window.removeEventListener('popstate', handler); };
  }, []);
  useLayoutEffect(() => {
    const element = scroll.current;
    if (!element || !activeTab) return;
    nearBottom.current = activeTab.nearBottom;
    element.scrollTop = activeTab.nearBottom ? element.scrollHeight : activeTab.scrollTop;
  }, [activeKey, loading]);
  // Older pages: a marker above the transcript loads the previous page when it nears the
  // viewport; the reading position is kept by shifting scrollTop by the added height.
  const olderMarker = useRef<HTMLDivElement>(null);
  const olderInFlight = useRef(new Set<string>());
  const prepended = useRef<{ key: string; height: number; top: number } | undefined>(undefined);
  const loadOlder = useCallback(async () => {
    const key = selectedRef.current;
    const tab = tabsRef.current.find(item => item.key === key);
    if (!tab || !tab.loaded || !tab.hasOlder || !tab.events.length || olderInFlight.current.has(key)) return;
    olderInFlight.current.add(key);
    updateTab(key, old => ({ ...old, loadingOlder: true }));
    const before = tab.events[0].seq;
    try {
      const data = await api<{ events: Event[]; hasMore: boolean }>(`${transcriptPath(tab)}/events?before=${before}&limit=${PAGE}&compact=1`);
      const element = scroll.current;
      // The tab may have been closed and reopened (reloaded) meanwhile: a page that no longer joins its start is dropped.
      if (tabsRef.current.find(item => item.key === key)?.events[0]?.seq !== before) return;
      if (element && selectedRef.current === key) prepended.current = { key, height: element.scrollHeight, top: element.scrollTop };
      updateTab(key, old => old.events[0]?.seq !== before ? { ...old, loadingOlder: false } : { ...old, loadingOlder: false, hasOlder: data.hasMore, events: [...data.events, ...old.events] });
    } catch (err) {
      updateTab(key, old => ({ ...old, loadingOlder: false }));
      if (selectedRef.current === key) setError((err as Error).message);
    } finally { olderInFlight.current.delete(key); }
  }, [api, updateTab]);
  useLayoutEffect(() => {
    const anchor = prepended.current; const element = scroll.current;
    if (!anchor || !element || anchor.key !== activeKey) return;
    prepended.current = undefined;
    const behavior = element.style.scrollBehavior;
    element.style.scrollBehavior = 'auto'; // smooth scrolling would animate the jump
    element.scrollTop = anchor.top + element.scrollHeight - anchor.height;
    element.style.scrollBehavior = behavior;
  }, [events]);
  const hasOlder = !!activeTab?.hasOlder;
  const firstSeq = events[0]?.seq;
  useEffect(() => {
    const root = scroll.current; const target = olderMarker.current;
    if (!root || !target || !hasOlder || loading) return;
    // Recreated per page: a new observer reports at once, so a marker still in view loads the next page.
    const observer = new IntersectionObserver(entries => { if (entries.some(entry => entry.isIntersecting)) void loadOlder(); }, { root, rootMargin: '800px 0px 0px 0px' });
    observer.observe(target);
    return () => observer.disconnect();
  }, [activeKey, hasOlder, loading, firstSeq, loadOlder]);
  // Opened tool calls whose text the listing cut fetch their events whole.
  const fullInFlight = useRef(new Set<string>());
  const loadFull = useCallback((call: ToolCall) => {
    const key = activeKey; const tab = tabsRef.current.find(item => item.key === key);
    if (!tab) return;
    for (const seq of call.truncated || []) {
      const id = `${key}#${seq}`;
      if (fullInFlight.current.has(id)) continue;
      fullInFlight.current.add(id);
      api<Event>(`${transcriptPath(tab)}/events/${seq}`)
        .then(full => updateTab(key, old => ({ ...old, events: old.events.map(event => event.seq === full.seq ? full : event) })))
        .catch(err => { if (selectedRef.current === key) setError((err as Error).message); })
        .finally(() => fullInFlight.current.delete(id));
    }
  }, [activeKey, api, updateTab]);
  useEffect(() => {
    if (nearBottom.current && scroll.current) scroll.current.scrollTop = scroll.current.scrollHeight;
  }, [events, requests]);
  const current = sessions.find(item => item.id === selected) || session;
  const [listLimit, setListLimit] = useState(LIST_RENDER);
  useEffect(() => { setListLimit(LIST_RENDER); }, [search, native]);
  const filteredSessions = sessions.filter(item => `${item.title} ${item.cwd}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()));
  const cachedCount = tabs.filter(tab => tab.native === native).length;
  const hostVersion = !info?.version || info.version === 'dev' ? 'dev' : /^\d/.test(info.version) ? `v${info.version}` : info.version;
  function sidebarState(item: Session) {
    const cached = tabs.find(tab => tab.key === tabKey(item.id, native));
    const waiting = !!cached?.requests.length && (item.mode !== 'channel' || item.online);
    return { cached, status: waiting ? 'waiting' : item.mode === 'channel' ? item.online ? 'idle' : 'offline' : item.status || 'idle', label: waiting ? '等待操作' : native ? '只读历史' : sessionLabel(item) };
  }
  const running = current?.mode !== 'channel' && (current?.status === 'running' || current?.status === 'waiting');
  const resumeID = native ? selected : current?.mode === 'channel' ? current.claudeSessionId : undefined;
  const terminalCommand = (info?.terminalCommand || '') + (resumeID ? ` --resume ${resumeID}` : '');
  // A session that can still deliver tool results: calls without one in the last group are running.
  const live = !native && (current?.mode === 'channel' ? !!current.online : running);
  // Unchanged calls and groups keep their objects so memoized cards skip re-rendering.
  const transcriptCache = useRef<TranscriptItem[]>([]);
  const transcript = useMemo(() => (transcriptCache.current = stabilize(transcriptCache.current, groupTranscript(messages(events), live))), [events, live]);
  // Question and plan cards parse the whole result; load truncated ones as they appear.
  useEffect(() => { for (const item of transcript) if (item.type === 'special' && item.call.truncated) loadFull(item.call); }, [transcript, loadFull]);
  // Shared terminals: AskUserQuestion / ExitPlanMode are answerable here and in the terminal; first answer wins.
  const shared = !native && current?.mode === 'channel';
  const outcomes = useMemo(() => shared ? linkOutcomes(transcript, events) : new Map<number, string>(), [shared, transcript, events]);
  const linked = useMemo(() => shared ? linkRequests(transcript, requests) : new Map<number, Request>(), [shared, transcript, requests]);
  const [toolsExpanded, setToolsExpanded] = useState(() => { try { return localStorage.getItem('cc-tools-expanded') === 'true'; } catch { return false; } });
  function toggleAllTools() {
    const next = !toolsExpanded;
    setToolsExpanded(next);
    try { localStorage.setItem('cc-tools-expanded', String(next)); } catch {}
    setTabs(old => old.map(tab => ({ ...tab, expandedGroups: {} })));
  }
  const toggleGroup = useCallback((key: number, open: boolean) => updateTab(activeKey, tab => ({ ...tab, expandedGroups: { ...tab.expandedGroups, [key]: open } })), [activeKey, updateTab]);
  const toggleCall = useCallback((seq: number, open: boolean) => updateTab(activeKey, tab => tab.expandedTools.includes(seq) === open ? tab : ({ ...tab, expandedTools: open ? [...tab.expandedTools, seq] : tab.expandedTools.filter(item => item !== seq) })), [activeKey, updateTab]);
  function questionHint(call: ToolCall): string | undefined {
    if (call.status === 'missing') return '未记录回答';
    if (native) return '未记录回答';
    if (shared) return ({ web: '已提交，等待 Claude 继续', handoff: '已交给终端，请在终端作答', terminal: '已在终端处理' } as Record<string, string>)[outcomes.get(call.seq) || ''] || '等待回答（请在终端作答）';
    // Managed sessions answer through the interactive request card below; don't show it twice.
    if (requests.some(request => request.kind === (call.name === 'ExitPlanMode' ? 'plan' : 'question'))) return undefined;
    return '等待回答';
  }
  useEffect(() => { setCopyStatus('idle'); }, [sharing, terminalCommand]);
  async function copyCommand() {
    setCopying(true);
    try { await copyText(terminalCommand); setCopyStatus('copied'); }
    catch { setCopyStatus('failed'); }
    finally { setCopying(false); }
  }
  useEffect(() => {
    let active = true;
    setCommands([]);
    if (selected && !native && connected) api<Command[]>(`/api/sessions/${selected}/commands`).then(data => { if (active) setCommands(data); }).catch(() => {});
    return () => { active = false; };
  }, [selected, native, connected, api]);
  useEffect(() => {
    if (!connected || settingsOpen || sessionSettingsOpen) return;
    let active = true;
    api<Defaults>('/api/preferences').then(data => { if (active) setDefaults(data); }).catch(() => {});
    return () => { active = false; };
  }, [connected, settingsOpen, sessionSettingsOpen, api]);
  useEffect(() => { setSessionSettingsOpen(false); }, [activeKey]);
  async function addFiles(files: File[]) {
    if (!files.length || !selected || native || uploading) return;
    const key = activeKey; const id = selected; const count = attachments.length;
    setUploading(true); setError('');
    try {
      const uploaded = await uploadFiles(token, id, files, count);
      updateTab(key, tab => ({ ...tab, attachments: [...tab.attachments, ...uploaded] }));
    } catch (err) { setError((err as Error).message); } finally { setUploading(false); }
  }
  async function action(fn: () => Promise<void>) { setBusy(true); setError(''); try { await fn(); } catch (err) { setError((err as Error).message); } finally { setBusy(false); } }
  // Stable so memoized shared question cards only re-render when their own data changes.
  const answerRequest = useCallback((request: Request, body: object) => {
    const key = activeKey;
    setBusy(true); setError('');
    api(`/api/requests/${request.id}`, { method: 'POST', body: JSON.stringify(body) })
      .then(() => updateTab(key, tab => { const { [request.id]: _, ...questionDrafts } = tab.questionDrafts; return { ...tab, requests: tab.requests.filter(item => item.id !== request.id), questionDrafts }; }))
      .catch(err => setError((err as Error).message))
      .finally(() => setBusy(false));
  }, [activeKey, api, updateTab]);
  const updateQuestionDraft = useCallback((id: string, update: (old: QuestionDraft) => QuestionDraft) => updateTab(activeKey, tab => ({ ...tab, questionDrafts: { ...tab.questionDrafts, [id]: update(tab.questionDrafts[id] || {}) } })), [activeKey, updateTab]);
  const sharedCard = (call: ToolCall | undefined, request: Request) => <SharedQuestion key={request.id} request={request} call={call} busy={busy} draft={activeTab?.questionDrafts[request.id]} onAnswer={answerRequest} onDraft={updateQuestionDraft} />;
  const linkedIds = new Set([...linked.values()].map(request => request.id));
  function choose(id: string, nativeSession = native, item?: Session) {
    const key = tabKey(id, nativeSession);
    // A draft saved earlier (or by another browser tab) comes back when the session is reopened.
    const saved = nativeSession || tabs.some(tab => tab.key === key) ? undefined : loadUI().drafts[id];
    if (saved) savedDrafts.current.set(id, JSON.stringify(pickDraft(saved)));
    setTabs(old => old.some(tab => tab.key === key) ? old : [...old, blankTab(key, id, nativeSession, item || sessions.find(entry => entry.id === id), saved)]);
    setNative(nativeSession); setSelected(id); setDrawer(false); setError(''); setRouteNotice('');
  }
  function closeTab(key: string) {
    const index = tabs.findIndex(tab => tab.key === key);
    if (tabs[index] && !tabs[index].native) forgetDraft(tabs[index].id);
    const remaining = tabs.filter(tab => tab.key !== key);
    setTabs(remaining);
    if (activeKey === key) { const next = remaining[Math.min(index, remaining.length - 1)]; setSelected(next?.id || ''); if (next) setNative(next.native); }
  }
  async function deleteSession(item: Session, stop: boolean) {
    await api(`/api/sessions/${encodeURIComponent(item.id)}${stop ? '?stop=1' : ''}`, { method: 'DELETE' });
    const key = tabKey(item.id, false);
    forgetDraft(item.id);
    setTabs(old => old.filter(tab => tab.key !== key));
    if (!listView.current) setSessions(old => old.filter(entry => entry.id !== item.id));
    if (!native && selected === item.id) setSelected('');
    setDeleting(undefined); setSessionSettingsOpen(false);
  }
  function switchTab(value: boolean) { setNative(value); setSelected(''); setSessions(loadList(value)); setSearch(''); }
  async function send(event: FormEvent) {
    event.preventDefault();
    const text = draft.trim(); const isCommand = text.startsWith('/');
    if ((!text && !attachments.length) || busy || uploading || (!isCommand && current?.mode === 'channel' && !current.online)) return;
    const key = activeKey; const id = selected; const sentImages = attachments;
    await action(async () => {
      if (isCommand) {
        if (attachments.length) throw new Error('请先移除附件再执行控制命令，或发送描述附件的普通消息');
        const result = await api<{ message?: string; openSettings?: boolean; sendMessage?: boolean }>(`/api/sessions/${id}/commands`, { method: 'POST', body: JSON.stringify({ command: text }) });
        if (result.sendMessage) await api(`/api/sessions/${id}/messages`, { method: 'POST', body: JSON.stringify({ text }) });
        if (result.openSettings) setSettingsOpen(true);
        updateTab(key, tab => ({ ...tab, notice: result.message, draft: tab.draft.trim() === text ? '' : tab.draft }));
      } else {
        await api(`/api/sessions/${id}/messages`, { method: 'POST', body: JSON.stringify({ text, attachmentIds: sentImages.map(image => image.id) }) });
        updateTab(key, tab => ({ ...tab, draft: tab.draft.trim() === text ? '' : tab.draft, attachments: tab.attachments.filter(image => !sentImages.some(sent => sent.id === image.id)) }));
      }
      await list();
    });
  }
  if (!connected) return <main className="login"><form className="login-card" onSubmit={event => { event.preventDefault(); if (token === credential) api<Info>('/api/info').then(data => { setInfo(data); setConnected(true); setError(''); localStorage.setItem('cc-token', token); }).catch(err => setError(err.message)); else { setError(''); setToken(credential.trim()); } }}><div className="brand-symbol">↗</div><span className="eyebrow">YOUR WORKSPACE, ANYWHERE</span><h1>CC Remote</h1><p>从手机继续你的 Claude Code 工作。</p><label>宿主访问令牌<input type="password" value={credential} onChange={event => setCredential(event.target.value)} placeholder="输入启动宿主时配置的令牌" autoComplete="current-password" required /></label>{error && <div className="error" role="alert">{error}</div>}<button className="primary">连接宿主 <span>→</span></button><small>连接至当前地址 · 令牌保存在此设备，退出登录时清除</small></form></main>;
  return <div className={`app ${sidebarCollapsed ? 'sidebar-collapsed' : ''}`} style={{ '--sidebar-width': `${sidebar.width}px` } as CSSProperties}>
    {drawer && <button className="backdrop" aria-label="关闭会话列表" onClick={() => setDrawer(false)} />}
    <aside className={`sidebar ${drawer ? 'open' : ''}`}><div className="brand"><div className="brand-symbol">↗</div><div className="brand-text"><div className="brand-name"><span>CC Remote</span><span className="version-pill" title={`宿主版本 ${hostVersion}`}>{hostVersion}</span></div><span className="brand-status" title={info?.claudeVersion || (info?.claudeAvailable ? 'Claude Code 已连接' : '未找到 Claude Code')}><span className={`dot ${info?.claudeAvailable ? 'idle' : 'error'}`} /><span>{info?.claudeAvailable ? 'Claude Code 已连接' : '未找到 Claude Code'}</span></span></div><button className="icon brand-action" title="退出登录" aria-label="退出登录" onClick={() => { localStorage.removeItem('cc-token'); sessionStorage.removeItem('cc-token'); clearTimeout(persistTimer.current); persistTimer.current = 0; draftChanges.current = {}; pendingLayout.current = undefined; pendingViews.current = ''; savedDrafts.current.clear(); savedLayout.current = ''; savedViews.current = ''; clearUI(); setRestored(false); setRouteNotice(''); history.replaceState(null, '', cleanURL()); setToken(''); setConnected(false); setCredential(''); setError(''); setSelected(''); setSessions([]); setTabs([]); }}><LogoutIcon size={16} /></button><button className="mobile icon brand-action" aria-label="关闭会话列表" onClick={() => setDrawer(false)}><CloseIcon size={18} /></button></div>
      <div className="sidebar-actions"><button className="new-session primary-action" title="新建会话" aria-label="新建会话" onClick={() => setCreate(true)}><PlusIcon size={15} /><span>新建</span></button><button className="new-session" title="连接原生终端" aria-label="连接原生终端" onClick={() => setSharing(true)}><TerminalIcon size={15} /><span>连接终端</span></button></div>
      <div className="tabs"><button className={!native ? 'active' : ''} onClick={() => switchTab(false)}>会话</button><button className={native ? 'active' : ''} onClick={() => switchTab(true)}>终端历史</button></div>
      <input className="session-search" aria-label="搜索会话" placeholder="搜索名称或项目目录" value={search} onChange={event => setSearch(event.target.value)} />
      <div className="session-list-heading"><span>{native ? '终端历史' : '全部会话'}</span><span title="缓存在当前页面内，切换保留草稿和阅读位置">已缓存 {cachedCount}</span></div>
      <div className="session-list">{filteredSessions.length === 0 ? <p className="muted empty-list">{search.trim() ? '没有匹配的会话' : native ? '暂无可读取的终端历史' : '开始你的第一个会话'}</p> : filteredSessions.slice(0, listLimit).map(item => {
        const state = sidebarState(item);
        return <div key={item.id} className={`session-row ${selected === item.id ? 'selected' : ''} ${state.cached ? 'cached' : ''}`}>
          <button className="session-item" aria-current={selected === item.id ? 'page' : undefined} onClick={() => choose(item.id)}>
            <div><span className={`dot ${state.status}`} /><strong>{item.title || '未命名会话'}</strong></div>
            <small title={item.cwd}>{item.cwd}</small>
            <span className="session-meta"><span className={`session-state ${state.status}`}>{!native && (item.mode === 'channel' ? '共享 · ' : '托管 · ')}{state.label}</span>{state.cached && <span className="cache-marker" title="切换会话时保留当前页面的消息、草稿和阅读位置">{state.cached.loaded ? '已缓存' : '加载中'}</span>}</span>
          </button>
          {state.cached && <button className="release-cache" title="释放此会话的页面缓存，未发送草稿将清除，宿主任务继续运行" aria-label={`释放 ${item.title || '会话'} 的缓存`} onClick={() => closeTab(state.cached!.key)}><CloseIcon size={13} /></button>}
          {!native && <button className="delete-session" title="删除此会话的 CC Remote 记录" aria-label={`删除 ${item.title || '会话'}`} onClick={() => { setDeleting(item); setDrawer(false); }}><TrashIcon size={13} /></button>}
        </div>;
      })}{filteredSessions.length > listLimit && <button type="button" className="list-more" onClick={() => setListLimit(n => n + LIST_RENDER)}>显示更多（还有 {filteredSessions.length - listLimit} 个）</button>}</div>
      <nav className="sidebar-nav" aria-label="全局"><button title="设置" onClick={() => { setSettingsOpen(true); setDrawer(false); }}><GearIcon size={15} /><span>设置</span></button><button title="服务状态" onClick={() => { setStatusOpen(true); setDrawer(false); }}><StatusIcon size={15} /><span>服务状态</span></button></nav>
    </aside>
    {!sidebarCollapsed && <SidebarResizer width={sidebar.width} max={sidebar.max} onChange={sidebar.set} />}
    <main className="workspace"><header className="topbar"><button className="desktop icon" aria-label={sidebarCollapsed ? '展开会话列表' : '折叠会话列表'} title={sidebarCollapsed ? '展开会话列表' : '折叠会话列表'} onClick={() => setSidebarCollapsed(old => { localStorage.setItem('cc-sidebar-collapsed', String(!old)); return !old; })}><MenuIcon size={18} /></button><button className="mobile icon" aria-label="打开会话列表" onClick={() => setDrawer(true)}><MenuIcon size={18} /></button><div className="header-title"><h1>{current?.title || '工作空间'}</h1><div className="header-sub">{current && !native && <SessionChips session={current} defaults={defaults} open={() => setSessionSettingsOpen(true)} />}<p title={current?.cwd || info?.cwd}>{current?.cwd || info?.cwd}</p></div></div><span className="badge">{native ? '只读历史' : sessionLabel(current)}</span>{selected && <button className={`icon${toolsExpanded ? ' active' : ''}`} title={toolsExpanded ? '折叠全部工具调用' : '展开全部工具调用'} aria-label={toolsExpanded ? '折叠全部工具调用' : '展开全部工具调用'} aria-pressed={toolsExpanded} onClick={toggleAllTools}>{toolsExpanded ? <CollapseAllIcon size={17} /> : <ExpandAllIcon size={17} />}</button>}<button className="icon" title="刷新会话" aria-label="刷新会话" onClick={() => action(list)}><RefreshIcon size={17} /></button>{current && !native && <button className="icon" title="会话设置" aria-label="会话设置" onClick={() => setSessionSettingsOpen(true)}><GearIcon size={17} /></button>}</header>
      {plainHttp && <div className="plain-http-banner banner" role="status">当前通过明文 HTTP 访问（{location.host}），访问令牌和会话内容会在网络中明文传输。建议改用 HTTPS 反向代理或 VPN。<button className="icon-button" onClick={dismissPlainHttp} aria-label="关闭明文访问提示"><CloseIcon size={14} /></button></div>}
      {error && <div className="error banner" role="alert">{error}<button className="icon-button" onClick={() => setError('')} aria-label="关闭错误"><CloseIcon size={14} /></button></div>}
      {routeNotice && <div className="command-notice" role="status"><span>{routeNotice}</span><button className="icon-button" aria-label="关闭提示" onClick={() => setRouteNotice('')}><CloseIcon size={14} /></button></div>}
      {activeTab?.notice && <div className="command-notice" role="status"><span>{activeTab.notice}</span><button className="icon-button" aria-label="关闭命令结果" onClick={() => updateTab(activeKey, tab => ({ ...tab, notice: undefined }))}><CloseIcon size={14} /></button></div>}{settingsOpen && <ClaudeSettings api={api} close={() => setSettingsOpen(false)} />}{statusOpen && <StatusPanel api={api} close={() => setStatusOpen(false)} select={id => { setStatusOpen(false); choose(id, false); }} />}{sessionSettingsOpen && current && !native && <SessionSettings api={api} session={current} defaults={defaults} close={() => setSessionSettingsOpen(false)} onDelete={() => { setSessionSettingsOpen(false); setDeleting(current); }} onUpdated={item => { setSessions(old => old.map(entry => entry.id === item.id ? { ...entry, ...item } : entry)); updateTab(activeKey, tab => tab.session ? { ...tab, session: { ...tab.session, ...item } } : tab); }} />}{deleting && <DeleteSessionDialog session={sessions.find(item => item.id === deleting.id) || deleting} close={() => setDeleting(undefined)} confirm={stop => deleteSession(deleting, stop)} />}{!selected ? <div className="welcome"><div className="welcome-icon">⌘</div><span className="eyebrow">READY WHEN YOU ARE</span><h2>把工作带到身边。</h2><p>选择一个会话，或在项目目录中开始新的任务。<br/>会话持续在宿主运行，关闭页面也可以稍后回来。</p><div className="welcome-actions"><button className="primary" onClick={() => setCreate(true)}><PlusIcon size={15} />新建会话</button><button className="secondary" onClick={() => setSharing(true)}><TerminalIcon size={15} />连接原生终端</button></div>{!info?.claudeAvailable && <p className="error">请先在宿主安装 Claude Code 并完成登录。</p>}</div> : <>
        <div className="conversation" ref={scroll} onScroll={() => { const element = scroll.current; if (element && !loading) { nearBottom.current = element.scrollHeight - element.scrollTop - element.clientHeight < 120; updateTab(activeKey, tab => ({ ...tab, scrollTop: element.scrollTop, nearBottom: nearBottom.current })); } }}>
          {loading && <div className="muted loading">正在加载会话…</div>}
          {!loading && activeTab?.hasOlder && <div ref={olderMarker} className="older-marker">{activeTab.loadingOlder ? '正在加载更早的消息…' : <button type="button" onClick={() => void loadOlder()}>加载更早的消息</button>}</div>}
          {!loading && events.length === 0 && <div className="conversation-empty"><h2>准备开始</h2><p>描述你的任务，Claude 会在这个项目目录中工作。</p></div>}
          {/* One child list: a shared card keyed by its request id moves between the transcript and the pending list without remounting. */}
          {[...transcript.map(item => {
            if (item.type === 'tools') return <ToolGroup key={item.key} groupKey={item.key} calls={item.calls} open={activeTab?.expandedGroups[item.key] ?? toolsExpanded} onToggle={toggleGroup} openCalls={activeTab?.expandedTools || NO_CALLS} onToggleCall={toggleCall} onLoadFull={loadFull} />;
            if (item.type === 'special') {
              const request = linked.get(item.call.seq);
              if (request) return sharedCard(item.call, request);
              const hint = questionHint(item.call); const outcome = outcomes.get(item.call.seq);
              return item.call.status === 'ok' || item.call.status === 'error' || hint ? <QuestionCard key={item.key} call={item.call} waitingHint={hint || ''} answeredBy={outcome === 'web' ? 'web' : outcome === 'terminal' || outcome === 'handoff' ? 'terminal' : undefined} /> : null;
            }
            const event = item.event;
            if (event.kind === 'compact') return <div className="compact-divider" role="separator" key={event.seq}><span>{compactSummary(event.payload)}</span></div>;
            return <article className={`message ${event.kind}`} key={event.seq}><div className="message-label">{event.kind === 'user' ? '你' : event.kind === 'error' ? '执行错误' : 'CLAUDE'}</div><Markdown text={event.text || ''} />{event.payload?.attachments?.length > 0 && <AttachmentList items={event.payload.attachments} token={token} />}</article>;
          }),
          // A request whose call already shows a terminal result is about to be withdrawn; don't show it twice.
          ...(native ? [] : requests.filter(request => !linkedIds.has(request.id) && !(shared && requestSettled(request, transcript))).map(request => shared && (request.kind === 'question' || request.kind === 'plan') ? sharedCard(undefined, request) : <RequestCard key={request.id} request={request} busy={busy} answers={activeTab?.answers[request.id] || {}} setAnswers={update => updateTab(activeKey, tab => ({ ...tab, answers: { ...tab.answers, [request.id]: update(tab.answers[request.id] || {}) } }))} onAnswer={(decision, answers) => action(async () => { await api(`/api/requests/${request.id}`, { method: 'POST', body: JSON.stringify({ decision, answers }) }); updateTab(activeKey, tab => ({ ...tab, requests: tab.requests.filter(item => item.id !== request.id) })); })} />))]}
          {!native && current?.queued?.map((item, index) => <article className="message user queued" key={`queued-${index}`}><div className="message-label">你 · 等待处理</div><Markdown text={item.text} />{item.payload?.attachments?.length > 0 && <AttachmentList items={item.payload.attachments} token={token} />}</article>)}
          {running && !requests.length && <div className="working"><span className="pulse" />{isCompacting(events) ? '正在压缩上下文…' : 'Claude 正在工作…'}</div>}<div ref={bottom} />
        </div>
        {native ? <div className="native-footer"><p>沿用原会话标题和上下文。通过连接命令恢复后，终端与网页可交替使用。</p><button className="primary" onClick={() => setSharing(true)}>连接此终端会话 ↔</button><button className="secondary" onClick={() => setImporting(true)}>导入为托管会话</button></div> : <form className={`composer${dragging ? ' dragging' : ''}`} onSubmit={send} onDragOver={event => { if (!event.dataTransfer.types.includes('Files')) return; event.preventDefault(); event.dataTransfer.dropEffect = 'copy'; setDragging(true); }} onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node)) setDragging(false); }} onDrop={event => { if (!event.dataTransfer.types.includes('Files')) return; event.preventDefault(); setDragging(false); void addFiles(Array.from(event.dataTransfer.files)); }}>{dragging && <div className="drop-hint" aria-hidden="true">松开以添加附件（单个 ≤ 20 MiB，每条最多 8 个）</div>}{draft.startsWith('/') && !draft.includes(' ') && <div className="command-menu" role="listbox" aria-label="命令建议">{commands.filter(command => command.name.startsWith(draft.slice(1))).map(command => <button type="button" key={command.name} onClick={() => setDraft(`/${command.name} `)}><strong>/{command.name}</strong><small>{command.description}{command.terminalOnly ? ' · 终端操作' : ''}</small></button>)}</div>}{attachments.length > 0 && <AttachmentList items={attachments} token={token} remove={id => updateTab(activeKey, tab => ({ ...tab, attachments: tab.attachments.filter(item => item.id !== id) }))} />}<textarea aria-label="发送给 Claude 的消息" placeholder={current?.mode === 'channel' && !current.online ? '终端离线，请先在宿主重新启动连接命令' : running ? '可以继续输入，消息会排在当前任务之后' : '描述任务，或继续提问…'} value={draft} onChange={event => setDraft(event.target.value)} onPaste={event => { const files = Array.from(event.clipboardData.files); if (files.length) { event.preventDefault(); void addFiles(files); } }} onCompositionStart={() => { composing.current = true; }} onCompositionEnd={() => { composing.current = false; }} onKeyDown={event => { if (event.key !== 'Enter' || event.nativeEvent.isComposing || composing.current || event.keyCode === 229) return; const modifier = event.ctrlKey || event.metaKey; if (!event.shiftKey && !event.altKey && (sendKey === 'enter' ? !modifier : modifier)) { event.preventDefault(); void send(event); } }} /><div className="composer-bottom"><AttachButton disabled={uploading || busy} upload={files => void addFiles(files)} /><span><Select className="send-key" label="发送快捷键" title="Shift + Enter 始终换行；中文输入法选词时不会发送" value={sendKey} options={SEND_KEYS} onChange={value => { setSendKey(value); localStorage.setItem('cc-send-key', value); }} /></span><div className="composer-actions">{running && <button type="button" className="stop" disabled={busy} title="停止当前轮次；排队的消息会继续执行" onClick={() => action(async () => { await api(`/api/sessions/${selected}/stop`, { method: 'POST' }); await list(); })}>■ 停止</button>}{(!running || draft.trim() || attachments.length > 0) && <button className="primary" disabled={busy || uploading || (!draft.trim() && !attachments.length) || (!draft.startsWith('/') && (!info?.claudeAvailable || (current?.mode === 'channel' && !current.online)))}>{uploading ? '上传中…' : running && !draft.startsWith('/') ? '排队 ↑' : '发送 ↑'}</button>}</div></div></form>}
      </>}
    </main>
    {sharing && <div className="modal-backdrop"><div className="modal"><div className="modal-heading"><h2>{resumeID ? '连接原会话' : '连接原生终端'}</h2><button className="icon" aria-label="关闭连接说明" onClick={() => setSharing(false)}><CloseIcon size={18} /></button></div><p>在宿主的项目目录中运行以下命令。Claude Code 保留原生终端界面，连接后会话自动出现在网页列表。</p>{resumeID && <p>先退出该会话当前的 Claude Code 进程，再用下面的命令恢复一次。原标题自动保留，无需命名；后续两端切换无需退出。</p>}<textarea className="terminal-command" aria-label="连接命令" readOnly rows={4} value={terminalCommand} onFocus={event => event.currentTarget.select()} /><div className="share-actions"><button type="button" className="secondary" disabled={copying || !terminalCommand} onClick={() => void copyCommand()}>{copying ? '复制中…' : copyStatus === 'copied' ? '已复制' : '复制命令'}</button><span role="status" className={`copy-feedback ${copyStatus === 'failed' ? 'failed' : ''}`}>{copyStatus === 'copied' ? '命令已复制到剪贴板' : copyStatus === 'failed' ? '浏览器未允许自动复制，请点击上方命令框，全选后复制；手机可长按复制。' : ''}</span></div><p>首次启动需在终端确认开发 Channel、项目信任及 MCP 启用。此选项不会跳过工具权限。</p><p>如在终端使用 /resume 或 /clear 切换了会话，旧网页连接会断开；请退出后使用连接命令打开目标会话。</p><small>消息、工具审批、提问选项与计划确认可在网页操作；提问与计划确认在网页和终端均可作答，先答先生效。停止生成暂在原生终端处理。Channels 仍是官方预览功能，组织策略可能限制使用。</small>{error && <div className="error" role="alert">{error}</div>}</div></div>}
    {create && <div className="modal-backdrop"><form className="modal" onSubmit={event => { event.preventDefault(); action(async () => { if (!confirmMode(newOverrides.permissionMode, '')) return; const item = await api<Session>('/api/sessions', { method: 'POST', body: JSON.stringify({ title: title.trim(), cwd: cwd.trim(), ...newOverrides, model: newOverrides.model.trim(), confirmDangerous: dangerousMode(newOverrides.permissionMode) }) }); setNative(false); setSessions(old => [item, ...old]); choose(item.id, false); setCreate(false); setTitle(''); }); }}><div className="modal-heading"><h2>新建会话</h2><button type="button" className="icon" aria-label="关闭" onClick={() => setCreate(false)}><CloseIcon size={18} /></button></div><p>为任务选择宿主上的项目目录。</p><label>会话名称<input value={title} onChange={event => setTitle(event.target.value)} placeholder="例如：修复登录页面" required autoFocus /></label><label>项目绝对路径<input value={cwd} onChange={event => setCwd(event.target.value)} placeholder="D:\\Projects\\app 或 /home/me/app" required /></label><small>目录需要已经存在，Claude 将在此目录中执行操作。</small><div className="new-session-overrides"><label>权限模式<Select label="新会话权限模式" value={newOverrides.permissionMode} options={permissionOptions('跟随默认')} onChange={permissionMode => setNewOverrides(old => ({ ...old, permissionMode }))} /></label><label>推理强度<Select label="新会话推理强度" value={newOverrides.effort} options={effortOptions('跟随默认')} onChange={effort => setNewOverrides(old => ({ ...old, effort }))} /></label></div><label>模型<ModelPicker api={api} label="新会话模型" value={newOverrides.model} onChange={model => setNewOverrides(old => ({ ...old, model }))} /></label>{error && <div className="error" role="alert">{error}</div>}<button className="primary" disabled={busy}>{busy ? '创建中…' : '创建会话 →'}</button></form></div>}
    {importing && <div className="modal-backdrop"><div className="modal"><div className="modal-heading"><h2>导入为托管会话</h2><button className="icon" aria-label="关闭" onClick={() => setImporting(false)}><CloseIcon size={18} /></button></div><p>同一个会话同时运行会导致状态和文件操作冲突。请先在原终端停止该会话。</p><label className="checkbox"><input type="checkbox" checked={stopped} onChange={event => setStopped(event.target.checked)} />我已停止原终端中的 Claude Code 会话</label><button className="primary" disabled={!stopped || busy} onClick={() => action(async () => { const item = await api<Session>(`/api/native/sessions/${selected}/import`, { method: 'POST', body: JSON.stringify({ confirmStopped: true }) }); setNative(false); setSessions([item]); choose(item.id, false); setImporting(false); })}>确认导入</button>{error && <div className="error" role="alert">{error}</div>}</div></div>}
  </div>;
}

function RequestCard({ request, busy, onAnswer, answers, setAnswers }: { request: Request; busy: boolean; onAnswer: (decision: string, answers?: Record<string, string>) => void; answers: Record<string, string>; setAnswers: (update: (old: Record<string, string>) => Record<string, string>) => void }) {
  const questions: any[] = request.input?.questions || [];
  return <section className="request"><span className="eyebrow">需要你的操作</span><h3>{request.kind === 'question' ? 'Claude 有几个问题' : request.kind === 'plan' ? '确认执行计划' : `允许执行 ${request.toolName || '工具'}？`}</h3>
    {request.kind === 'question' ? questions.map((question, index) => <div className="question" key={index}><p>{question.question}</p><div className="options">{(question.options || []).map((option: any) => <button key={option.label} className={(answers[question.question] || '').split(', ').includes(option.label) ? 'chosen' : ''} disabled={busy} onClick={() => setAnswers(old => { const key = question.question; const values = (old[key] || '').split(', ').filter(Boolean); return { ...old, [key]: question.multiSelect ? (values.includes(option.label) ? values.filter(value => value !== option.label) : [...values, option.label]).join(', ') : option.label }; })}>{option.label}{option.description && <small>{option.description}</small>}</button>)}</div><input aria-label={`回答：${question.question}`} placeholder="也可以输入自己的回答" value={answers[question.question] || ''} onChange={event => setAnswers(old => ({ ...old, [question.question]: event.target.value }))} /></div>) : <pre>{typeof request.input === 'string' ? request.input : JSON.stringify(request.input, null, 2)}</pre>}
    <div className="request-actions"><button className="secondary" disabled={busy} onClick={() => onAnswer('deny')}>拒绝</button><button className="primary" disabled={busy || (request.kind === 'question' && questions.some(question => !answers[question.question]?.trim()))} onClick={() => onAnswer('allow', request.kind === 'question' ? answers : undefined)}>{request.kind === 'question' ? '提交回答' : '允许本次操作'}</button></div>
  </section>;
}

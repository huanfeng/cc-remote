import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type FormEvent } from 'react';
import Markdown from './Markdown';
import { ClaudeSettings } from './ClaudeSettings';
import { StatusPanel } from './StatusPanel';
import { AttachButton, AttachmentList, uploadFiles, type Attachment } from './Attachments';
import { QuickSwitch, type QuickKind, type QuickTarget } from './QuickSwitch';
import { ComposerInput } from './ComposerInput';
import { copyText } from './clipboard';
import { DeleteSessionDialog, SessionChips, SessionSettings, type Defaults, type LiveStatus } from './SessionSettings';
import { SidebarResizer, useSidebarWidth } from './SidebarResizer';
import { ModelPicker, confirmMode, dangerousMode, effortOptions, permissionOptions } from './ModelPicker';
import { Select } from './Select';
import { AgentIcon, CloseIcon, CollapseAllIcon, ExpandAllIcon, GearIcon, LogoutIcon, MenuIcon, PlusIcon, RefreshIcon, StatusIcon, TerminalIcon, TrashIcon } from './icons';
import { ToolGroup } from './ToolGroup';
import { AgentCard, TaskPanel, type NestedProps, type TaskInfo } from './AgentCard';
import { QuestionCard, type QuestionDraft } from './QuestionCard';
import { compactSummary, groupTranscript, isCompacting, parentOf, linkOutcomes, linkRequests, requestCall, requestSettled, stabilize, type ToolCall, type TranscriptItem } from './toolGroups';
import { MAX_UI_CHARS, UI_KEY, UI_PREFIX, VIEW_KEY, deserialize, draftEmpty, emptyUI, forget, formatHash, mergeUI, parseHash, parseKey, parseViews, serialize, serializeViews, type Route, type SessionDraft, type UIState, type ViewState } from './persist';
import { lang, t } from './i18n';
import { applyIdentity, initialIdentity, onIdentity } from './hostIdentity';
import { TopbarMenu, type TopbarAction } from './TopbarMenu';
import { matchProjects, projectKey, recentProjects, type RecentProject } from './recentProjects';

type Session = { id: string; title: string; cwd: string; truncated?: boolean; claudeArgs?: string[]; status?: string; createdAt?: string; updatedAt: string; mode?: string; online?: boolean; claudeSessionId?: string; permissionMode?: string; model?: string; effort?: string; queued?: { text: string; payload?: any }[]; live?: LiveStatus };
type Event = { seq: number; kind: string; text?: string; payload?: any; createdAt: string; truncated?: boolean };
type Request = { id: string; kind: string; toolName: string; input: any; status: string; createdAt?: string };
type Info = { version?: string; cwd: string; claudeAvailable: boolean; claudeVersion: string; attachCommand?: string; channelExperimental?: boolean; language?: string; name?: string; color?: string };
type ConversationTab = { key: string; id: string; native: boolean; session?: Session; events: Event[]; requests: Request[]; draft: string; attachments: Attachment[]; notice?: string; scrollTop: number; nearBottom: boolean; loaded: boolean; hasOlder: boolean; loadingOlder: boolean; expandedTools: number[]; expandedGroups: Record<number, boolean>; answers: Record<string, Record<string, string>>; questionDrafts: Record<string, QuestionDraft> };
type Command = { name: string; description: string; terminalOnly?: boolean };
const SEND_KEYS = [{ value: 'ctrl-enter', label: 'Ctrl / ⌘ + Enter', description: t('Enter adds a new line; the shortcut sends', 'Enter 换行，组合键发送') }, { value: 'enter', label: t('Enter sends', 'Enter 发送'), description: t('Shift + Enter adds a new line', 'Shift + Enter 换行') }];
const transcriptPath = (tab: { id: string; native: boolean }) => `/api/${tab.native ? 'native/sessions' : 'sessions'}/${encodeURIComponent(tab.id)}`;
const tabKey = (id: string, native: boolean) => `${native ? 'native' : 'session'}:${id}`;
const STATUS_LABELS: Record<string, string> = { running: t('Running', '执行中'), waiting: t('Waiting for answer', '等待回答'), idle: t('Ready', '就绪'), completed: t('Completed', '已完成'), interrupted: t('Stopped', '已停止'), failed: t('Failed', '执行失败'), stopped: t('Stopped', '已停止'), offline: t('Offline', '离线'), error: t('Failed', '执行失败') };
const statusLabel = (status?: string) => STATUS_LABELS[status || ''] || status || t('History', '历史');
const sessionLabel = (session?: Session) => session?.mode === 'channel' && !session.online ? t('Terminal offline', '终端离线') : ['running', 'waiting'].includes(session?.status || '') ? statusLabel(session?.status) : session?.mode === 'channel' ? t('Terminal online', '终端在线') : session?.mode === 'terminal' && !session.online ? t('Not running', '未运行') : statusLabel(session?.status || 'idle');
const modeLabel = (mode?: string) => mode === 'channel' ? t('Shared', '共享') : mode === 'terminal' ? t('Terminal', '终端') : t('Managed', '托管');
const COPY_FAILED = t('The browser did not allow automatic copying. Click the command box above, select all and copy; on a phone, long-press to copy.', '浏览器未允许自动复制，请点击上方命令框，全选后复制；手机可长按复制。');

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
type SharedQuestionProps = { request: Request; call?: ToolCall; busy: boolean; sessionMode?: string; draft?: QuestionDraft; onAnswer: (request: Request, body: object) => void; onDraft: (id: string, update: (old: QuestionDraft) => QuestionDraft) => void };

/**
 * Interactive card for a shared-terminal question; "Answer in terminal" leaves it to the native dialog.
 * Its unsent input lives in the tab (by request id), so moving between the transcript and
 * the pending list, or switching sessions, keeps it. A request's content never changes, and
 * polling recreates its object, so it is compared by id.
 */
const SharedQuestion = memo(function SharedQuestion({ request, call, busy, sessionMode, draft, onAnswer, onDraft }: SharedQuestionProps) {
  const shown = useMemo(() => call ? (request.kind === 'plan' ? { ...call, input: request.input } : call) : requestCall(request), [call, request.id]);
  const setDraft = useCallback((update: (old: QuestionDraft) => QuestionDraft) => onDraft(request.id, update), [onDraft, request.id]);
  return <QuestionCard call={shown} waitingHint={t('Waiting for an answer (on the web or in the terminal)', '等待回答（网页或终端均可）')} busy={busy} sessionMode={sessionMode} draft={draft || NO_DRAFT} setDraft={setDraft} onAnswer={answers => onAnswer(request, { decision: 'allow', answers })} onDecide={decision => onAnswer(request, decision)} onHandoff={() => onAnswer(request, { decision: 'terminal' })} />;
}, (a, b) => a.request.id === b.request.id && a.call === b.call && a.busy === b.busy && a.sessionMode === b.sessionMode && a.draft === b.draft && a.onAnswer === b.onAnswer && a.onDraft === b.onDraft);

function messages(events: Event[]) {
  const items: Event[] = [];
  let stream: Event | undefined;
  for (const event of events) {
    // A subagent's messages nest under its Agent call and never touch the main reply's stream.
    if (parentOf(event)) { if (['assistant', 'tool', 'tool_result'].includes(event.kind)) items.push(event); continue; }
    if (event.kind === 'assistant_delta') {
      if (!stream) { stream = { ...event, kind: 'assistant', text: '' }; items.push(stream); }
      stream.text = (stream.text || '') + (event.text || '');
    } else if (event.kind === 'assistant') {
      if (stream) { stream.text = event.text; stream = undefined; }
      else items.push(event);
    } else {
      // A background task's notification is a user turn Claude Code adds, not something typed
      // (the host filters it now; terminal sessions may have recorded it before).
      if (event.kind === 'user' && event.text?.trimStart().startsWith('<task-notification>')) continue;
      if (['user', 'tool', 'tool_result', 'error', 'compact', 'recap'].includes(event.kind)) items.push(event);
      if (['user', 'result', 'error', 'compact', 'recap'].includes(event.kind)) stream = undefined;
    }
  }
  return items;
}

export default function App() {
  const [token, setToken] = useState(() => localStorage.getItem('cc-token') || sessionStorage.getItem('cc-token') || '');
  const [credential, setCredential] = useState('');
  const [connected, setConnected] = useState(false);
  const [info, setInfo] = useState<Info>();
  const [host, setHost] = useState(initialIdentity);
  useEffect(() => onIdentity(setHost), []);
  useEffect(() => { if (info?.name && info.color && (info.name !== host.name || info.color !== host.color)) applyIdentity({ name: info.name, color: info.color }); }, [info?.name, info?.color]);
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
  const [newMode, setNewMode] = useState<'managed' | 'terminal'>(() => { try { return localStorage.getItem('cc-new-mode') === 'managed' ? 'managed' : 'terminal'; } catch { return 'terminal'; } });
  const [trustFolder, setTrustFolder] = useState(false);
  const [cwd, setCwd] = useState('');
  const [newOverrides, setNewOverrides] = useState({ permissionMode: '', model: '', effort: '' });
  const [importing, setImporting] = useState<'' | 'terminal' | 'managed'>('');
  const [stopped, setStopped] = useState(false);
  const [sharing, setSharing] = useState(false);
  const [copyStatus, setCopyStatus] = useState<'idle' | 'copied' | 'failed'>('idle');
  const [copying, setCopying] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [statusOpen, setStatusOpen] = useState(false);
  const [tasksOpen, setTasksOpen] = useState(false);
  const [sessionSettingsOpen, setSessionSettingsOpen] = useState(false);
  const [quick, setQuick] = useState<QuickTarget | null>(null);
  const [deleting, setDeleting] = useState<Session>();
  const [defaults, setDefaults] = useState<Defaults>();
  // defaultMode of Claude's own settings.json, the last fallback of a session's permission mode.
  const [claudeMode, setClaudeMode] = useState('');
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
    const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}`, 'X-CC-Lang': lang, ...(cached ? { 'If-None-Match': cached.tag } : {}), ...options?.headers }, cache: 'no-store' });
    if (response.status === 304 && cached) return cached.data as T;
    if (!response.ok) {
      let message = t(`Request failed (${response.status})`, `请求失败 (${response.status})`);
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
        // The tab on screen tells the host someone is looking (terminal sessions: focus for recaps).
        const viewing = snapshot.key === selectedRef.current && !document.hidden ? '&viewing=1' : '';
        if (!snapshot.loaded) {
          const data = await api<{ session: Session; events: Event[]; hasMore?: boolean }>(`${base}?tail=${PAGE}&compact=1${viewing}`);
          nextSession = data.session;
          nextEvents = data.events;
          hasOlder = !!data.hasMore;
        } else {
          nextEvents = await api<Event[]>(`${base}/events?after=${snapshot.events.at(-1)?.seq || 0}&compact=1${viewing}`);
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
      if (hash && (!target || lookup(target) === null)) setRouteNotice(t('The session does not exist or was deleted', '会话不存在或已删除'));
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
      setRouteNotice(t('The session does not exist or was deleted', '会话不存在或已删除'));
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
  // The soft keyboard shrinks the conversation: stay at the bottom when it was there.
  useEffect(() => {
    const element = scroll.current;
    if (!element || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(() => { if (nearBottom.current) element.scrollTop = element.scrollHeight; });
    observer.observe(element);
    return () => observer.disconnect();
  }, [selected]);
  const current = sessions.find(item => item.id === selected) || session;
  const [listLimit, setListLimit] = useState(LIST_RENDER);
  useEffect(() => { setListLimit(LIST_RENDER); }, [search, native]);
  const filteredSessions = sessions.filter(item => `${item.title} ${item.cwd}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()));
  const cachedCount = tabs.filter(tab => tab.native === native).length;
  const hostVersion = !info?.version || info.version === 'dev' ? 'dev' : /^\d/.test(info.version) ? `v${info.version}` : info.version;
  function sidebarState(item: Session) {
    const cached = tabs.find(tab => tab.key === tabKey(item.id, native));
    const waiting = !!cached?.requests.length && (item.mode !== 'channel' || item.online);
    return { cached, status: waiting ? 'waiting' : item.mode === 'channel' ? item.online ? 'idle' : 'offline' : item.status || 'idle', label: waiting ? t('Needs action', '等待操作') : native ? t('Read-only history', '只读历史') : sessionLabel(item) };
  }
  const running = current?.mode !== 'channel' && (current?.status === 'running' || current?.status === 'waiting');
  // Terminal sessions: the host holds Claude Code; a local terminal attaches to it.
  const attaching = !native && current?.mode === 'terminal';
  const attachCommand = attaching && info?.attachCommand ? `${info.attachCommand} ${current!.id.slice(0, 8)}` : '';
  // A session that can still deliver tool results: calls without one in the last group are running.
  const live = !native && (current?.mode === 'channel' ? !!current.online : running);
  // Unchanged calls and groups keep their objects so memoized cards skip re-rendering.
  const transcriptCache = useRef<TranscriptItem[]>([]);
  // Subagents and background tasks of the open managed session, folded by the host from
  // Claude's task events. Fetched on opening, when a new task event arrives in the transcript
  // poll, and when the session's status changes (a process that exits ends its tasks).
  const [tasks, setTasks] = useState<TaskInfo[]>([]);
  const taskSignal = useMemo(() => { for (let i = events.length - 1; i >= 0; i--) if (events[i].kind === 'status' && events[i].text?.startsWith('task_')) return events[i].seq; return 0; }, [events]);
  const sessionStatus = session?.status;
  useEffect(() => { setTasks([]); }, [selected, native]);
  useEffect(() => {
    if (!selected || native || !connected) return;
    let active = true;
    api<TaskInfo[]>(`/api/sessions/${encodeURIComponent(selected)}/tasks`).then(data => { if (active) setTasks(data); }).catch(() => {});
    return () => { active = false; };
  }, [selected, native, connected, api, taskSignal, sessionStatus]);
  const taskByCall = useMemo(() => new Map(tasks.filter(task => task.toolUseId).map(task => [task.toolUseId!, task])), [tasks]);
  const runningTasks = tasks.filter(task => task.status === 'running').length;
  const transcript = useMemo(() => (transcriptCache.current = stabilize(transcriptCache.current, groupTranscript(messages(events), live, id => taskByCall.get(id)?.status === 'running'))), [events, live, taskByCall]);
  // Question and plan cards parse the whole result; load truncated ones as they appear.
  useEffect(() => { for (const item of transcript) if (item.type === 'special' && item.call.truncated) loadFull(item.call); }, [transcript, loadFull]);
  // Shared terminals: AskUserQuestion / ExitPlanMode are answerable here and in the terminal; first answer wins.
  // Questions and plans of shared and terminal sessions are answerable here and in the terminal.
  const shared = !native && (current?.mode === 'channel' || current?.mode === 'terminal');
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
  const nested: NestedProps = useMemo(() => ({ openGroups: activeTab?.expandedGroups || {}, toolsExpanded, onToggleGroup: toggleGroup, openCalls: activeTab?.expandedTools || NO_CALLS, onToggleCall: toggleCall, onLoadFull: loadFull, tasks: taskByCall }), [activeTab?.expandedGroups, activeTab?.expandedTools, toolsExpanded, toggleGroup, toggleCall, loadFull, taskByCall]);
  // Jump from the task panel to the call that started a task (an agent's card, or a command
  // inside one): open the cards and groups around it and scroll there.
  function locateTask(task: TaskInfo) {
    const id = task.toolUseId || '';
    const groups: number[] = []; let call: number | undefined;
    const find = (items: TranscriptItem[]): boolean => items.some(item => {
      if (item.type === 'agent') { if (item.call.id === id) { groups.push(item.key); return true; } if (find(item.items)) { groups.push(item.key); return true; } return false; }
      if (item.type === 'tools') { const hit = item.calls.find(entry => entry.id === id); if (hit) { groups.push(item.key); call = hit.seq; return true; } }
      return false;
    });
    setTasksOpen(false);
    if (!id || !find(transcript)) { setRouteNotice(hasOlder ? t('This task is in earlier messages; scroll up to load them and try again', '这个任务在更早的消息里，向上滚动加载后再试') : t('No call found for this task', '没有找到这个任务的调用记录')); return; }
    updateTab(activeKey, tab => ({ ...tab, expandedGroups: { ...tab.expandedGroups, ...Object.fromEntries(groups.map(key => [key, true])) }, expandedTools: call === undefined || tab.expandedTools.includes(call) ? tab.expandedTools : [...tab.expandedTools, call] }));
    window.setTimeout(() => document.querySelector(`[data-agent="${CSS.escape(id)}"],[data-call="${CSS.escape(id)}"]`)?.scrollIntoView({ block: 'start', behavior: 'smooth' }), 50);
  }
  const headerStatus = native ? t('Read-only history', '只读历史') : sessionLabel(current);
  // The top bar's secondary actions, gathered in the ⋯ menu at phone width.
  const topbarActions: TopbarAction[] = [
    ...(selected ? [{ key: 'tools', label: toolsExpanded ? t('Collapse all tool calls', '折叠全部工具调用') : t('Expand all tool calls', '展开全部工具调用'), icon: toolsExpanded ? <CollapseAllIcon size={15} /> : <ExpandAllIcon size={15} />, onClick: toggleAllTools, active: toolsExpanded }] : []),
    { key: 'refresh', label: t('Refresh sessions', '刷新会话'), icon: <RefreshIcon size={15} />, onClick: () => action(list) },
    ...(attaching ? [{ key: 'attach', label: t('Attach from a local terminal', '在本地终端接入'), icon: <TerminalIcon size={15} />, onClick: () => setSharing(true) }] : []),
    ...(current && !native ? [{ key: 'settings', label: t('Session settings', '会话设置'), icon: <GearIcon size={15} />, onClick: () => setSessionSettingsOpen(true) }] : []),
  ];
  function questionHint(call: ToolCall): string | undefined {
    if (call.status === 'missing') return t('No answer recorded', '未记录回答');
    if (native) return t('No answer recorded', '未记录回答');
    if (shared) return ({ web: t('Submitted, waiting for Claude to continue', '已提交，等待 Claude 继续'), handoff: t('Handed to the terminal; answer there', '已交给终端，请在终端作答'), terminal: t('Handled in the terminal', '已在终端处理'), expired: t('Timed out on the web; answer in the terminal', '网页作答已超时，请在终端作答') } as Record<string, string>)[outcomes.get(call.seq) || ''] || t('Waiting for an answer (answer in the terminal)', '等待回答（请在终端作答）');
    // Managed sessions answer through the interactive request card below; don't show it twice.
    if (requests.some(request => request.kind === (call.name === 'ExitPlanMode' ? 'plan' : 'question'))) return undefined;
    return t('Waiting for an answer', '等待回答');
  }
  useEffect(() => { setCopyStatus('idle'); }, [sharing, attachCommand]);
  async function copyAttach() {
    setCopying(true);
    try { await copyText(attachCommand); setCopyStatus('copied'); }
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
  useEffect(() => { if (connected) api<{ permissionMode?: string }>('/api/claude/settings').then(data => setClaudeMode(data.permissionMode || '')).catch(() => {}); }, [connected, settingsOpen, api]);
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
  // New session: the directories of recent sessions (CC Remote's and Claude Code's
  // history), the most recent one filled in while the path is still the default.
  const [recent, setRecent] = useState<RecentProject[]>([]);
  useEffect(() => {
    if (!create) return;
    let active = true;
    Promise.all([api<Session[]>('/api/sessions').catch(() => []), api<Session[]>('/api/native/sessions').catch(() => [])]).then(([own, history]) => {
      if (!active) return;
      const list = recentProjects([...own, ...history]);
      setRecent(list);
      if (list.length) setCwd(old => !old.trim() || old === info?.cwd ? list[0].path : old);
    });
    return () => { active = false; };
  }, [create, api, info?.cwd]);
  // Queued messages of a terminal session come back into the message box for editing.
  function withdrawQueued() {
    void action(async () => {
      const data = await api<{ text: string }>(`/api/sessions/${selected}/queue/withdraw`, { method: 'POST' });
      if (data.text) setDraft([data.text, draft].filter(part => part.trim()).join('\n\n'));
      await list();
      (document.querySelector('.composer textarea') as HTMLTextAreaElement | null)?.focus();
    });
  }
  function stopTurn() { void action(async () => { await api(`/api/sessions/${selected}/stop`, { method: 'POST' }); await list(); }); }
  // Esc stops the running turn, as in the terminal (in a terminal session a second Esc
  // soon after acts as Ctrl+C). Dialogs, menus, an input method and the command list
  // keep their own Esc.
  const stopKey = useRef(stopTurn); stopKey.current = stopTurn;
  const canStop = running && !busy && !native;
  useEffect(() => {
    if (!canStop) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented || event.isComposing || event.keyCode === 229) return;
      if (document.querySelector('.modal-backdrop, .popover-menu, [role="dialog"], .command-menu')) return;
      event.preventDefault();
      stopKey.current();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [canStop]);
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
  // The session's permission mode: its launch options and override, then the CC Remote and Claude defaults.
  const sessionMode = current?.claudeArgs?.includes('--dangerously-skip-permissions') ? 'bypassPermissions' : current?.permissionMode || defaults?.permissionMode || claudeMode;
  const sharedCard = (call: ToolCall | undefined, request: Request) => <SharedQuestion key={request.id} request={request} call={call} busy={busy} sessionMode={sessionMode} draft={activeTab?.questionDrafts[request.id]} onAnswer={answerRequest} onDraft={updateQuestionDraft} />;
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
  // Claude Code asks before switching a conversation's model: the prompt cache
  // is per model, so the next message reads the whole history again.
  const confirmModelSwitch = (x: Session, model: string) => !x.live?.contextTokens || window.confirm(t(`Switch to ${model}?\nThe conversation's cache only works with the current model; after switching, the next message reads the whole history again (about ${Math.round(x.live.contextTokens / 1000)}k tokens), which is slower and uses more tokens.`, `切换到 ${model}？\n当前对话的缓存只对原模型有效，切换后下一条消息会重新读取全部历史（约 ${Math.round(x.live.contextTokens / 1000)}k tokens），更慢也更耗 token。`));

  // Switches the model or effort from the chip list: a terminal session gets
  // the command typed into Claude Code; a managed one changes its next run.
  async function quickPick(kind: QuickKind, value: string) {
    const x = current; const key = activeKey;
    if (!x) return;
    await action(async () => {
      if (x.mode === 'terminal') {
        if (kind === 'model' && !confirmModelSwitch(x, value)) return;
        await api(`/api/sessions/${x.id}/messages`, { method: 'POST', body: JSON.stringify({ text: `/${kind} ${value}` }) });
        updateTab(key, tab => ({ ...tab, notice: t(`Ran /${kind} ${value} in the terminal`, `已在终端执行 /${kind} ${value}`) }));
        return;
      }
      const item = await api<Session>(`/api/sessions/${x.id}`, { method: 'PATCH', body: JSON.stringify({ permissionMode: x.permissionMode || '', model: kind === 'model' ? value : x.model || '', effort: kind === 'effort' ? value : x.effort || '', confirmDangerous: dangerousMode(x.permissionMode || '') }) });
      setSessions(old => old.map(entry => entry.id === item.id ? { ...entry, ...item } : entry));
      updateTab(key, tab => tab.session ? { ...tab, session: { ...tab.session, ...item } } : tab);
    });
  }

  // Sends Claude Code's suggested next prompt as a message.
  async function sendSuggestion(text: string) {
    const id = selected;
    await action(async () => {
      await api(`/api/sessions/${id}/messages`, { method: 'POST', body: JSON.stringify({ text }) });
      await list();
    });
  }

  async function send(event: FormEvent) {
    event.preventDefault();
    const text = draft.trim(); const isCommand = text.startsWith('/');
    if ((!text && !attachments.length) || busy || uploading || (!isCommand && current?.mode === 'channel' && !current.online)) return;
    const key = activeKey; const id = selected; const sentImages = attachments;
    await action(async () => {
      if (isCommand && /^\/(model|effort)$/.test(text) && current?.mode !== 'channel') {
        const box = document.querySelector('.composer')?.getBoundingClientRect();
        if (box) setQuick({ kind: text.slice(1) as QuickKind, anchor: new DOMRect(box.left + 12, box.top, 0, 0) });
        updateTab(key, tab => ({ ...tab, draft: '' }));
        return;
      }
      if (isCommand && current?.mode === 'terminal' && /^\/model\s+\S/.test(text) && !confirmModelSwitch(current, text.slice(7).trim())) return;
      if (isCommand) {
        if (attachments.length) throw new Error(t('Remove the attachments before running a command, or send them with a regular message', '请先移除附件再执行控制命令，或发送描述附件的普通消息'));
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
  if (!connected) return <main className="login"><form className="login-card" onSubmit={event => { event.preventDefault(); if (token === credential) api<Info>('/api/info').then(data => { setInfo(data); setConnected(true); setError(''); localStorage.setItem('cc-token', token); }).catch(err => setError(err.message)); else { setError(''); setToken(credential.trim()); } }}><div className="brand-symbol">↗</div><span className="eyebrow">YOUR WORKSPACE, ANYWHERE</span><h1>CC Remote</h1><span className="login-host"><span className="login-host-dot" />{host.name}</span><p>{t('Continue your Claude Code work from your phone.', '从手机继续你的 Claude Code 工作。')}</p><label>{t('Host access token', '宿主访问令牌')}<input type="password" value={credential} onChange={event => setCredential(event.target.value)} placeholder={t('Enter the token configured when starting the host', '输入启动宿主时配置的令牌')} autoComplete="current-password" required /></label>{error && <div className="error" role="alert">{error}</div>}<button className="primary">{t('Connect to host', '连接宿主')} <span>→</span></button><small>{t('Connects to this address · the token is saved on this device and cleared when you sign out', '连接至当前地址 · 令牌保存在此设备，退出登录时清除')}</small></form></main>;
  return <div className={`app ${sidebarCollapsed ? 'sidebar-collapsed' : ''}`} style={{ '--sidebar-width': `${sidebar.width}px` } as CSSProperties}>
    {drawer && <button className="backdrop" aria-label={t('Close session list', '关闭会话列表')} onClick={() => setDrawer(false)} />}
    <aside className={`sidebar ${drawer ? 'open' : ''}`}><div className="brand"><div className="brand-symbol">↗</div><div className="brand-text"><div className="brand-name"><span title={`${host.name} · CC Remote`}>{host.name}</span><span className="version-pill" title={t(`Host version ${hostVersion}`, `宿主版本 ${hostVersion}`)}>{hostVersion}</span></div><span className="brand-status" title={info?.claudeVersion || (info?.claudeAvailable ? t('Claude Code connected', 'Claude Code 已连接') : t('Claude Code not found', '未找到 Claude Code'))}><span className={`dot ${info?.claudeAvailable ? 'idle' : 'error'}`} /><span>{info?.claudeAvailable ? t('Claude Code connected', 'Claude Code 已连接') : t('Claude Code not found', '未找到 Claude Code')}</span></span></div><button className="icon brand-action" title={t('Sign out', '退出登录')} aria-label={t('Sign out', '退出登录')} onClick={() => { localStorage.removeItem('cc-token'); sessionStorage.removeItem('cc-token'); clearTimeout(persistTimer.current); persistTimer.current = 0; draftChanges.current = {}; pendingLayout.current = undefined; pendingViews.current = ''; savedDrafts.current.clear(); savedLayout.current = ''; savedViews.current = ''; clearUI(); setRestored(false); setRouteNotice(''); history.replaceState(null, '', cleanURL()); setToken(''); setConnected(false); setCredential(''); setError(''); setSelected(''); setSessions([]); setTabs([]); }}><LogoutIcon size={16} /></button><button className="mobile icon brand-action" aria-label={t('Close session list', '关闭会话列表')} onClick={() => setDrawer(false)}><CloseIcon size={18} /></button></div>
      <div className="sidebar-actions"><button className="new-session primary-action" title={t('New session', '新建会话')} aria-label={t('New session', '新建会话')} onClick={() => setCreate(true)}><PlusIcon size={15} /><span>{t('New', '新建')}</span></button></div>
      <div className="tabs"><button className={!native ? 'active' : ''} onClick={() => switchTab(false)}>{t('Sessions', '会话')}</button><button className={native ? 'active' : ''} onClick={() => switchTab(true)}>{t('Terminal history', '终端历史')}</button></div>
      <input className="session-search" aria-label={t('Search sessions', '搜索会话')} placeholder={t('Search by name or project directory', '搜索名称或项目目录')} value={search} onChange={event => setSearch(event.target.value)} />
      <div className="session-list-heading"><span>{native ? t('Terminal history', '终端历史') : t('All sessions', '全部会话')}</span><span title={t('Kept in this page; switching keeps drafts and scroll position', '缓存在当前页面内，切换保留草稿和阅读位置')}>{t(`Cached ${cachedCount}`, `已缓存 ${cachedCount}`)}</span></div>
      <div className="session-list">{filteredSessions.length === 0 ? <p className="muted empty-list">{search.trim() ? t('No matching sessions', '没有匹配的会话') : native ? t('No terminal history to read yet', '暂无可读取的终端历史') : t('Start your first session', '开始你的第一个会话')}</p> : filteredSessions.slice(0, listLimit).map(item => {
        const state = sidebarState(item);
        return <div key={item.id} className={`session-row ${selected === item.id ? 'selected' : ''} ${state.cached ? 'cached' : ''}`}>
          <button className="session-item" aria-current={selected === item.id ? 'page' : undefined} onClick={() => choose(item.id)}>
            <div><span className={`dot ${state.status}`} /><strong>{item.title || t('Untitled session', '未命名会话')}</strong></div>
            <small title={item.cwd}>{item.cwd}</small>
            <span className="session-meta"><span className={`session-state ${state.status}`}>{!native && `${modeLabel(item.mode)} · `}{state.label}</span>{state.cached && <span className="cache-marker" title={t('Switching sessions keeps this page\'s messages, drafts and scroll position', '切换会话时保留当前页面的消息、草稿和阅读位置')}>{state.cached.loaded ? t('Cached', '已缓存') : t('Loading', '加载中')}</span>}</span>
          </button>
          {state.cached && <button className="release-cache" title={t('Release this session\'s page cache; unsent drafts are cleared, the host task keeps running', '释放此会话的页面缓存，未发送草稿将清除，宿主任务继续运行')} aria-label={t(`Release the cache of ${item.title || 'session'}`, `释放 ${item.title || '会话'} 的缓存`)} onClick={() => closeTab(state.cached!.key)}><CloseIcon size={13} /></button>}
          {!native && <button className="delete-session" title={t('Delete this session\'s CC Remote records', '删除此会话的 CC Remote 记录')} aria-label={t(`Delete ${item.title || 'session'}`, `删除 ${item.title || '会话'}`)} onClick={() => { setDeleting(item); setDrawer(false); }}><TrashIcon size={13} /></button>}
        </div>;
      })}{filteredSessions.length > listLimit && <button type="button" className="list-more" onClick={() => setListLimit(n => n + LIST_RENDER)}>{t(`Show more (${filteredSessions.length - listLimit} left)`, `显示更多（还有 ${filteredSessions.length - listLimit} 个）`)}</button>}</div>
      <nav className="sidebar-nav" aria-label={t('Global', '全局')}><button title={t('Settings', '设置')} onClick={() => { setSettingsOpen(true); setDrawer(false); }}><GearIcon size={15} /><span>{t('Settings', '设置')}</span></button><button title={t('Host status', '服务状态')} onClick={() => { setStatusOpen(true); setDrawer(false); }}><StatusIcon size={15} /><span>{t('Host status', '服务状态')}</span></button></nav>
    </aside>
    {!sidebarCollapsed && <SidebarResizer width={sidebar.width} max={sidebar.max} onChange={sidebar.set} />}
    <main className="workspace"><header className="topbar"><button className="desktop icon" aria-label={sidebarCollapsed ? t('Expand session list', '展开会话列表') : t('Collapse session list', '折叠会话列表')} title={sidebarCollapsed ? t('Expand session list', '展开会话列表') : t('Collapse session list', '折叠会话列表')} onClick={() => setSidebarCollapsed(old => { localStorage.setItem('cc-sidebar-collapsed', String(!old)); return !old; })}><MenuIcon size={18} /></button><button className="mobile icon" aria-label={t('Open session list', '打开会话列表')} onClick={() => setDrawer(true)}><MenuIcon size={18} /></button><div className="header-title"><h1>{current?.title || t('Workspace', '工作空间')}</h1><div className="header-sub"><span className="badge mobile-badge">{headerStatus}</span>{current && !native && <SessionChips session={current} defaults={defaults} open={() => setSessionSettingsOpen(true)} quick={(kind, anchor) => setQuick(old => old?.kind === kind ? null : { kind, anchor })} />}<p title={current?.cwd || info?.cwd}>{current?.cwd || info?.cwd}</p></div></div><span className="badge desktop-badge">{headerStatus}</span>{selected && <button className={`icon topbar-extra${toolsExpanded ? ' active' : ''}`} title={toolsExpanded ? t('Collapse all tool calls', '折叠全部工具调用') : t('Expand all tool calls', '展开全部工具调用')} aria-label={toolsExpanded ? t('Collapse all tool calls', '折叠全部工具调用') : t('Expand all tool calls', '展开全部工具调用')} aria-pressed={toolsExpanded} onClick={toggleAllTools}>{toolsExpanded ? <CollapseAllIcon size={17} /> : <ExpandAllIcon size={17} />}</button>}{tasks.length > 0 && <button className={`icon task-button${runningTasks ? ' active' : ''}`} title={runningTasks ? t(`${runningTasks} tasks running`, `${runningTasks} 个任务运行中`) : t('Subagents and background tasks', '子 agent 与后台任务')} aria-label={t('Subagents and background tasks', '子 agent 与后台任务')} onClick={() => setTasksOpen(true)}><AgentIcon size={17} />{runningTasks > 0 && <span className="task-count">{runningTasks}</span>}</button>}<button className="icon topbar-extra" title={t('Refresh sessions', '刷新会话')} aria-label={t('Refresh sessions', '刷新会话')} onClick={() => action(list)}><RefreshIcon size={17} /></button>{attaching && <button className="icon topbar-extra" title={t('Attach from a local terminal', '在本地终端接入')} aria-label={t('Attach from a local terminal', '在本地终端接入')} onClick={() => setSharing(true)}><TerminalIcon size={17} /></button>}{current && !native && <button className="icon topbar-extra" title={t('Session settings', '会话设置')} aria-label={t('Session settings', '会话设置')} onClick={() => setSessionSettingsOpen(true)}><GearIcon size={17} /></button>}<TopbarMenu actions={topbarActions} /></header>
      {plainHttp && <div className="plain-http-banner banner" role="status">{t(`You are connected over plain HTTP (${location.host}); the access token and session content travel the network unencrypted. Use an HTTPS reverse proxy or a VPN instead.`, `当前通过明文 HTTP 访问（${location.host}），访问令牌和会话内容会在网络中明文传输。建议改用 HTTPS 反向代理或 VPN。`)}<button className="icon-button" onClick={dismissPlainHttp} aria-label={t('Dismiss plain HTTP warning', '关闭明文访问提示')}><CloseIcon size={14} /></button></div>}
      {error && <div className="error banner" role="alert">{error}<button className="icon-button" onClick={() => setError('')} aria-label={t('Dismiss error', '关闭错误')}><CloseIcon size={14} /></button></div>}
      {routeNotice && <div className="command-notice" role="status"><span>{routeNotice}</span><button className="icon-button" aria-label={t('Dismiss notice', '关闭提示')} onClick={() => setRouteNotice('')}><CloseIcon size={14} /></button></div>}
      {activeTab?.notice && <div className="command-notice" role="status"><span>{activeTab.notice}</span><button className="icon-button" aria-label={t('Dismiss command result', '关闭命令结果')} onClick={() => updateTab(activeKey, tab => ({ ...tab, notice: undefined }))}><CloseIcon size={14} /></button></div>}{settingsOpen && <ClaudeSettings api={api} close={() => setSettingsOpen(false)} />}{tasksOpen && <TaskPanel tasks={tasks} close={() => setTasksOpen(false)} locate={locateTask} />}{statusOpen && <StatusPanel api={api} close={() => setStatusOpen(false)} select={id => { setStatusOpen(false); choose(id, false); }} />}{quick && current && !native && <QuickSwitch api={api} sessionId={current.id} target={quick} current={quick.kind === 'model' ? (current.mode === 'terminal' ? current.live?.modelId || current.model || '' : current.model || '') : (current.mode === 'terminal' ? current.live?.effort || current.effort || '' : current.effort || '')} inherit={current.mode === 'terminal' ? undefined : quick.kind === 'model' ? defaults?.model || '' : defaults?.effort || ''} close={() => setQuick(null)} pick={value => void quickPick(quick.kind, value)} />}{sessionSettingsOpen && current && !native && <SessionSettings api={api} session={current} defaults={defaults} close={() => setSessionSettingsOpen(false)} onDelete={() => { setSessionSettingsOpen(false); setDeleting(current); }} onUpdated={item => { setSessions(old => old.map(entry => entry.id === item.id ? { ...entry, ...item } : entry)); updateTab(activeKey, tab => tab.session ? { ...tab, session: { ...tab.session, ...item } } : tab); }} />}{deleting && <DeleteSessionDialog session={sessions.find(item => item.id === deleting.id) || deleting} close={() => setDeleting(undefined)} confirm={stop => deleteSession(deleting, stop)} />}{!selected ? <div className="welcome"><div className="welcome-icon">⌘</div><span className="eyebrow">READY WHEN YOU ARE</span><h2>{t('Take your work with you.', '把工作带到身边。')}</h2><p>{t('Pick a session, or start a new task in a project directory.', '选择一个会话，或在项目目录中开始新的任务。')}<br/>{t('Sessions keep running on the host; close the page and come back later.', '会话持续在宿主运行，关闭页面也可以稍后回来。')}</p><div className="welcome-actions"><button className="primary" onClick={() => setCreate(true)}><PlusIcon size={15} />{t('New session', '新建会话')}</button></div>{!info?.claudeAvailable && <p className="error">{t('Install Claude Code on the host and sign in first.', '请先在宿主安装 Claude Code 并完成登录。')}</p>}</div> : <>
        <div className="conversation" ref={scroll} onScroll={() => { const element = scroll.current; if (element && !loading) { nearBottom.current = element.scrollHeight - element.scrollTop - element.clientHeight < 120; updateTab(activeKey, tab => ({ ...tab, scrollTop: element.scrollTop, nearBottom: nearBottom.current })); } }}>
          {loading && <div className="muted loading">{t('Loading session…', '正在加载会话…')}</div>}
          {!loading && !activeTab?.hasOlder && activeTab?.session?.truncated && <div className="older-marker muted">{t('This transcript is over 64 MiB; only its most recent part is shown.', '这个会话的记录超过 64 MiB，只显示最近的部分。')}</div>}
          {!loading && activeTab?.hasOlder && <div ref={olderMarker} className="older-marker">{activeTab.loadingOlder ? t('Loading earlier messages…', '正在加载更早的消息…') : <button type="button" onClick={() => void loadOlder()}>{t('Load earlier messages', '加载更早的消息')}</button>}</div>}
          {!loading && events.length === 0 && <div className="conversation-empty"><h2>{t('Ready to start', '准备开始')}</h2><p>{t('Describe your task; Claude works in this project directory.', '描述你的任务，Claude 会在这个项目目录中工作。')}</p></div>}
          {/* One child list: a shared card keyed by its request id moves between the transcript and the pending list without remounting. */}
          {[...transcript.map(item => {
            if (item.type === 'agent') return <AgentCard key={item.key} call={item.call} items={item.items} nested={nested} />;
            if (item.type === 'tools') return <ToolGroup key={item.key} groupKey={item.key} calls={item.calls} open={activeTab?.expandedGroups[item.key] ?? toolsExpanded} onToggle={toggleGroup} openCalls={activeTab?.expandedTools || NO_CALLS} onToggleCall={toggleCall} onLoadFull={loadFull} />;
            if (item.type === 'special') {
              const request = linked.get(item.call.seq);
              if (request) return sharedCard(item.call, request);
              const hint = questionHint(item.call); const outcome = outcomes.get(item.call.seq);
              return item.call.status === 'ok' || item.call.status === 'error' || hint ? <QuestionCard key={item.key} call={item.call} waitingHint={hint || ''} answeredBy={outcome === 'web' ? 'web' : outcome === 'terminal' || outcome === 'handoff' ? 'terminal' : undefined} /> : null;
            }
            const event = item.event;
            if (event.kind === 'recap') return <aside className="recap" key={event.seq} aria-label={t('Claude\'s recap', 'Claude 的回顾')}><div className="recap-label">{t('※ Recap · while you were away', '※ 回顾 · 你离开期间')}</div><Markdown text={event.text || ''} /></aside>;
            if (event.kind === 'compact') return <div className="compact-divider" role="separator" key={event.seq}><span>{compactSummary(event.payload)}</span></div>;
            return <article className={`message ${event.kind}`} key={event.seq}><div className="message-label">{event.kind === 'user' ? t('YOU', '你') : event.kind === 'error' ? t('Error', '执行错误') : 'CLAUDE'}</div><Markdown text={event.text || ''} />{event.payload?.attachments?.length > 0 && <AttachmentList items={event.payload.attachments} token={token} />}</article>;
          }),
          // A request whose call already shows a terminal result is about to be withdrawn; don't show it twice.
          ...(native ? [] : requests.filter(request => !linkedIds.has(request.id) && !(shared && requestSettled(request, transcript))).map(request => shared && (request.kind === 'question' || request.kind === 'plan') ? sharedCard(undefined, request) : <RequestCard key={request.id} request={request} busy={busy} answers={activeTab?.answers[request.id] || {}} setAnswers={update => updateTab(activeKey, tab => ({ ...tab, answers: { ...tab.answers, [request.id]: update(tab.answers[request.id] || {}) } }))} onAnswer={(decision, answers) => action(async () => { await api(`/api/requests/${request.id}`, { method: 'POST', body: JSON.stringify({ decision, answers }) }); updateTab(activeKey, tab => ({ ...tab, requests: tab.requests.filter(item => item.id !== request.id) })); })} />))]}
          {!native && current?.queued?.map((item, index) => <article className="message user queued" key={`queued-${index}`}><div className="message-label">{t('YOU · queued', '你 · 等待处理')}</div><Markdown text={item.text} />{item.payload?.attachments?.length > 0 && <AttachmentList items={item.payload.attachments} token={token} />}</article>)}
          {!native && current?.mode === 'terminal' && (current.queued?.length || 0) > 0 && <div className="queued-actions"><button type="button" className="secondary" disabled={busy} onClick={withdrawQueued}>{t('Withdraw to edit', '撤回编辑')}</button><small>{t('Takes the queued messages back into the message box, like pressing Up in the terminal; the current turn keeps running.', '把排队消息取回输入框修改，和终端里按 ↑ 一样；当前任务不受影响。')}</small></div>}
          {running && !requests.length && <div className="working"><span className="pulse" />{isCompacting(events) ? t('Compacting context…', '正在压缩上下文…') : current?.live?.activity || t('Claude is working…', 'Claude 正在工作…')}</div>}
          {!running && runningTasks > 0 && <button type="button" className="working working-tasks" onClick={() => setTasksOpen(true)}><span className="pulse" />{t(`${runningTasks} background tasks running · View`, `${runningTasks} 个后台任务运行中 · 查看`)}</button>}<div ref={bottom} />
        </div>
        {native ? <div className="native-footer"><p>{t('Keeps the original session title and context. As a terminal session the host runs it: attach from a local terminal with ccr and switch between the terminal and the web at any time.', '沿用原会话标题和上下文。以终端会话继续后由宿主运行，本地终端用 ccr 接入，终端与网页可随时切换。')}</p><button className="primary" onClick={() => setImporting('terminal')}>{t('Continue as terminal session ↔', '以终端会话继续 ↔')}</button><button className="secondary" onClick={() => setImporting('managed')}>{t('Import as managed session', '导入为托管会话')}</button></div> : <form className={`composer${dragging ? ' dragging' : ''}`} onSubmit={send} onDragOver={event => { if (!event.dataTransfer.types.includes('Files')) return; event.preventDefault(); event.dataTransfer.dropEffect = 'copy'; setDragging(true); }} onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node)) setDragging(false); }} onDrop={event => { if (!event.dataTransfer.types.includes('Files')) return; event.preventDefault(); setDragging(false); void addFiles(Array.from(event.dataTransfer.files)); }}>{dragging && <div className="drop-hint" aria-hidden="true">{t('Drop to attach (≤ 20 MiB each, up to 8 per message)', '松开以添加附件（单个 ≤ 20 MiB，每条最多 8 个）')}</div>}{draft.startsWith('/') && !draft.includes(' ') && <div className="command-menu" role="listbox" aria-label={t('Command suggestions', '命令建议')}>{commands.filter(command => command.name.startsWith(draft.slice(1))).map(command => <button type="button" key={command.name} onClick={() => { if ((command.name === 'model' || command.name === 'effort') && current?.mode !== 'channel') { const box = document.querySelector('.composer')?.getBoundingClientRect(); setDraft(''); if (box) setQuick({ kind: command.name, anchor: new DOMRect(box.left + 12, box.top, 0, 0) }); return; } setDraft(`/${command.name} `); }}><strong>/{command.name}</strong><small>{command.description}{command.terminalOnly ? t(' · in the terminal', ' · 终端操作') : ''}</small></button>)}</div>}{attachments.length > 0 && <AttachmentList items={attachments} token={token} remove={id => updateTab(activeKey, tab => ({ ...tab, attachments: tab.attachments.filter(item => item.id !== id) }))} />}{current?.mode === 'terminal' && !running && !draft && current.live?.suggestion && <div className="suggestion-bar"><span className="suggestion-label">{t('Suggested', '建议')}</span><button type="button" className="suggestion-text" title={t('Send this suggestion', '发送这条建议')} disabled={busy} onClick={() => void sendSuggestion(current.live!.suggestion!)}>{current.live.suggestion}</button><button type="button" className="suggestion-edit" title={t('Put it in the message box to edit', '填入输入框后修改')} onClick={() => setDraft(current.live!.suggestion!)}>{t('Edit', '编辑')}</button></div>}<ComposerInput aria-label={t('Message to Claude', '发送给 Claude 的消息')} placeholder={current?.mode === 'channel' && !current.online ? t('The terminal is offline; run the connect command on the host again first', '终端离线，请先在宿主重新启动连接命令') : running ? t('Keep typing; the message is queued after the current task', '可以继续输入，消息会排在当前任务之后') : t('Describe a task, or ask a follow-up…', '描述任务，或继续提问…')} value={draft} onChange={event => setDraft(event.target.value)} onPaste={event => { const files = Array.from(event.clipboardData.files); if (files.length) { event.preventDefault(); void addFiles(files); } }} onCompositionStart={() => { composing.current = true; }} onCompositionEnd={() => { composing.current = false; }} onKeyDown={event => { if (event.key !== 'Enter' || event.nativeEvent.isComposing || composing.current || event.keyCode === 229) return; const modifier = event.ctrlKey || event.metaKey; if (!event.shiftKey && !event.altKey && (sendKey === 'enter' ? !modifier : modifier)) { event.preventDefault(); void send(event); } }} /><div className="composer-bottom"><button type="button" className="slash-button" title={t('Commands', '命令')} aria-label={t('Command list', '命令列表')} onClick={() => { setDraft(draft.startsWith('/') ? '' : '/'); (document.querySelector('.composer textarea') as HTMLTextAreaElement | null)?.focus(); }}>/</button><AttachButton disabled={uploading || busy} upload={files => void addFiles(files)} /><span><Select className="send-key" label={t('Send shortcut', '发送快捷键')} title={t('Shift + Enter always adds a new line; nothing is sent while an input method is composing', 'Shift + Enter 始终换行；中文输入法选词时不会发送')} value={sendKey} options={SEND_KEYS} onChange={value => { setSendKey(value); localStorage.setItem('cc-send-key', value); }} /></span><div className="composer-actions">{running && <button type="button" className="stop" disabled={busy} title={t('Stop the current turn (Esc); queued messages still run', '停止当前轮次（Esc）；排队的消息会继续执行')} onClick={stopTurn}>{t('■ Stop', '■ 停止')}</button>}{(!running || draft.trim() || attachments.length > 0) && <button className="primary" disabled={busy || uploading || (!draft.trim() && !attachments.length) || (!draft.startsWith('/') && (!info?.claudeAvailable || (current?.mode === 'channel' && !current.online)))}>{uploading ? t('Uploading…', '上传中…') : running && !draft.startsWith('/') ? t('Queue ↑', '排队 ↑') : t('Send ↑', '发送 ↑')}</button>}</div></div></form>}
      </>}
    </main>
    {sharing && attaching && <div className="modal-backdrop"><div className="modal"><div className="modal-heading"><h2>{t('Attach from a local terminal', '在本地终端接入')}</h2><button className="icon" aria-label={t('Close attach instructions', '关闭接入说明')} onClick={() => setSharing(false)}><CloseIcon size={18} /></button></div><p>{t('Run this command on the host to continue the session in a local terminal, just like running Claude Code directly.', '在宿主上运行以下命令，就能在本地终端里继续这个会话，和直接运行 Claude Code 一样。')}</p><textarea className="terminal-command" aria-label={t('Attach command', '接入命令')} readOnly rows={3} value={attachCommand} onFocus={event => event.currentTarget.select()} /><div className="share-actions"><button type="button" className="secondary" disabled={copying || !attachCommand} onClick={() => void copyAttach()}>{copying ? t('Copying…', '复制中…') : copyStatus === 'copied' ? t('Copied', '已复制') : t('Copy command', '复制命令')}</button><span role="status" className={`copy-feedback ${copyStatus === 'failed' ? 'failed' : ''}`}>{copyStatus === 'copied' ? t('Command copied to the clipboard', '命令已复制到剪贴板') : copyStatus === 'failed' ? COPY_FAILED : ''}</span></div><p>{t('Press Ctrl+] to detach. Detaching only leaves the terminal view; Claude Code and its subagents keep running, and you can switch between the web and the terminal at any time.', '按 Ctrl+] 断开。断开只是离开终端界面，Claude Code 和其中的子 agent 会继续运行，网页和本地可以随时切换。')}</p><small>{t('Quicker: run ', '更快的方式：在项目目录运行 ')}<code>ccr</code>{t(' in the project directory to attach to its terminal session (a new one if there is none); ', '，接入该目录的终端会话（没有就新建）；')}<code>ccr ls</code>{t(' lists recent sessions to pick from. Terminal sessions end when the host restarts and resume their context the next time you use them.', ' 列出最近的会话供选择。宿主重启时终端会话会结束，下次使用时自动恢复上下文。')}</small></div></div>}    {create && <div className="modal-backdrop"><form className="modal" onSubmit={event => { event.preventDefault(); action(async () => { if (!confirmMode(newOverrides.permissionMode, '')) return; const item = await api<Session>('/api/sessions', { method: 'POST', body: JSON.stringify({ title: title.trim(), cwd: cwd.trim(), ...newOverrides, model: newOverrides.model.trim(), confirmDangerous: dangerousMode(newOverrides.permissionMode), ...(newMode === 'terminal' ? { mode: 'terminal', trustFolder } : {}) }) }); try { localStorage.setItem('cc-new-mode', newMode); } catch {} setTrustFolder(false); setNative(false); setSessions(old => [item, ...old]); choose(item.id, false); setCreate(false); setTitle(''); }); }}><div className="modal-heading"><h2>{t('New session', '新建会话')}</h2><button type="button" className="icon" aria-label={t('Close', '关闭')} onClick={() => setCreate(false)}><CloseIcon size={18} /></button></div><p>{t('Choose a project directory on the host for the task.', '为任务选择宿主上的项目目录。')}</p><div className="mode-choice" role="radiogroup" aria-label={t('Session type', '会话类型')}><button type="button" role="radio" aria-checked={newMode === 'terminal'} className={newMode === 'terminal' ? 'active' : ''} onClick={() => setNewMode('terminal')}><strong>{t('Terminal session', '终端会话')}</strong><small>{t('The host holds an interactive Claude Code; a local terminal can attach any time, and switching never interrupts the task', '宿主持有交互式 Claude Code，本地终端可随时接入，切换不中断任务')}</small></button><button type="button" role="radio" aria-checked={newMode === 'managed'} className={newMode === 'managed' ? 'active' : ''} onClick={() => setNewMode('managed')}><strong>{t('Managed session', '托管会话')}</strong><small>{t('The host runs Claude in the background; best if you only use the web', '宿主在后台运行 Claude，适合只在网页上使用')}</small></button></div><label>{t('Session name', '会话名称')}<input value={title} onChange={event => setTitle(event.target.value)} placeholder={t('e.g. Fix the login page', '例如：修复登录页面')} required autoFocus /></label><label>{t('Absolute project path', '项目绝对路径')}<input value={cwd} onChange={event => setCwd(event.target.value)} placeholder={t('D:\\Projects\\app or /home/me/app', 'D:\\Projects\\app 或 /home/me/app')} required /></label>{recent.length > 0 && <div className="recent-projects" role="group" aria-label={t('Recent projects', '最近项目')}><span className="eyebrow">{t('RECENT PROJECTS', '最近项目')}</span>{matchProjects(recent, cwd).map(item => <button type="button" key={item.path} className={projectKey(item.path) === projectKey(cwd) ? 'active' : ''} title={item.path} onClick={() => setCwd(item.path)}><strong>{item.name}</strong><small>{item.path}</small></button>)}</div>}<small>{t('The directory must already exist; Claude works in it.', '目录需要已经存在，Claude 将在此目录中执行操作。')}</small>{newMode === 'terminal' && <label className="checkbox"><input type="checkbox" checked={trustFolder} onChange={event => setTrustFolder(event.target.checked)} required />{t('Trust this folder: Claude Code may read, edit and execute files in it (the same as confirming "trust this folder" in the terminal)', '信任此目录：Claude Code 可以读取、修改和执行其中的文件（等同于在终端里确认「信任此文件夹」）')}</label>}<div className="new-session-overrides"><label>{t('Permission mode', '权限模式')}<Select label={t('New session permission mode', '新会话权限模式')} value={newOverrides.permissionMode} options={permissionOptions(t('Use default', '跟随默认'))} onChange={permissionMode => setNewOverrides(old => ({ ...old, permissionMode }))} /></label><label>{t('Effort', '推理强度')}<Select label={t('New session effort', '新会话推理强度')} value={newOverrides.effort} options={effortOptions(t('Use default', '跟随默认'))} onChange={effort => setNewOverrides(old => ({ ...old, effort }))} /></label></div><label>{t('Model', '模型')}<ModelPicker api={api} label={t('New session model', '新会话模型')} value={newOverrides.model} onChange={model => setNewOverrides(old => ({ ...old, model }))} /></label>{error && <div className="error" role="alert">{error}</div>}<button className="primary" disabled={busy}>{busy ? t('Creating…', '创建中…') : t('Create session →', '创建会话 →')}</button></form></div>}
    {importing && <div className="modal-backdrop"><div className="modal"><div className="modal-heading"><h2>{importing === 'terminal' ? t('Continue as terminal session', '以终端会话继续') : t('Import as managed session', '导入为托管会话')}</h2><button className="icon" aria-label={t('Close', '关闭')} onClick={() => setImporting('')}><CloseIcon size={18} /></button></div><p>{t('Running the same session in two places at once makes its state and file operations conflict. Stop the session in its original terminal first.', '同一个会话同时运行会导致状态和文件操作冲突。请先在原终端停止该会话。')}</p><label className="checkbox"><input type="checkbox" checked={stopped} onChange={event => setStopped(event.target.checked)} />{t('I have stopped the Claude Code session in the original terminal', '我已停止原终端中的 Claude Code 会话')}</label>{importing === 'terminal' && <p>{t('Claude Code will run in the session\'s project directory, trusting it as when you resumed it there.', 'Claude Code 将在该会话的项目目录中运行，和在那里恢复会话一样信任此目录。')}</p>}<button className="primary" disabled={!stopped || busy} onClick={() => action(async () => { const item = importing === 'terminal' ? await api<Session>('/api/sessions', { method: 'POST', body: JSON.stringify({ mode: 'terminal', trustFolder: true, resume: selected }) }) : await api<Session>(`/api/native/sessions/${selected}/import`, { method: 'POST', body: JSON.stringify({ confirmStopped: true }) }); setNative(false); setSessions([item]); choose(item.id, false); setImporting(''); })}>{importing === 'terminal' ? t('Continue', '继续') : t('Import', '确认导入')}</button>{error && <div className="error" role="alert">{error}</div>}</div></div>}
  </div>;
}

function RequestCard({ request, busy, onAnswer, answers, setAnswers }: { request: Request; busy: boolean; onAnswer: (decision: string, answers?: Record<string, string>) => void; answers: Record<string, string>; setAnswers: (update: (old: Record<string, string>) => Record<string, string>) => void }) {
  const questions: any[] = request.input?.questions || [];
  return <section className="request"><span className="eyebrow">{t('Needs your action', '需要你的操作')}</span><h3>{request.kind === 'question' ? t('Claude has some questions', 'Claude 有几个问题') : request.kind === 'plan' ? t('Confirm the plan', '确认执行计划') : t(`Allow ${request.toolName || 'tool'}?`, `允许执行 ${request.toolName || '工具'}？`)}</h3>
    {request.kind === 'question' ? questions.map((question, index) => <div className="question" key={index}><p>{question.question}</p><div className="options">{(question.options || []).map((option: any) => <button key={option.label} className={(answers[question.question] || '').split(', ').includes(option.label) ? 'chosen' : ''} disabled={busy} onClick={() => setAnswers(old => { const key = question.question; const values = (old[key] || '').split(', ').filter(Boolean); return { ...old, [key]: question.multiSelect ? (values.includes(option.label) ? values.filter(value => value !== option.label) : [...values, option.label]).join(', ') : option.label }; })}>{option.label}{option.description && <small>{option.description}</small>}</button>)}</div><input aria-label={t(`Answer: ${question.question}`, `回答：${question.question}`)} placeholder={t('Or type your own answer', '也可以输入自己的回答')} value={answers[question.question] || ''} onChange={event => setAnswers(old => ({ ...old, [question.question]: event.target.value }))} /></div>) : <pre>{typeof request.input === 'string' ? request.input : JSON.stringify(request.input, null, 2)}</pre>}
    <div className="request-actions"><button className="secondary" disabled={busy} onClick={() => onAnswer('deny')}>{t('Deny', '拒绝')}</button><button className="primary" disabled={busy || (request.kind === 'question' && questions.some(question => !answers[question.question]?.trim()))} onClick={() => onAnswer('allow', request.kind === 'question' ? answers : undefined)}>{request.kind === 'question' ? t('Submit answers', '提交回答') : t('Allow once', '允许本次操作')}</button></div>
  </section>;
}

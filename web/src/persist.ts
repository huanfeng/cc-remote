// Pure helpers for UI state that survives a page refresh: the URL hash route,
// per-session drafts and the opened-tab list in localStorage, and per-tab view
// state (scroll, tool expansion) in sessionStorage. No DOM or storage access here.
import type { Attachment } from './Attachments';
import type { QuestionDraft } from './QuestionCard';

export const UI_KEY = 'cc-ui:v1';
export const VIEW_KEY = 'cc-ui-view:v1';
export const UI_PREFIX = 'cc-ui';
export const DRAFT_TTL = 7 * 24 * 60 * 60 * 1000;
export const MAX_UI_CHARS = 1 << 20;
const MAX_VIEWS = 64;

export type Route = { id: string; native: boolean };
export type SessionDraft = { draft: string; attachments: Attachment[]; questionDrafts: Record<string, QuestionDraft>; answers: Record<string, Record<string, string>>; updatedAt: number };
/** `open` and `selected` hold tab keys (`session:<id>` / `native:<id>`); drafts are keyed by managed session id. */
export type UIState = { v: 1; open: string[]; selected: string; sidebarTab: 'session' | 'native'; drafts: Record<string, SessionDraft> };
export type ViewState = { scrollTop: number; nearBottom: boolean; expandedTools: number[]; expandedGroups: Record<number, boolean> };

const isObject = (value: unknown): value is Record<string, any> => !!value && typeof value === 'object' && !Array.isArray(value);
const isKey = (value: unknown): value is string => typeof value === 'string' && /^(session|native):./.test(value);

export function parseHash(hash: string): Route | undefined {
  const match = /^#\/(s|native)\/(.+)$/.exec(hash);
  if (!match) return undefined;
  try {
    const id = decodeURIComponent(match[2]);
    return id && !id.includes('/') ? { id, native: match[1] === 'native' } : undefined;
  } catch { return undefined; }
}

export function formatHash(id: string, native: boolean): string {
  return id ? `#/${native ? 'native' : 's'}/${encodeURIComponent(id)}` : '';
}

export function parseKey(key: string): Route | undefined {
  if (!isKey(key)) return undefined;
  const [kind, ...rest] = key.split(':');
  return { id: rest.join(':'), native: kind === 'native' };
}

export const emptyUI = (): UIState => ({ v: 1, open: [], selected: '', sidebarTab: 'session', drafts: {} });

export function draftEmpty(draft: Omit<SessionDraft, 'updatedAt'>): boolean {
  return !draft.draft && !draft.attachments.length && !Object.keys(draft.questionDrafts).length && !Object.keys(draft.answers).length;
}

function cleanAttachment(value: unknown): Attachment | undefined {
  if (!isObject(value) || typeof value.id !== 'string' || typeof value.name !== 'string' || typeof value.url !== 'string' || !value.url.startsWith('/api/sessions/')) return undefined;
  return { id: value.id, name: value.name, url: value.url, mime: typeof value.mime === 'string' ? value.mime : '', size: Number.isFinite(value.size) ? value.size : 0, ...(typeof value.kind === 'string' ? { kind: value.kind } : {}) };
}

function cleanAnswers(value: unknown): Record<string, Record<string, string>> {
  const result: Record<string, Record<string, string>> = {};
  if (!isObject(value)) return result;
  for (const [id, answers] of Object.entries(value)) {
    if (!isObject(answers)) continue;
    const entries = Object.entries(answers).filter(([, text]) => typeof text === 'string');
    if (entries.length) result[id] = Object.fromEntries(entries);
  }
  return result;
}

function cleanDraft(value: unknown): SessionDraft | undefined {
  if (!isObject(value) || !Number.isFinite(value.updatedAt)) return undefined;
  const draft: SessionDraft = {
    draft: typeof value.draft === 'string' ? value.draft : '',
    attachments: Array.isArray(value.attachments) ? value.attachments.map(cleanAttachment).filter((item): item is Attachment => !!item) : [],
    questionDrafts: isObject(value.questionDrafts) ? Object.fromEntries(Object.entries(value.questionDrafts).filter(([, item]) => isObject(item))) as Record<string, QuestionDraft> : {},
    answers: cleanAnswers(value.answers),
    updatedAt: value.updatedAt,
  };
  return draftEmpty(draft) ? undefined : draft;
}

/** Brings any stored shape to the current version; unknown or corrupt data starts over. */
export function migrate(value: unknown): UIState {
  if (!isObject(value) || value.v !== 1) return emptyUI();
  const drafts: Record<string, SessionDraft> = {};
  if (isObject(value.drafts)) for (const [id, item] of Object.entries(value.drafts)) { const draft = cleanDraft(item); if (draft) drafts[id] = draft; }
  return {
    v: 1,
    open: Array.isArray(value.open) ? [...new Set(value.open.filter(isKey))] : [],
    selected: isKey(value.selected) ? value.selected : '',
    sidebarTab: value.sidebarTab === 'native' ? 'native' : 'session',
    drafts,
  };
}

/** Drops drafts untouched for longer than the TTL. */
export function expire(state: UIState, now: number, ttl = DRAFT_TTL): UIState {
  return { ...state, drafts: Object.fromEntries(Object.entries(state.drafts).filter(([, draft]) => now - draft.updatedAt <= ttl)) };
}

/** Drops the oldest drafts until the serialized state fits in `max` characters. */
export function cap(state: UIState, max = MAX_UI_CHARS): UIState {
  let result = state;
  const oldest = Object.entries(state.drafts).sort((a, b) => a[1].updatedAt - b[1].updatedAt).map(([id]) => id);
  while (JSON.stringify(result).length > max && oldest.length) {
    const { [oldest.shift()!]: _, ...drafts } = result.drafts;
    result = { ...result, drafts };
  }
  return result;
}

export function deserialize(raw: string | null | undefined, now: number): UIState {
  if (!raw) return emptyUI();
  try { return expire(migrate(JSON.parse(raw)), now); } catch { return emptyUI(); }
}

export function serialize(state: UIState, now: number, max = MAX_UI_CHARS): string {
  return JSON.stringify(cap(expire(state, now), max));
}

/**
 * Applies this page's changes onto the latest stored state: the layout fields are
 * replaced, and only the drafts this page changed are written (`null` deletes), so
 * another browser tab's drafts for other sessions are never clobbered.
 */
export function mergeUI(stored: UIState, layout: Pick<UIState, 'open' | 'selected' | 'sidebarTab'> | undefined, changes: Record<string, SessionDraft | null>): UIState {
  const drafts = { ...stored.drafts };
  for (const [id, draft] of Object.entries(changes)) { if (draft && !draftEmpty(draft)) drafts[id] = draft; else delete drafts[id]; }
  return { ...stored, ...(layout || {}), drafts };
}

/** Removes a session everywhere: its tab key from the open list and selection, and its draft. */
export function forget(state: UIState, route: Route): UIState {
  const key = `${route.native ? 'native' : 'session'}:${route.id}`;
  const drafts = { ...state.drafts };
  if (!route.native) delete drafts[route.id];
  return { ...state, open: state.open.filter(item => item !== key), selected: state.selected === key ? '' : state.selected, drafts };
}

function cleanView(value: unknown): ViewState | undefined {
  if (!isObject(value)) return undefined;
  const groups: Record<number, boolean> = {};
  if (isObject(value.expandedGroups)) for (const [key, open] of Object.entries(value.expandedGroups)) if (/^\d+$/.test(key) && typeof open === 'boolean') groups[Number(key)] = open;
  return {
    scrollTop: Number.isFinite(value.scrollTop) && value.scrollTop > 0 ? value.scrollTop : 0,
    nearBottom: value.nearBottom !== false,
    expandedTools: Array.isArray(value.expandedTools) ? value.expandedTools.filter((seq: unknown) => Number.isInteger(seq)) : [],
    expandedGroups: groups,
  };
}

export function parseViews(raw: string | null | undefined): Record<string, ViewState> {
  const result: Record<string, ViewState> = {};
  try {
    const value = raw ? JSON.parse(raw) : undefined;
    if (isObject(value)) for (const [key, item] of Object.entries(value)) { const view = isKey(key) && cleanView(item); if (view) result[key] = view; }
  } catch {}
  return result;
}

/** Keeps the most recently listed `max` views (callers list the open tabs last). */
export function serializeViews(views: Record<string, ViewState>, max = MAX_VIEWS): string {
  return JSON.stringify(Object.fromEntries(Object.entries(views).slice(-max)));
}

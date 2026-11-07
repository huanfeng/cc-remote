// The one request helper every host call goes through: bearer token, page
// language, no HTTP caching, JSON bodies, and ETag revalidation of cursorless
// GETs so an unchanged poll costs a 304 and hands back the very same object
// (React then skips the re-render). Errors become BackendError; a 401 tells the
// shell once that the token was refused.
//
// Plain TypeScript with explicit .ts imports: node --test loads it directly.
import { BackendError, type Delivery } from '../errors.ts';

export type Lang = 'en' | 'zh';

export interface ClientOptions {
  /** '' for the page's own origin, or 'https://host:port' for a standalone page. */
  baseUrl: string;
  token: string;
  lang: Lang;
  onUnauthorized?: () => void;
  /** Injected in tests; defaults to the global fetch. */
  fetch?: typeof fetch;
}

export interface RequestOptions {
  method?: string;
  /** Sent as JSON. */
  body?: unknown;
  signal?: AbortSignal;
}

/** Cursor queries change every time; revalidating them would only fill the cache. */
const CURSOR = /[?&](after|before)=/;
const MAX_CACHED = 64;

export class HttpClient {
  readonly baseUrl: string;
  readonly lang: Lang;
  private readonly token: string;
  private readonly onUnauthorized?: () => void;
  private readonly fetchImpl: typeof fetch;
  private readonly etags = new Map<string, { tag: string; data: unknown }>();
  private signedOut = false;

  constructor(o: ClientOptions) {
    this.baseUrl = o.baseUrl.replace(/\/+$/, '');
    this.token = o.token;
    this.lang = o.lang;
    this.onUnauthorized = o.onUnauthorized;
    this.fetchImpl = o.fetch ?? ((input, init) => globalThis.fetch(input, init));
  }

  /** A bilingual string in the page's language. */
  t(en: string, zh: string) { return this.lang === 'zh' ? zh : en; }

  /** The absolute (or same-origin) URL of a host path. */
  url(path: string) { return /^[a-z][a-z0-9+.-]*:/i.test(path) ? path : this.baseUrl + path; }

  /** Headers every request carries (the XHR upload sets the same). */
  authHeaders(): Record<string, string> {
    return { Authorization: `Bearer ${this.token}`, 'X-CC-Lang': this.lang };
  }

  clearCache() { this.etags.clear(); }

  /** A JSON request. A cursorless GET revalidates with its ETag and returns the cached object on 304. */
  async json<T>(path: string, o: RequestOptions = {}): Promise<T> {
    const method = (o.method ?? 'GET').toUpperCase();
    const cacheable = method === 'GET' && !CURSOR.test(path);
    const cached = cacheable ? this.etags.get(path) : undefined;
    const headers: Record<string, string> = { ...this.authHeaders() };
    if (o.body !== undefined) headers['Content-Type'] = 'application/json';
    if (cached) headers['If-None-Match'] = cached.tag;
    const response = await this.send(path, {
      method, headers, signal: o.signal,
      body: o.body !== undefined ? JSON.stringify(o.body) : undefined,
    });
    if (response.status === 304 && cached) {
      // Most recently used last, so the oldest entry is the one dropped.
      this.etags.delete(path); this.etags.set(path, cached);
      return cached.data as T;
    }
    if (!response.ok) throw await this.error(response);
    if (response.status === 204) return undefined as T;
    const text = await response.text();
    const data = (text ? JSON.parse(text) : undefined) as T;
    const tag = response.headers.get('ETag');
    if (cacheable && tag) {
      this.etags.delete(path);
      this.etags.set(path, { tag, data });
      if (this.etags.size > MAX_CACHED) this.etags.delete(this.etags.keys().next().value!);
    }
    return data;
  }

  /** A request whose body the caller reads (files, attachments); errors are mapped, never cached. */
  async raw(path: string, o: { method?: string; signal?: AbortSignal; okStatuses?: number[] } = {}): Promise<Response> {
    const response = await this.send(path, { method: o.method ?? 'GET', headers: this.authHeaders(), signal: o.signal });
    if (!response.ok && !o.okStatuses?.includes(response.status)) throw await this.error(response);
    return response;
  }

  private async send(path: string, init: RequestInit): Promise<Response> {
    try {
      return await this.fetchImpl(this.url(path), { ...init, cache: 'no-store' });
    } catch (err) {
      if ((err as { name?: string })?.name === 'AbortError' || init.signal?.aborted) throw err;
      const detail = err instanceof Error ? err.message : String(err);
      throw new BackendError(this.t(`Could not reach the host: ${detail}`, `无法连接宿主：${detail}`), 0);
    }
  }

  /** Maps a failed response ({error, code, delivery}) to a BackendError; a 401 signs out once. */
  async error(response: Response): Promise<BackendError> {
    let message = this.t(`Request failed (${response.status})`, `请求失败 (${response.status})`);
    let code: string | undefined;
    let delivery: Delivery | undefined;
    try {
      const body = await response.json() as { error?: string; message?: string; code?: string; delivery?: string };
      message = body.error || body.message || message;
      code = body.code;
      if (body.delivery === 'rejected' || body.delivery === 'pending' || body.delivery === 'unknown') delivery = body.delivery;
    } catch { /* not JSON */ }
    if (response.status === 401) this.unauthorized();
    return new BackendError(message, response.status, code, delivery);
  }

  /** The token was refused: forget cached replies and tell the shell, once. */
  unauthorized() {
    this.etags.clear();
    if (this.signedOut) return;
    this.signedOut = true;
    this.onUnauthorized?.();
  }
}

/** Appends a query string part to a path. */
export function withQuery(path: string, query: string) {
  if (!query) return path;
  return `${path}${path.includes('?') ? '&' : '?'}${query}`;
}

/** Native and preference endpoints take ?provider=codex; Claude is the default. */
export function providerQuery(path: string, provider: string | undefined) {
  return provider === 'codex' ? withQuery(path, 'provider=codex') : path;
}

/** Leaves out the fields that are undefined (the host refuses unknown fields, not missing ones). */
export function defined<T extends object>(o: T): Partial<T> {
  const out: Partial<T> = {};
  for (const [k, v] of Object.entries(o)) if (v !== undefined) (out as Record<string, unknown>)[k] = v;
  return out;
}

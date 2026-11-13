// The desktop app's side of the page (desktop/, Tauri 2). Imported first by
// main.tsx: lib/hosts.ts decides at load time whether the page holds several
// hosts, by window.__CC_DESKTOP__, so the bridge must exist before it runs.
//
// What changes inside the app:
// - fetch to another origin goes out from Rust (desktop/src-tauri/src/net.rs):
//   the host refuses an Origin it does not know, and a LAN host is often plain
//   http. Same-origin requests (the app's own files) stay in the webview.
// - the terminal's WebSocket is DesktopSocket, also through Rust (ws.rs).
// - window controls, links opening in the browser, Ctrl+click on a host opening
//   a second window.
import { Channel, invoke, isTauri } from '@tauri-apps/api/core';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { WebviewWindow } from '@tauri-apps/api/webviewWindow';

export type DesktopPlatform = 'windows' | 'linux' | 'mac';

export interface DesktopBridge {
  platform: DesktopPlatform;
  minimize(): void;
  toggleMaximize(): void;
  close(): void;
}

declare global {
  interface Window { __CC_DESKTOP__?: DesktopBridge }
}

export const DESKTOP = typeof window !== 'undefined' && isTauri();

function platformOf(): DesktopPlatform {
  const ua = navigator.userAgent;
  if (/Mac/i.test(ua)) return 'mac';
  if (/Windows/i.test(ua)) return 'windows';
  return 'linux';
}

const abortError = () => new DOMException('The operation was aborted.', 'AbortError');

// ---- fetch ----

let nextRequest = 1;
type ReplyMeta = { status: number; statusText: string; url: string; headers: [string, string][] };
const NULL_BODY = new Set([101, 103, 204, 205, 304]);

async function desktopFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const request = new Request(input, init);
  const signal = request.signal;
  if (signal.aborted) throw abortError();
  const id = nextRequest++;
  const headers: [string, string][] = [];
  request.headers.forEach((value, name) => headers.push([name, value]));
  const body = request.method === 'GET' || request.method === 'HEAD' ? new Uint8Array() : new Uint8Array(await request.arrayBuffer());
  const meta = encodeURIComponent(JSON.stringify({ id, method: request.method, url: request.url, headers }));
  const onAbort = () => { void invoke('http_abort', { id }); };
  signal.addEventListener('abort', onAbort, { once: true });
  let reply: ArrayBuffer;
  try {
    reply = await invoke<ArrayBuffer>('http_request', body, { headers: { 'x-cc-meta': meta } });
  } catch (err) {
    if (signal.aborted) throw abortError();
    // Like a browser's network failure, with the reason (DNS, refused, TLS) attached.
    throw new TypeError(String(err));
  } finally {
    signal.removeEventListener('abort', onAbort);
  }
  const view = new DataView(reply);
  const length = view.getUint32(0);
  const head = JSON.parse(new TextDecoder().decode(new Uint8Array(reply, 4, length))) as ReplyMeta;
  const content = NULL_BODY.has(head.status) || request.method === 'HEAD' ? null : reply.slice(4 + length);
  return new Response(content, { status: head.status, statusText: head.statusText, headers: head.headers });
}

function installFetch() {
  const own = window.fetch.bind(window);
  window.fetch = (input, init) => {
    const href = input instanceof Request ? input.url : String(input);
    let url: URL;
    try { url = new URL(href, location.href); } catch { return own(input, init); }
    // Tauri's invoke itself uses fetch(http[s]://ipc.localhost/...). Sending
    // that through desktopFetch would invoke Rust recursively until V8 OOMs.
    const remote = (url.protocol === 'http:' || url.protocol === 'https:') && url.origin !== location.origin && url.hostname !== 'ipc.localhost';
    return remote ? desktopFetch(input, init) : own(input, init);
  };
}

// ---- WebSocket ----

type SocketEvent = { type: 'open' } | { type: 'text'; data: string } | { type: 'close'; code: number; reason: string };

/** The subset of WebSocket the terminal stream uses (backend/http/terminal.ts WebSocketLike). */
export class DesktopSocket {
  binaryType: BinaryType = 'arraybuffer';
  readyState: 0 | 1 | 2 | 3 = 0;
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  private readonly id: Promise<number>;
  /** Frames go one after another: separate invokes may overtake each other. */
  private queue: Promise<unknown> = Promise.resolve();

  constructor(url: string) {
    const events = new Channel<ArrayBuffer | SocketEvent | string>();
    this.id = invoke<number>('ws_open', { url, events });
    // Events wait for the id, so an early 'open' cannot run before send() can work.
    events.onmessage = message => { void this.id.then(() => this.dispatch(message), () => undefined); };
    this.id.catch(err => this.finish(1006, String(err)));
  }

  private dispatch(message: ArrayBuffer | SocketEvent | string) {
    if (this.readyState === 3) return;
    if (message instanceof ArrayBuffer) { this.onmessage?.(new MessageEvent('message', { data: message })); return; }
    const event: SocketEvent = typeof message === 'string' ? JSON.parse(message) : message;
    if (event.type === 'open') {
      if (this.readyState !== 0) return;
      this.readyState = 1;
      this.onopen?.(new Event('open'));
    } else if (event.type === 'text') {
      this.onmessage?.(new MessageEvent('message', { data: event.data }));
    } else {
      this.finish(event.code, event.reason);
    }
  }

  private finish(code: number, reason: string) {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.onclose?.(new CloseEvent('close', { code, reason, wasClean: code === 1000 }));
  }

  send(data: string | Blob | BufferSource) {
    if (this.readyState !== 1) return;
    const frame: string | Uint8Array | Promise<Uint8Array> = typeof data === 'string' ? data
      : data instanceof Blob ? data.arrayBuffer().then(b => new Uint8Array(b))
        : ArrayBuffer.isView(data) ? new Uint8Array(data.buffer.slice(data.byteOffset, data.byteOffset + data.byteLength))
          : new Uint8Array(data);
    this.queue = this.queue.then(() => Promise.all([this.id, frame])).then(([id, frame]) => (typeof frame === 'string'
      ? invoke('ws_send_text', { id, text: frame })
      : invoke('ws_send', frame, { headers: { 'x-cc-socket': String(id) } }))).catch(() => undefined);
  }

  close(code?: number, reason?: string) {
    if (this.readyState >= 2) return;
    this.readyState = 2;
    this.queue = this.queue.then(() => this.id).then(id => invoke('ws_close', { id, code, reason })).catch(() => undefined);
  }
}

// ---- Links and windows ----

const openExternal = (url: string) => { void invoke('plugin:opener|open_url', { url }).catch(() => undefined); };

let nextWindow = 1;
/** A page of this app in a second window (Ctrl+click on a host). */
function openWindow(url: string) {
  const label = `host-${Date.now().toString(36)}-${nextWindow++}`;
  new WebviewWindow(label, {
    url, title: 'CC Remote', width: 1200, height: 800, minWidth: 720, minHeight: 480,
    decorations: platformOf() === 'mac', ...(platformOf() === 'mac' ? { titleBarStyle: 'overlay', hiddenTitle: true } : {}),
  });
}

function installLinks() {
  // Links to other sites open in the browser; the webview would otherwise navigate away from the app.
  document.addEventListener('click', e => {
    if (e.defaultPrevented || e.button !== 0) return;
    const a = (e.target as Element | null)?.closest?.('a[href]') as HTMLAnchorElement | null;
    if (!a || a.hasAttribute('download')) return;
    const url = new URL(a.href, location.href);
    if (url.origin === location.origin || !/^(https?|mailto):$/.test(url.protocol)) return;
    e.preventDefault();
    openExternal(url.href);
  }, true);
  window.open = ((url?: string | URL) => {
    if (!url) return null;
    const target = new URL(String(url), location.href);
    if (target.origin === location.origin) openWindow(target.pathname + target.search + target.hash);
    else if (/^(https?|mailto):$/.test(target.protocol)) openExternal(target.href);
    return null;
  }) as typeof window.open;
}

function installBridge() {
  const platform = platformOf();
  const win = getCurrentWindow();
  window.__CC_DESKTOP__ = {
    platform,
    minimize: () => { void win.minimize(); },
    toggleMaximize: () => { void win.toggleMaximize(); },
    close: () => { void win.close(); },
  };
  document.documentElement.classList.add('desktop-shell', `platform-${platform}-desktop`);
}

if (DESKTOP) {
  installBridge();
  installFetch();
  installLinks();
}

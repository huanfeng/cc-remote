// One connection to a terminal session's PTY: POST /api/terminals/{id}/ticket,
// then a WebSocket to /api/terminals/{id}/stream?ticket=… (a browser WebSocket
// cannot send the Authorization header; the one-time ticket is its credential).
//
// A stream lives for one connection, like the mock's FakeTerminal: it reports
// 'connecting', 'open', and ends with 'exited' (close code 4001, Claude Code quit)
// or 'failed'. Reconnecting with backoff, the open timeout (a stream the page's
// CSP blocks never fires close) and pausing while hidden are the terminal
// engine's (features/terminal/terminalEngine.ts): it opens a new stream per try.
import type { TerminalHandlers, TerminalStream } from '../types.ts';
import type { HttpClient } from './client.ts';
import { BackendError } from '../errors.ts';

/** The host closes a web stream with this when Claude Code exited (internal/server/terminal.go statusExited). */
export const STATUS_EXITED = 4001;

export type WebSocketLike = Pick<WebSocket, 'send' | 'close' | 'readyState'> & {
  binaryType: BinaryType;
  onopen: ((ev: Event) => void) | null;
  onmessage: ((ev: MessageEvent) => void) | null;
  onclose: ((ev: CloseEvent) => void) | null;
  onerror: ((ev: Event) => void) | null;
};
export type WebSocketCtor = new (url: string) => WebSocketLike;

const OPEN = 1;

/** ws: or wss: for an http(s) URL, resolved against the page when relative. */
export function socketUrl(httpUrl: string): string {
  const base = typeof location !== 'undefined' ? location.href : 'http://localhost/';
  const url = new URL(httpUrl, base);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  return url.toString();
}

export class HttpTerminalStream implements TerminalStream {
  private ws?: WebSocketLike;
  private closed = false;
  private opened = false;
  private readonly abort = new AbortController();
  private readonly encoder = new TextEncoder();
  private readonly client: HttpClient;
  private readonly id: string;
  private readonly h: TerminalHandlers;
  private readonly Socket: WebSocketCtor;

  constructor(client: HttpClient, id: string, h: TerminalHandlers, Socket: WebSocketCtor) {
    this.client = client;
    this.id = id;
    this.h = h;
    this.Socket = Socket;
    h.onState('connecting');
    void this.start();
  }

  private async start() {
    const t = (en: string, zh: string) => this.client.t(en, zh);
    const path = `/api/terminals/${encodeURIComponent(this.id)}`;
    let ticket: string;
    try {
      ticket = (await this.client.json<{ ticket: string }>(`${path}/ticket`, { method: 'POST', signal: this.abort.signal })).ticket;
    } catch (err) {
      if (this.closed) return;
      const status = err instanceof BackendError ? err.status : 0;
      // The session is gone or not a terminal, or the token was refused: retrying cannot help.
      this.end('failed', err instanceof Error ? err.message : String(err), status >= 400 && status < 500 ? { retry: false } : undefined);
      return;
    }
    if (this.closed) return;
    const url = socketUrl(this.client.url(`${path}/stream?ticket=${encodeURIComponent(ticket)}`));
    const host = new URL(url).host;
    let ws: WebSocketLike;
    try {
      ws = new this.Socket(url);
    } catch (err) {
      // A blocked address (ws: from an https page).
      this.end('failed', err instanceof Error ? err.message : String(err), { retry: false });
      return;
    }
    this.ws = ws;
    ws.binaryType = 'arraybuffer';
    ws.onopen = () => {
      if (this.closed) return;
      this.opened = true;
      this.h.onState('open');
    };
    ws.onmessage = ev => {
      if (this.closed) return;
      const data: unknown = ev.data;
      if (typeof data !== 'string') {
        if (data instanceof ArrayBuffer) this.h.onData(new Uint8Array(data));
        else if (ArrayBuffer.isView(data)) this.h.onData(new Uint8Array(data.buffer, data.byteOffset, data.byteLength));
        return;
      }
      let m: { type?: string; cols?: number; rows?: number; held?: boolean };
      try { m = JSON.parse(data); } catch { return; }
      if (m.type === 'size' && m.cols && m.rows) this.h.onSize({ cols: m.cols, rows: m.rows, held: !!m.held });
    };
    ws.onerror = () => { /* a close event follows */ };
    ws.onclose = ev => {
      if (this.closed) return;
      if (ev.code === STATUS_EXITED) { this.end('exited', ev.reason); return; }
      this.end('failed', this.opened
        ? ev.reason || t(`Connection lost (${ev.code})`, `连接已断开（${ev.code}）`)
        : t(`The WebSocket to ${host} did not open (${ev.code}); a reverse proxy or tunnel in between may not forward WebSocket`,
          `到 ${host} 的 WebSocket 没能建立（${ev.code}），中间的反向代理或隧道可能没有转发 WebSocket`));
    };
  }

  private end(state: 'exited' | 'failed', detail: string, opts?: { retry?: boolean }) {
    if (this.closed) return;
    this.closed = true;
    this.detach();
    this.h.onState(state, detail, opts);
  }

  private detach() {
    const ws = this.ws;
    this.ws = undefined;
    if (!ws) return;
    ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
    try { ws.close(); } catch { /* already closing */ }
  }

  private json(value: object) { if (this.ws?.readyState === OPEN) this.ws.send(JSON.stringify(value)); }

  /** Keystrokes and paste go as binary frames. */
  send(data: string) { if (this.ws?.readyState === OPEN) this.ws.send(this.encoder.encode(data)); }
  resize(cols: number, rows: number) { this.json({ type: 'resize', cols, rows }); }
  redraw() { this.json({ type: 'redraw' }); }
  /** Ends the stream without reporting anything more. */
  close() {
    if (this.closed) return;
    this.closed = true;
    this.abort.abort();
    this.detach();
  }
}

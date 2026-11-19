import type { ProjectShellHandlers, ProjectShellStream } from '../types.ts';
import { BackendError } from '../errors.ts';
import type { HttpClient } from './client.ts';
import { socketUrl, type WebSocketCtor, type WebSocketLike } from './terminal.ts';

const OPEN = 1;
const EXITED = 4001;
const OPEN_TIMEOUT = 15_000;

export const projectShellsPath = '/api/project-shells';

export function projectShellRunPath(shellId: string, runId: string): string {
  return `${projectShellsPath}/${encodeURIComponent(shellId)}/runs/${encodeURIComponent(runId)}`;
}

function isSequence(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
}

/** One ticket and one connection to an existing Shell run. The pane owns retries. */
export class HttpProjectShellStream implements ProjectShellStream {
  private readonly abort = new AbortController();
  private readonly encoder = new TextEncoder();
  private readonly readyTimer: ReturnType<typeof setTimeout>;
  private readonly client: HttpClient;
  private readonly handlers: ProjectShellHandlers;
  private socket?: WebSocketLike;
  private closed = false;
  private ready = false;
  private controlling = false;

  constructor(
    client: HttpClient,
    handlers: ProjectShellHandlers,
    Socket: WebSocketCtor,
    path: string,
    after: number,
  ) {
    this.client = client;
    this.handlers = handlers;
    this.readyTimer = setTimeout(() => {
      this.finish('failed', this.client.t('Project terminal connection timed out', '连接项目终端超时'));
    }, OPEN_TIMEOUT);
    handlers.onState('connecting');
    void this.connect(Socket, path, after);
  }

  private async connect(Socket: WebSocketCtor, path: string, after: number) {
    let ticket: string;
    try {
      ({ ticket } = await this.client.json<{ ticket: string }>(`${path}/ticket`, {
        method: 'POST', signal: this.abort.signal,
      }));
    } catch (error) {
      const detail = error instanceof Error ? error.message : String(error);
      const status = error instanceof BackendError ? error.status : 0;
      if ([404, 409, 410].includes(status)) this.finish('exited', detail);
      else this.finish('failed', detail, ![400, 401, 403, 405, 422].includes(status));
      return;
    }
    if (this.closed) return;

    const query = new URLSearchParams({ ticket, after: String(after) });
    try {
      this.socket = new Socket(socketUrl(this.client.url(`${path}/stream?${query}`)));
    } catch {
      this.finish('failed', this.client.t('Could not open a project terminal connection', '无法打开项目终端连接'), false);
      return;
    }
    const socket = this.socket;
    socket.binaryType = 'arraybuffer';
    socket.onmessage = event => this.message(event.data);
    socket.onerror = () => { /* The browser follows this with a close event. */ };
    socket.onclose = event => {
      if (event.code === EXITED) this.finish('exited', event.reason);
      else this.finish('failed', event.reason || this.client.t('Project terminal connection lost', '项目终端连接已断开'));
    };
  }

  private message(data: unknown) {
    if (this.closed) return;
    if (data instanceof ArrayBuffer || ArrayBuffer.isView(data)) {
      const view = data instanceof ArrayBuffer
        ? new DataView(data)
        : new DataView(data.buffer, data.byteOffset, data.byteLength);
      if (view.byteLength < 8) return;
      const seq = Number(view.getBigUint64(0));
      if (isSequence(seq)) {
        this.handlers.onData(new Uint8Array(view.buffer, view.byteOffset + 8, view.byteLength - 8), seq);
      }
      return;
    }
    if (typeof data !== 'string') return;
    let frame: { type?: string; seq?: number; cols?: number; rows?: number; held?: boolean; detail?: string } | null;
    try { frame = JSON.parse(data); } catch { return; }
    if (!frame) return;

    switch (frame.type) {
      case 'size':
        if (frame.cols && frame.rows && isSequence(frame.cols) && isSequence(frame.rows)) {
          this.handlers.onSize({ cols: frame.cols, rows: frame.rows }, isSequence(frame.seq) ? frame.seq : undefined);
        }
        break;
      case 'control':
        this.controlling = frame.held === true;
        this.handlers.onControl(this.controlling);
        break;
      case 'ready':
        if (!isSequence(frame.seq)) break;
        clearTimeout(this.readyTimer);
        this.ready = true;
        this.handlers.onReady(frame.seq);
        break;
      case 'reset':
        if (isSequence(frame.seq)) this.handlers.onReset(frame.seq);
        break;
      case 'exit':
        this.finish('exited', frame.detail);
        break;
    }
  }

  private finish(state: 'exited' | 'failed', detail?: string, retry?: boolean) {
    if (this.closed) return;
    this.close();
    this.handlers.onState(state, detail, retry === undefined ? undefined : { retry });
  }

  private json(value: object) {
    if (this.ready && this.socket?.readyState === OPEN) this.socket.send(JSON.stringify(value));
  }

  send(data: string | Uint8Array) {
    if (this.ready && this.controlling && this.socket?.readyState === OPEN) {
      this.socket.send(typeof data === 'string' ? this.encoder.encode(data) : new Uint8Array(data));
    }
  }

  resize(cols: number, rows: number) {
    if (this.controlling) this.json({ type: 'resize', cols, rows });
  }

  redraw() { this.json({ type: 'redraw' }); }
  take() { this.json({ type: 'take' }); }

  close() {
    if (this.closed) return;
    this.closed = true;
    clearTimeout(this.readyTimer);
    this.abort.abort();
    const socket = this.socket;
    this.socket = undefined;
    if (!socket) return;
    socket.onopen = socket.onmessage = socket.onclose = socket.onerror = null;
    try { socket.close(); } catch { /* The connection is already closing. */ }
  }
}

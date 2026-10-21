import { useEffect, useRef, useState } from 'react';
import type { Terminal } from '@xterm/xterm';
import type { FitAddon } from '@xterm/addon-fit';
import { copyText } from './clipboard';
import { apiUrl } from './hosts';
import { t } from './i18n';
import './terminal.css';

// A terminal session's own screen: xterm.js on the host's PTY stream, the same
// Claude Code a local `ccr` shows. The PTY has one size, the last front end to
// set it wins, so the view either *controls* the size (fits the PTY to itself)
// or *follows* it (scales its font to show the PTY's size as it is). On a
// computer typing takes control; on a phone the view follows unless "fit this
// device" is on, so a phone never squeezes the terminal on the computer.

type Api = <T>(path: string, options?: RequestInit) => Promise<T>;
type State = 'loading' | 'connecting' | 'open' | 'exited' | 'paused' | 'failed';
type Engine = { send(data: string): void; redraw(): void; take(): void; follow(control: boolean): void; layout(): void; reconnect(): void; term?: Terminal };

const touch = typeof matchMedia === 'function' && matchMedia('(pointer: coarse)').matches;
const encoder = new TextEncoder();
const FONT = "'Cascadia Mono', Consolas, 'SFMono-Regular', Menlo, 'Noto Sans Mono CJK SC', monospace";
const BASE_FONT = touch ? 12 : 13;
const STATUS_EXITED = 4001; // the host closes with it when Claude Code exited
const MAX_ATTEMPTS = 12; // about a minute and a half of retries: longer than a host restart
const OPEN_TIMEOUT = 15_000; // a proxy that holds the upgrade neither opens nor closes the socket
const THEME = { background: '#1d231f', foreground: '#e3e7dc', cursor: '#c7d8b3', selectionBackground: '#4c5d4580', black: '#1d231f', brightBlack: '#6b7568' };

const read = (key: string) => { try { return localStorage.getItem(key) || ''; } catch { return ''; } };
const write = (key: string, value: string) => { try { localStorage.setItem(key, value); } catch { /* private window */ } };

// Keys a phone keyboard lacks; arrows follow the cursor key mode Claude Code set.
const KEYS: { label: string; title: string; data: (app: boolean) => string }[] = [
  { label: 'Esc', title: 'Esc', data: () => '\x1b' },
  { label: 'Tab', title: 'Tab', data: () => '\t' },
  { label: '⇧Tab', title: 'Shift+Tab', data: () => '\x1b[Z' },
  { label: '↑', title: '↑', data: app => app ? '\x1bOA' : '\x1b[A' },
  { label: '↓', title: '↓', data: app => app ? '\x1bOB' : '\x1b[B' },
  { label: '←', title: '←', data: app => app ? '\x1bOD' : '\x1b[D' },
  { label: '→', title: '→', data: app => app ? '\x1bOC' : '\x1b[C' },
  { label: '^C', title: 'Ctrl+C', data: () => '\x03' },
  { label: '⏎', title: 'Enter', data: () => '\r' },
];

export function TerminalView({ id, api }: { id: string; api: Api }) {
  const box = useRef<HTMLDivElement>(null);
  const screen = useRef<HTMLDivElement>(null);
  const engine = useRef<Engine | undefined>(undefined);
  const [state, setState] = useState<State>('loading');
  const [reason, setReason] = useState('');
  const [size, setSize] = useState<[number, number]>();
  const [fitDevice, setFitDevice] = useState(() => touch && read('cc-term-fit') === '1');
  const fitRef = useRef(fitDevice); fitRef.current = fitDevice;
  const [control, setControl] = useState(!touch || fitDevice);
  const [zoom, setZoom] = useState(() => Number(read('cc-term-zoom')) || 1);
  const zoomRef = useRef(zoom); zoomRef.current = zoom;
  const [draft, setDraft] = useState('');
  const composing = useRef(false);

  useEffect(() => {
    // gen numbers connection attempts: a newer one (or hiding the page) makes an older one give up.
    let disposed = false, ws: WebSocket | undefined, timer = 0, attempt = 0, frame = 0, gen = 0, exited = false;
    let term: Terminal | undefined, fit: FitAddon | undefined;
    // Sizes this view sent lately: the host echoes each, maybe after a later one was sent.
    let hostSize: [number, number] | undefined, sent = '', firstSize = true;
    const mine = new Map<string, number>();
    let controlling = !touch || fitRef.current;
    // Taking control again sends the size even when it is the one sent before another front end changed it.
    const setMode = (value: boolean) => { controlling = value; sent = ''; setControl(value); };
    const send = (data: string) => { if (ws?.readyState === WebSocket.OPEN) ws.send(encoder.encode(data)); };
    const sendJSON = (value: object) => { if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(value)); };

    // Size the terminal: fit the PTY to the view, or the font to the PTY's size.
    const layout = (again = true) => {
      const el = box.current, host = screen.current;
      if (!term || !fit || !el || !host || !el.clientWidth) return;
      host.classList.toggle('following', !controlling);
      if (controlling) {
        if (term.options.fontSize !== BASE_FONT) term.options.fontSize = BASE_FONT;
        const dims = fit.proposeDimensions();
        if (!dims || !(dims.cols >= 20) || !(dims.rows >= 5)) return;
        if (dims.cols !== term.cols || dims.rows !== term.rows) term.resize(dims.cols, dims.rows);
        const key = `${dims.cols}x${dims.rows}`;
        if (key !== sent && ws?.readyState === WebSocket.OPEN) { sent = key; mine.set(key, Date.now()); sendJSON({ type: 'resize', cols: dims.cols, rows: dims.rows }); }
        return;
      }
      if (!hostSize) return;
      if (hostSize[0] !== term.cols || hostSize[1] !== term.rows) term.resize(hostSize[0], hostSize[1]);
      const drawn = host.querySelector('.xterm-screen')?.getBoundingClientRect();
      if (!drawn?.width) return;
      const style = getComputedStyle(el);
      const width = el.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
      const height = el.clientHeight - parseFloat(style.paddingTop) - parseFloat(style.paddingBottom);
      const current = term.options.fontSize || BASE_FONT;
      // A phone fits the width and scrolls down (zoom buttons make it larger); a computer shows it all.
      let next = current * width / drawn.width;
      if (!touch) next = Math.min(next, current * height / drawn.height, 15);
      next = Math.max(4, Math.min(28, Math.floor(next * (touch ? zoomRef.current : 1) * 10) / 10));
      if (Math.abs(next - current) >= 0.1) {
        term.options.fontSize = next;
        if (again) requestAnimationFrame(() => layout(false)); // measured cells are not exactly proportional
      }
    };
    const schedule = () => { cancelAnimationFrame(frame); frame = requestAnimationFrame(() => layout()); };

    const connect = async () => {
      const g = ++gen;
      clearTimeout(timer);
      if (disposed || exited || document.hidden) return;
      setState('connecting');
      let ticket = '';
      try { ticket = (await api<{ ticket: string }>(`/api/terminals/${encodeURIComponent(id)}/ticket`, { method: 'POST', signal: AbortSignal.timeout(OPEN_TIMEOUT) })).ticket; }
      catch (err) {
        if (disposed || g !== gen) return;
        setReason((err as Error).name === 'TimeoutError' ? t('The host did not answer the ticket request in time', '宿主没有及时回应终端票据请求') : (err as Error).message);
        const status = (err as { status?: number }).status || 0;
        if (status >= 400 && status < 500) setState('failed'); // the session is gone or not a terminal: retrying cannot help
        else retry();
        return;
      }
      if (disposed || g !== gen || document.hidden) return;
      const url = new URL(apiUrl(`/api/terminals/${encodeURIComponent(id)}/stream?ticket=${ticket}`), location.href);
      url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
      let socket: WebSocket;
      try { socket = new WebSocket(url); }
      catch (err) { setReason((err as Error).message); setState('failed'); return; } // a blocked address (http from an https page)
      socket.binaryType = 'arraybuffer';
      let opened = false, late = false;
      // A socket the page's CSP blocks closes without a close event, so the timeout reports it too.
      const opening = window.setTimeout(() => { if (ws === socket && !opened) { late = true; socket.close(); closed(1006); } }, OPEN_TIMEOUT);
      ws = socket;
      sent = '';
      firstSize = true; // the size the PTY had before this view connected
      socket.onopen = () => {
        if (ws !== socket) return;
        opened = true;
        clearTimeout(opening);
        attempt = 0;
        setReason('');
        setState('open');
        term?.reset(); // the host paints the whole screen for a new front end
        layout();
        if (!controlling) sendJSON({ type: 'redraw' }); // no resize to trigger it
      };
      socket.onmessage = event => {
        if (ws !== socket) return;
        if (typeof event.data !== 'string') { term?.write(new Uint8Array(event.data)); return; }
        let message: { type?: string; cols?: number; rows?: number };
        try { message = JSON.parse(event.data); } catch { return; }
        if (message.type !== 'size' || !message.cols || !message.rows) return;
        hostSize = [message.cols, message.rows];
        setSize(hostSize);
        // Another front end set its size: follow it until this one is used again.
        const key = `${message.cols}x${message.rows}`, own = Date.now() - (mine.get(key) || 0) < 5000;
        if (controlling && !firstSize && !own) setMode(false);
        firstSize = false;
        schedule();
      };
      const closed = (code: number, reason = '') => {
        clearTimeout(opening);
        if (disposed || ws !== socket) return;
        ws = undefined;
        if (code === STATUS_EXITED) { exited = true; setState('exited'); setReason(reason); return; }
        // Refused before it opened: the ticket was fine, so something between this page and the host turned the WebSocket down.
        setReason(opened ? reason || t(`Connection lost (${code})`, `连接已断开（${code}）`) : late ? t(`The WebSocket to ${url.host} did not open within ${OPEN_TIMEOUT / 1000}s; the browser or a reverse proxy or tunnel in between held it`, `到 ${url.host} 的 WebSocket ${OPEN_TIMEOUT / 1000} 秒内没有建立，被浏览器拦下，或被中间的反向代理、隧道挂住`) : t(`The WebSocket to ${url.host} did not open (${code}); a reverse proxy or tunnel in between may not forward WebSocket`, `到 ${url.host} 的 WebSocket 没能建立（${code}），中间的反向代理或隧道可能没有转发 WebSocket`));
        if (document.hidden) { setState('paused'); return; }
        retry();
      };
      socket.onclose = event => closed(event.code, event.reason);
    };
    const retry = () => {
      clearTimeout(timer);
      if (document.hidden) { setState('paused'); return; } // connects again when shown
      if (attempt >= MAX_ATTEMPTS) { setState('failed'); return; }
      setState('connecting');
      timer = window.setTimeout(() => void connect(), Math.min(1000 * 2 ** attempt++, 10000));
    };
    const take = () => { if (!controlling && (!touch || fitRef.current)) { setMode(true); layout(); } };

    engine.current = {
      send: data => { take(); send(data); },
      redraw: () => sendJSON({ type: 'redraw' }),
      take: () => { if (!controlling) { setMode(true); layout(); } },
      follow: value => { setMode(value); schedule(); },
      layout: schedule,
      reconnect: () => { exited = false; attempt = 0; const socket = ws; ws = undefined; socket?.close(); void connect(); },
    };

    const onVisible = () => {
      if (exited) return; // only "Start again" starts Claude Code again
      if (document.hidden) { gen++; clearTimeout(timer); const socket = ws; ws = undefined; socket?.close(); setState('paused'); }
      else if (!ws) { attempt = 0; void connect(); }
    };
    const observer = new ResizeObserver(schedule);

    (async () => {
      const [{ Terminal }, { FitAddon }, { Unicode11Addon }, { WebLinksAddon }] = await Promise.all([
        import('@xterm/xterm'), import('@xterm/addon-fit'), import('@xterm/addon-unicode11'), import('@xterm/addon-web-links'), import('@xterm/xterm/css/xterm.css'),
      ]);
      if (disposed || !screen.current) return;
      term = new Terminal({ fontFamily: FONT, fontSize: BASE_FONT, allowProposedApi: true, scrollback: 3000, theme: THEME, disableStdin: touch, macOptionIsMeta: true });
      fit = new FitAddon();
      term.loadAddon(fit);
      term.loadAddon(new Unicode11Addon());
      term.unicode.activeVersion = '11'; // CJK and emoji take two cells, as Claude Code draws them
      term.loadAddon(new WebLinksAddon());
      term.open(screen.current);
      engine.current!.term = term;
      if (touch && term.textarea) { term.textarea.readOnly = true; term.textarea.setAttribute('inputmode', 'none'); } // no keyboard on tap: the input bar types
      term.onData(data => { take(); send(data); });
      term.attachCustomKeyEventHandler(event => {
        if (event.type !== 'keydown') return true;
        const mod = event.ctrlKey || event.metaKey;
        // Copy a selection instead of interrupting; paste through the browser so images and IMEs work as usual.
        if (mod && event.key.toLowerCase() === 'c' && term!.hasSelection()) { void copyText(term!.getSelection()).catch(() => {}); term!.clearSelection(); return false; }
        if (mod && !event.altKey && event.key.toLowerCase() === 'v') return false;
        return true;
      });
      observer.observe(box.current!);
      document.addEventListener('visibilitychange', onVisible);
      if (!touch) term.focus();
      void connect();
    })();

    return () => {
      disposed = true;
      clearTimeout(timer);
      cancelAnimationFrame(frame);
      observer.disconnect();
      document.removeEventListener('visibilitychange', onVisible);
      ws?.close();
      term?.dispose();
      engine.current = undefined;
    };
  }, [id, api]);

  useEffect(() => { write('cc-term-zoom', String(zoom)); engine.current?.layout(); }, [zoom]);

  const toggleFit = () => {
    const next = !fitDevice;
    setFitDevice(next);
    fitRef.current = next;
    write('cc-term-fit', next ? '1' : '');
    engine.current?.follow(next);
  };
  const press = (data: (app: boolean) => string) => engine.current?.send(data(!!engine.current.term?.modes.applicationCursorKeysMode));
  // A message as the web sends one: pasted (newlines stay in it), then Enter on its own.
  const submit = () => {
    const e = engine.current;
    if (!e) return;
    if (!draft) { e.send('\r'); return; }
    const text = draft.replace(/\r\n?/g, '\n');
    e.send(e.term?.modes.bracketedPasteMode ? `\x1b[200~${text}\x1b[201~` : text);
    setTimeout(() => engine.current?.send('\r'), 300);
    setDraft('');
  };

  const failure = state === 'connecting' && reason;
  const label = { loading: t('Loading the terminal…', '正在加载终端…'), connecting: t('Connecting…', '连接中…'), open: t('Connected', '已连接'), exited: reason || t('Claude Code exited', 'Claude Code 已退出'), paused: t('Paused in the background', '后台已暂停'), failed: t('Could not connect', '无法连接') }[state];
  return <div className="term-view">
    <div className="term-bar">
      <span className={`dot ${state === 'open' ? 'idle' : state === 'exited' || state === 'failed' ? 'error' : 'running'}`} />
      <span className="term-state">{label}</span>
      {size && <span className="term-size" title={control ? t('This view sets the terminal size', '终端尺寸由此处决定') : t('Showing the size another front end set', '显示的是其它端设定的尺寸')}>{size[0]}×{size[1]}{control ? '' : t(' · following', ' · 跟随')}</span>}
      <span className="term-bar-actions">
        {touch && !fitDevice && <><button type="button" aria-label={t('Smaller', '缩小')} title={t('Smaller', '缩小')} onClick={() => setZoom(old => Math.max(0.5, Math.round((old - 0.25) * 100) / 100))}>A−</button><button type="button" aria-label={t('Larger', '放大')} title={t('Larger', '放大')} onClick={() => setZoom(old => Math.min(3, Math.round((old + 0.25) * 100) / 100))}>A+</button></>}
        {touch && <button type="button" className={fitDevice ? 'active' : ''} aria-pressed={fitDevice} title={t('Resize the terminal to this device; the terminal on your computer gets the same size', '把终端尺寸改成本机大小；电脑上的终端也会变成这个尺寸')} onClick={toggleFit}>{t('Fit device', '适配本机')}</button>}
        {!touch && !control && <button type="button" title={t('Resize the terminal to this window', '把终端尺寸改成本窗口大小')} onClick={() => engine.current?.take()}>{t('Use this window', '使用本窗口尺寸')}</button>}
        {state === 'exited' ? <button type="button" onClick={() => engine.current?.reconnect()}>{t('Start again', '重新启动')}</button> : state === 'failed' ? <button type="button" onClick={() => engine.current?.reconnect()}>{t('Connect again', '重新连接')}</button> : <button type="button" title={t('Paint the whole screen again', '整屏重画')} disabled={state !== 'open'} onClick={() => engine.current?.redraw()}>{t('Redraw', '重绘')}</button>}
      </span>
    </div>
    {(failure || state === 'failed') && reason && <div className="term-notice" role="status">{reason}</div>}
    <div className="term-box" ref={box}><div className="term-screen" ref={screen} /></div>
    {touch && <>
      <div className="term-keys" role="toolbar" aria-label={t('Terminal keys', '终端按键')}>{KEYS.map(key => <button type="button" key={key.label} title={key.title} disabled={state !== 'open'} onPointerDown={event => event.preventDefault() /* keep the keyboard up */} onClick={() => press(key.data)}>{key.label}</button>)}</div>
      <form className="term-input" onSubmit={event => { event.preventDefault(); submit(); }}>
        <textarea rows={1} value={draft} placeholder={t('Type, then send (empty sends Enter)', '输入后发送（空内容即回车）')} aria-label={t('Text for the terminal', '发送到终端的文字')} enterKeyHint="send" onChange={event => setDraft(event.target.value)} onCompositionStart={() => { composing.current = true; }} onCompositionEnd={() => { composing.current = false; }} onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing && !composing.current && event.keyCode !== 229) { event.preventDefault(); submit(); } }} />
        <button className="primary" disabled={state !== 'open'}>{t('Send', '发送')}</button>
      </form>
    </>}
  </div>;
}

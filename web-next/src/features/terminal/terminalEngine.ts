// The imperative half of the terminal view: xterm on the host's PTY stream,
// connection state, reconnects and the size control. The React view renders
// what it reports through onUi and calls its methods; nothing here re-renders.
// Logic follows the old web/src/TerminalView.tsx; xterm setup follows ZCode's
// packages/ui/src/terminal/TerminalSession.tsx.
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { Unicode11Addon } from '@xterm/addon-unicode11';
import { WebLinksAddon } from '@xterm/addon-web-links';
import type { Backend } from '@/backend/backend';
import type { TerminalState, TerminalStream } from '@/backend/types';
import type { FileRef } from '@/domain/filePaths';
import { filePathLinks } from '@/domain/terminalPaths';
import { copyText } from '@/domain/clipboard';
import { mergeTerminalTheme } from '@/terminal/terminalTheme';
import { t } from '@/lib/i18n';
import {
  BASE_FONT_DESKTOP, BASE_FONT_PHONE, MAX_ATTEMPTS, OPEN_TIMEOUT_MS, RESIZE_THROTTLE_MS, SentSizes,
  backoffDelay, followFontSize, initialControl, keepsControl, sizeKey, throttleLatest, typingTakesControl, usableDims,
} from './terminalSize';

export interface TerminalUi {
  state: TerminalState;
  /** Why the last connection ended; while reconnecting it is shown with the attempt. */
  reason: string;
  /** Reconnect attempts so far (0 while connected). */
  attempt: number;
  size?: { cols: number; rows: number };
  held: boolean;
  controlling: boolean;
  hasSelection: boolean;
}

export interface TerminalEngine {
  send(data: string): void;
  redraw(): void;
  /** "Use this window": take the size back. */
  take(): void;
  /** Phone "fit device" switched: control (true) or follow (false). */
  follow(control: boolean): void;
  layout(): void;
  reconnect(): void;
  copySelection(): void;
  appCursor(): boolean;
  bracketedPaste(): boolean;
  focus(): void;
  dispose(): void;
}

export interface EngineOptions {
  backend: Backend;
  sessionId: string;
  mobile: boolean;
  box: HTMLDivElement;
  screen: HTMLDivElement;
  zoom: () => number;
  fitDevice: () => boolean;
  openFile: (ref: FileRef) => void;
  onUi: (patch: Partial<TerminalUi>) => void;
}

export const initialUi = (mobile: boolean, fitDevice: boolean): TerminalUi => ({
  state: 'connecting', reason: '', attempt: 0, held: false, controlling: initialControl(mobile, fitDevice), hasSelection: false,
});

function monoFont() {
  const v = getComputedStyle(document.documentElement).getPropertyValue('--font-mono').trim();
  return v || 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace';
}

export function createTerminalEngine(o: EngineOptions): TerminalEngine {
  const { backend, sessionId, mobile, box, screen } = o;
  const baseFont = mobile ? BASE_FONT_PHONE : BASE_FONT_DESKTOP;

  // gen numbers connection attempts: a newer one (or hiding the page) makes an older one's events stale.
  let disposed = false, gen = 0, attempt = 0, exited = false, opened = false;
  let stream: TerminalStream | undefined;
  let retryTimer = 0, openTimer = 0, frame = 0;
  let controlling = initialControl(mobile, o.fitDevice());
  let holding = false;
  let firstSize = true;
  let hostSize: { cols: number; rows: number } | undefined;
  let sentKey = '';
  const sent = new SentSizes();

  const term = new Terminal({
    fontFamily: monoFont(),
    fontSize: baseFont,
    scrollback: 3000,
    allowProposedApi: true,
    theme: mergeTerminalTheme(undefined),
    disableStdin: mobile, // the soft keyboard types into the input bar
    macOptionIsMeta: true,
    cursorBlink: false,
  });
  const fit = new FitAddon();
  term.loadAddon(fit);
  term.loadAddon(new Unicode11Addon());
  term.unicode.activeVersion = '11'; // CJK and emoji take two cells, as Claude Code draws them
  term.loadAddon(new WebLinksAddon());
  term.registerLinkProvider(filePathLinks(term, () => o.openFile));
  term.open(screen);
  if (mobile && term.textarea) {
    // A tap must not raise the keyboard over the screen: the input bar types.
    term.textarea.readOnly = true;
    term.textarea.setAttribute('inputmode', 'none');
  }

  const setMode = (value: boolean) => {
    controlling = value;
    sentKey = ''; // taking control again sends the size even if it was sent before
    o.onUi({ controlling: value });
  };

  const sendResize = throttleLatest<{ cols: number; rows: number }>(RESIZE_THROTTLE_MS, d => {
    const key = sizeKey(d.cols, d.rows);
    if (!stream || !opened || !controlling || key === sentKey) return;
    sentKey = key;
    sent.record(d.cols, d.rows, Date.now());
    stream.resize(d.cols, d.rows);
  }, { now: () => Date.now(), set: (f, ms) => window.setTimeout(f, ms), clear: h => clearTimeout(h as number) });

  // Size the terminal: fit the PTY to the view, or the font to the PTY's size.
  const layout = (again = true) => {
    if (disposed || !box.clientWidth || !box.clientHeight) return;
    screen.toggleAttribute('data-following', !controlling);
    if (controlling) {
      if (term.options.fontSize !== baseFont) term.options.fontSize = baseFont;
      const dims = fit.proposeDimensions();
      if (!usableDims(dims)) return;
      if (dims.cols !== term.cols || dims.rows !== term.rows) term.resize(dims.cols, dims.rows);
      sendResize.push({ cols: dims.cols, rows: dims.rows });
      return;
    }
    if (!hostSize) return;
    if (hostSize.cols !== term.cols || hostSize.rows !== term.rows) term.resize(hostSize.cols, hostSize.rows);
    const drawn = screen.querySelector('.xterm-screen')?.getBoundingClientRect();
    if (!drawn?.width || !drawn.height) return;
    const style = getComputedStyle(box);
    const width = box.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
    const height = box.clientHeight - parseFloat(style.paddingTop) - parseFloat(style.paddingBottom);
    const current = term.options.fontSize || baseFont;
    const next = followFontSize({ current, width, height, drawnWidth: drawn.width, drawnHeight: drawn.height, mobile, zoom: o.zoom() });
    if (Math.abs(next - current) >= 0.1) {
      term.options.fontSize = next;
      if (again) requestAnimationFrame(() => layout(false)); // measured cells are not exactly proportional
    }
  };
  const schedule = () => { cancelAnimationFrame(frame); frame = requestAnimationFrame(() => layout()); };

  // Closes the current stream; its late events are stale from here on.
  const drop = () => {
    gen++;
    clearTimeout(openTimer);
    sendResize.cancel();
    const s = stream;
    stream = undefined;
    opened = false;
    s?.close();
  };

  const retry = (reason: string) => {
    clearTimeout(retryTimer);
    if (document.hidden) { o.onUi({ state: 'paused', reason }); return; } // connects again when shown
    if (attempt >= MAX_ATTEMPTS) { o.onUi({ state: 'failed', reason, attempt }); return; }
    const delay = backoffDelay(attempt++);
    o.onUi({ state: 'connecting', reason, attempt });
    retryTimer = window.setTimeout(connect, delay);
  };

  function connect() {
    clearTimeout(retryTimer);
    drop();
    const g = ++gen;
    if (disposed || exited || document.hidden) return;
    o.onUi({ state: 'connecting' });
    firstSize = true; // the size the PTY had before this view connected
    sentKey = '';
    const live = () => !disposed && g === gen;
    const s = backend.openTerminal(sessionId, {
      onData(chunk) { if (live()) term.write(chunk); },
      onSize(size) {
        if (!live()) return;
        hostSize = { cols: size.cols, rows: size.rows };
        holding = size.held;
        const own = sent.isOwn(size.cols, size.rows, Date.now());
        // Another front end set its size (or a local terminal holds it): follow it until this one is used again.
        if (controlling && !keepsControl({ held: holding, firstSize, own })) setMode(false);
        firstSize = false;
        o.onUi({ size: hostSize, held: holding });
        schedule();
      },
      onState(state, detail) {
        if (!live()) return;
        if (state === 'open') {
          clearTimeout(openTimer);
          opened = true;
          attempt = 0;
          o.onUi({ state: 'open', reason: '', attempt: 0 });
          term.reset(); // the host paints the whole screen for a new front end
          layout();
          if (!controlling) stream?.redraw(); // no resize to trigger a repaint
          return;
        }
        if (state === 'exited') {
          drop();
          exited = true;
          o.onUi({ state: 'exited', reason: detail ?? '' });
          return;
        }
        if (state === 'failed' || state === 'paused') {
          const wasOpen = opened;
          drop();
          retry(detail || (wasOpen ? t('Connection lost', '连接已断开') : t('Could not reach the terminal', '无法连接到终端')));
        }
      },
    });
    if (!live()) { s.close(); return; }
    stream = s;
    // A stream the page's CSP or a proxy blocks may neither open nor fail; the timeout reports it.
    openTimer = window.setTimeout(() => {
      if (!live() || opened) return;
      drop();
      retry(t(`The terminal did not open within ${OPEN_TIMEOUT_MS / 1000}s`, `终端 ${OPEN_TIMEOUT_MS / 1000} 秒内没有建立连接`));
    }, OPEN_TIMEOUT_MS);
  }

  const send = (data: string) => { if (stream && opened) stream.send(data); };
  const take = () => {
    if (!typingTakesControl({ mobile, fitDevice: o.fitDevice(), held: holding, controlling })) return;
    setMode(true);
    layout();
  };

  term.onData(data => { take(); send(data); });
  term.onSelectionChange(() => o.onUi({ hasSelection: term.hasSelection() }));
  const copySelection = () => {
    if (!term.hasSelection()) return;
    void copyText(term.getSelection()).catch(() => {});
    term.clearSelection();
  };
  term.attachCustomKeyEventHandler(event => {
    if (event.type !== 'keydown') return true;
    const mod = event.ctrlKey || event.metaKey;
    // Copy a selection instead of interrupting; paste through the browser so IMEs and images work as usual.
    if (mod && !event.altKey && event.key.toLowerCase() === 'c' && term.hasSelection()) { event.preventDefault(); copySelection(); return false; }
    if (mod && !event.altKey && event.key.toLowerCase() === 'v') return false;
    return true;
  });

  // Theme switches (.dark, palette) change the --color-terminal-* tokens.
  const themeObserver = new MutationObserver(() => { term.options.theme = mergeTerminalTheme(undefined); });
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'data-palette'] });

  const onVisible = () => {
    if (exited) return; // only "Start again" starts Claude Code again
    if (document.hidden) {
      clearTimeout(retryTimer);
      drop();
      o.onUi({ state: 'paused' });
    } else if (!stream) {
      attempt = 0;
      connect();
    }
  };
  document.addEventListener('visibilitychange', onVisible);
  const observer = new ResizeObserver(schedule);
  observer.observe(box);

  if (!mobile) term.focus();
  connect();

  return {
    send: data => { take(); send(data); },
    redraw: () => stream?.redraw(),
    take: () => { if (!controlling && !holding) { setMode(true); layout(); } },
    follow: value => { setMode(value && !holding); schedule(); },
    layout: schedule,
    reconnect: () => { exited = false; attempt = 0; o.onUi({ reason: '', attempt: 0 }); connect(); },
    copySelection,
    appCursor: () => !!term.modes.applicationCursorKeysMode,
    bracketedPaste: () => !!term.modes.bracketedPasteMode,
    focus: () => { if (!mobile) term.focus(); },
    dispose: () => {
      disposed = true;
      clearTimeout(retryTimer);
      cancelAnimationFrame(frame);
      drop();
      observer.disconnect();
      themeObserver.disconnect();
      document.removeEventListener('visibilitychange', onVisible);
      term.dispose();
    },
  };
}

import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { Unicode11Addon } from '@xterm/addon-unicode11';
import { WebLinksAddon } from '@xterm/addon-web-links';
import type { Backend } from '@/backend/backend';
import type { ProjectShellStream } from '@/backend/types';
import { BASE_FONT_DESKTOP, BASE_FONT_PHONE, backoffDelay, MAX_ATTEMPTS, RESIZE_THROTTLE_MS, throttleLatest, usableDims } from '@/features/terminal/terminalSize';
import { mergeTerminalTheme } from '@/terminal/terminalTheme';
import { projectShellOutput } from './projectShellOutput';

export interface ShellTerminalState {
  status: 'connecting' | 'online' | 'retrying' | 'failed' | 'exited';
  control: boolean;
  truncated: boolean;
  detail: string;
}

export const initialShellTerminalState: ShellTerminalState = { status: 'connecting', control: false, truncated: false, detail: '' };

export function createProjectShellTerminal({ backend, shellId, runId, element, mobile, visible = true, onState }: {
  backend: Backend; shellId: string; runId: string; element: HTMLDivElement; mobile: boolean;
  visible?: boolean;
  onState(state: ShellTerminalState): void;
}) {
  const terminal = new Terminal({
    fontFamily: getComputedStyle(document.documentElement).getPropertyValue('--font-mono').trim() || 'monospace',
    fontSize: mobile ? BASE_FONT_PHONE : BASE_FONT_DESKTOP,
    scrollback: mobile ? 15000 : 30000,
    allowProposedApi: true,
    theme: mergeTerminalTheme(undefined),
    disableStdin: true,
    macOptionIsMeta: true,
  });
  const fit = new FitAddon();
  terminal.loadAddon(fit);
  terminal.loadAddon(new Unicode11Addon());
  terminal.unicode.activeVersion = '11';
  terminal.loadAddon(new WebLinksAddon());
  terminal.open(element);
  if (terminal.textarea) terminal.textarea.tabIndex = 0;

  let state = initialShellTerminalState;
  let stream: ProjectShellStream | undefined;
  let connected = false;
  let controlling = false;
  let generation = 0;
  let cursor = 0;
  let attempts = 0;
  let retryTimer = 0;
  let sentSize = '';
  let needsRedraw = false;
  const markApplied = (seq: number) => {
    cursor = seq;
    stream?.ack?.(cursor);
  };
  const output = projectShellOutput((bytes, parsed) => terminal.write(bytes, parsed), markApplied);
  const update = (patch: Partial<ShellTerminalState>) => {
    state = { ...state, ...patch };
    onState(state);
  };
  const sending = () => connected && controlling && !!stream;
  const send = (data: string | Uint8Array) => { if (sending()) stream?.send(data); };
  const data = terminal.onData(send);
  const binary = terminal.onBinary(value => send(Uint8Array.from(value, char => char.charCodeAt(0))));

  const resize = throttleLatest(RESIZE_THROTTLE_MS, ({ cols, rows }: { cols: number; rows: number }) => {
    if (!visible || !sending()) return;
    const key = `${cols}x${rows}`;
    if (key === sentSize || (cols === terminal.cols && rows === terminal.rows)) return;
    sentSize = key;
    stream?.resize(cols, rows);
  }, { now: () => Date.now(), set: (callback, ms) => window.setTimeout(callback, ms), clear: handle => clearTimeout(handle as number) });
  const layout = () => {
    if (!visible || !sending() || !element.clientWidth || !element.clientHeight) return;
    const size = fit.proposeDimensions();
    if (usableDims(size)) resize.push(size);
  };
  const observer = new ResizeObserver(layout);
  observer.observe(element);

  const disconnect = () => {
    generation++;
    stream?.close();
    stream = undefined;
    connected = false;
    controlling = false;
    terminal.options.disableStdin = true;
    sentSize = '';
    clearTimeout(retryTimer);
  };

  const refreshViewport = () => {
    if (!visible || !sending()) return;
    layout();
    if (needsRedraw) {
      needsRedraw = false;
      stream?.redraw();
    }
  };

  const applyControl = () => {
    terminal.options.disableStdin = !sending();
    if (connected) update({ control: controlling });
    refreshViewport();
  };

  const connect = () => {
    disconnect();
    const current = generation;
    // Let already-delivered frames finish before asking the host where to resume.
    output.barrier(() => {
      if (current !== generation) return;
      let received = cursor;
      stream = backend.openProjectShell(shellId, runId, cursor, {
        onState(status, detail = '', options) {
          if (current !== generation || status === 'connecting') return;
          disconnect();
          if (status === 'exited') {
            update({ status, detail, control: false });
          } else if (options?.retry !== false && attempts < MAX_ATTEMPTS) {
            retryTimer = window.setTimeout(connect, backoffDelay(attempts++));
            update({ status: 'retrying', detail, control: false });
          } else {
            update({ status: 'failed', detail, control: false });
          }
        },
        onReset(seq) {
          if (current !== generation) return;
          received = seq - 1;
          output.barrier(() => {
            terminal.reset();
            cursor = seq - 1;
            needsRedraw = true;
            update({ truncated: true });
          });
        },
        onData(bytes, seq) {
          if (current !== generation || seq <= received) return;
          received = seq;
          output.data(bytes, seq);
        },
        onSize({ cols, rows }, seq) {
          if (current !== generation || (seq !== undefined && seq <= received)) return;
          if (seq !== undefined) received = seq;
          output.barrier(() => {
            if (cols !== terminal.cols || rows !== terminal.rows) terminal.resize(cols, rows);
            if (seq !== undefined) markApplied(seq);
          });
        },
        onControl(held) {
          if (current !== generation) return;
          controlling = held;
          sentSize = '';
          applyControl();
        },
        onReady(seq) {
          if (current !== generation) return;
          received = Math.max(received, seq);
          output.barrier(() => {
            if (current !== generation) return;
            markApplied(Math.max(cursor, seq));
            connected = true;
            attempts = 0;
            update({ status: 'online', control: controlling, detail: '' });
            applyControl();
          });
        },
      });
    });
  };

  connect();
  return {
    focus: () => { if (visible) terminal.focus(); },
    setVisible(next: boolean) {
      if (visible === next) return;
      visible = next;
      if (visible) {
        terminal.refresh(0, terminal.rows - 1);
        refreshViewport();
      } else resize.cancel();
    },
    send,
    take: () => { if (connected) stream?.take(); },
    applyTheme: () => { terminal.options.theme = mergeTerminalTheme(undefined); },
    retry() {
      attempts = 0;
      update({ status: 'connecting', detail: '', control: false });
      connect();
    },
    dispose() {
      disconnect();
      output.dispose();
      observer.disconnect();
      resize.cancel();
      data.dispose();
      binary.dispose();
      terminal.dispose();
    },
  };
}

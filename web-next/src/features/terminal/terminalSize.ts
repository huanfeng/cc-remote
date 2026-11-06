// Pure decisions behind the terminal view, kept free of xterm and React so
// node tests can import them directly.
//
// The PTY has one size and the last front end to set it wins. A view either
// *controls* the size (fits the PTY to itself) or *follows* it (scales its
// font to show the PTY's size as it is). Every size the host reports is an
// echo of some front end's resize; one that matches what this view sent in
// the last few seconds is its own, anything else means another front end took
// over and this view starts following.

/** Reconnect attempts before the view gives up and offers "Connect again". */
export const MAX_ATTEMPTS = 12;
/** A stream that neither opens nor fails in this time counts as failed (a proxy holding the upgrade, a CSP block). */
export const OPEN_TIMEOUT_MS = 15_000;
/** An echo within this long of a resize this view sent is this view's own. */
export const OWN_ECHO_MS = 5_000;
/** Resizes sent to the host at most this often while the window is dragged. */
export const RESIZE_THROTTLE_MS = 200;

export const BASE_FONT_DESKTOP = 13;
export const BASE_FONT_PHONE = 12;
export const ZOOM_MIN = 0.5;
export const ZOOM_MAX = 3;
export const ZOOM_STEP = 0.25;

/** Delay before reconnect attempt `attempt` (0-based): 1 s, 2 s, 4 s, 8 s, then 10 s. */
export function backoffDelay(attempt: number): number {
  return Math.min(1000 * 2 ** Math.max(0, attempt), 10_000);
}

export const sizeKey = (cols: number, rows: number) => `${cols}x${rows}`;

/** Sizes this view sent lately; the host echoes each, maybe after a later one was sent. */
export class SentSizes {
  private sent = new Map<string, number>();
  record(cols: number, rows: number, now: number) {
    this.sent.set(sizeKey(cols, rows), now);
    for (const [key, at] of this.sent) if (now - at > OWN_ECHO_MS) this.sent.delete(key);
  }
  isOwn(cols: number, rows: number, now: number): boolean {
    const at = this.sent.get(sizeKey(cols, rows));
    return at !== undefined && now - at < OWN_ECHO_MS;
  }
  clear() { this.sent.clear(); }
}

/** Whether a view starts out controlling the size: computers do; phones only with "fit device". */
export function initialControl(mobile: boolean, fitDevice: boolean): boolean {
  return !mobile || fitDevice;
}

/**
 * Whether a controlling view keeps control after the host reported a size.
 * The first size after connecting is the PTY's size from before this view
 * came; it says nothing about another front end.
 */
export function keepsControl(p: { held: boolean; firstSize: boolean; own: boolean }): boolean {
  if (p.held) return false;
  return p.firstSize || p.own;
}

/** Whether typing in the view takes control back: on a computer, or a phone set to fit itself. */
export function typingTakesControl(p: { mobile: boolean; fitDevice: boolean; held: boolean; controlling: boolean }): boolean {
  return !p.controlling && !p.held && (!p.mobile || p.fitDevice);
}

/** Fitted dimensions worth applying: xterm proposes nonsense for a hidden or tiny box. */
export function usableDims(d: { cols: number; rows: number } | undefined): d is { cols: number; rows: number } {
  return !!d && d.cols >= 20 && d.rows >= 5 && Number.isFinite(d.cols) && Number.isFinite(d.rows);
}

/**
 * The font size that shows the PTY's size in the view while following.
 * A phone fits the width (zoom makes it larger and the box scrolls); a
 * computer shows the whole screen, never larger than 15px.
 */
export function followFontSize(p: {
  current: number;
  width: number; height: number;
  drawnWidth: number; drawnHeight: number;
  mobile: boolean; zoom: number;
}): number {
  let next = p.current * p.width / p.drawnWidth;
  if (!p.mobile) next = Math.min(next, p.current * p.height / p.drawnHeight, 15);
  next = next * (p.mobile ? p.zoom : 1);
  return Math.max(4, Math.min(28, Math.floor(next * 10) / 10));
}

export function stepZoom(zoom: number, dir: 1 | -1): number {
  const next = Math.round((zoom + dir * ZOOM_STEP) * 100) / 100;
  return Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, next));
}

/** A throttle that sends the latest value at once, then at most every `ms`, always ending on the latest. */
export function throttleLatest<T>(ms: number, fn: (v: T) => void, clock: { now(): number; set(f: () => void, ms: number): unknown; clear(h: unknown): void }) {
  let last = -Infinity;
  let timer: unknown;
  let pending: { v: T } | undefined;
  const flush = () => {
    timer = undefined;
    if (!pending) return;
    const { v } = pending;
    pending = undefined;
    last = clock.now();
    fn(v);
  };
  return {
    push(v: T) {
      pending = { v };
      if (timer !== undefined) return;
      const wait = last + ms - clock.now();
      if (wait <= 0) flush();
      else timer = clock.set(flush, wait);
    },
    cancel() { if (timer !== undefined) clock.clear(timer); timer = undefined; pending = undefined; },
  };
}

// Keys a phone keyboard lacks; arrows follow the cursor key mode the TUI set.
export const PHONE_KEYS: { label: string; title: string; data: (appCursor: boolean) => string }[] = [
  { label: 'Esc', title: 'Esc', data: () => '\x1b' },
  { label: 'Tab', title: 'Tab', data: () => '\t' },
  { label: '⇧Tab', title: 'Shift+Tab', data: () => '\x1b[Z' },
  { label: '←', title: '←', data: app => app ? '\x1bOD' : '\x1b[D' },
  { label: '↑', title: '↑', data: app => app ? '\x1bOA' : '\x1b[A' },
  { label: '↓', title: '↓', data: app => app ? '\x1bOB' : '\x1b[B' },
  { label: '→', title: '→', data: app => app ? '\x1bOC' : '\x1b[C' },
  { label: '^C', title: 'Ctrl+C', data: () => '\x03' },
  { label: '⏎', title: 'Enter', data: () => '\r' },
];

/** A message as the phone input sends it: pasted (newlines kept), Enter follows separately. */
export function pastePayload(draft: string, bracketed: boolean): string {
  const text = draft.replace(/\r\n?/g, '\n');
  return bracketed ? `\x1b[200~${text}\x1b[201~` : text;
}

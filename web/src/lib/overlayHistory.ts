/** One temporary history entry protects the route under the open dialog stack. */
export class OverlayHistory {
  private entries: { id: object; depth: number; close(): void }[] = [];
  private pending = false;
  private armed = false;
  private callbacks: (() => void)[] = [];
  private requested: object | undefined;
  private suspended = false;
  private listeners = new Set<() => void>();
  get hasOpen() { return this.entries.length > 0; }
  subscribe(listener: () => void) { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; }
  private readonly marker = `overlay:${Math.random()}`;
  private browser: Pick<Window, 'history' | 'addEventListener'>;
  constructor(browser: Pick<Window, 'history' | 'addEventListener'>) {
    this.browser = browser;
    browser.addEventListener('popstate', () => this.popped());
    // A reload/host switch does not restore dialogs from history state.
    if (browser.history.state?.ccrOverlay) {
      this.pending = true;
      browser.history.back();
    }
  }
  add(close: () => void, depth = 0) {
    // Let a newly opened confirmation decide whether its parent will close.
    const confirmation = this.requested !== undefined;
    if (confirmation) this.suspended = true;
    const entry = { id: {}, depth, close };
    this.entries.push(entry);
    this.entries.sort((a, b) => a.depth - b.depth);
    this.reconcile();
    this.listeners.forEach(listener => listener());
    return () => {
      this.entries = this.entries.filter(x => x !== entry);
      if (confirmation) queueMicrotask(() => {
        // Layout cleanups for accepted parent+confirmation finish together.
        if (this.entries.some(x => x.id === this.requested)) {
          this.callbacks = [];
          this.requested = undefined;
        }
        this.suspended = false;
        this.reconcile();
      });
      this.reconcile();
      this.listeners.forEach(listener => listener());
    };
  }
  dismiss() {
    const top = this.entries.at(-1);
    if (!top) return false;
    top.close();
    return true;
  }
  afterClose(callback: () => void) {
    this.callbacks.push(callback);
    this.reconcile();
  }
  private marked() { return this.browser.history.state?.ccrOverlay === this.marker; }
  private reconcile() {
    if (this.pending) return;
    if (this.entries.length) {
      if (!this.marked()) {
        this.browser.history.pushState({ ...this.browser.history.state, ccrOverlay: this.marker }, '');
      }
      this.armed = true;
      const top = this.entries.at(-1)!;
      if (!this.suspended && this.callbacks.length && this.requested !== top.id) {
        this.requested = top.id;
        top.close();
      }
    } else if (this.marked()) {
      this.pending = true;
      this.browser.history.back();
    } else if (!this.suspended) {
      const callbacks = this.callbacks.splice(0);
      this.requested = undefined;
      callbacks.forEach(callback => callback());
    }
  }
  private popped() {
    if (this.pending) {
      this.pending = false;
      this.armed = false;
    } else if (this.armed && !this.marked()) {
      this.armed = false;
      this.pending = true; // close callbacks may synchronously unregister layers
      this.dismiss();
      this.pending = false;
    } else if (this.browser.history.state?.ccrOverlay && !this.entries.length) {
      // Forward never resurrects a dismissed dialog or replays its action.
      this.pending = true;
      this.browser.history.back();
    }
    this.reconcile();
  }
}

let overlays: OverlayHistory | undefined;
export const overlayHistory = () => overlays ??= new OverlayHistory(window);

export function dismissOverlayShortcut(event: KeyboardEvent) {
  if (!(event.ctrlKey || event.metaKey) || event.altKey || event.shiftKey || event.isComposing || event.key.toLowerCase() !== 'w') return false;
  if (!overlayHistory().dismiss()) return false;
  event.preventDefault();
  event.stopImmediatePropagation();
  return true;
}

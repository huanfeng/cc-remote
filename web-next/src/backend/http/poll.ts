// A polling loop that never overlaps itself: the next tick is scheduled after
// the current one settles, so a slow host is not asked twice at once (a tick
// due while one is in flight is skipped). now() runs one as soon as possible
// (after an action, or when the page becomes visible again).

export interface Poller {
  /** Runs a tick right away, or right after the one in flight. */
  now(): void;
  stop(): void;
}

export interface PollOptions {
  /** Skip ticks while the page is hidden; poll again as soon as it is shown. */
  pauseHidden?: boolean;
  /** Called with a tick's rejection (the loop keeps going). */
  onError?: (e: unknown) => void;
}

const pageHidden = () => typeof document !== 'undefined' && document.hidden;

export function poll(run: () => Promise<void>, intervalMs: number, o: PollOptions = {}): Poller {
  let stopped = false;
  let busy = false;
  let again = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const schedule = () => {
    clearTimeout(timer);
    if (!stopped) timer = setTimeout(tick, intervalMs);
  };
  async function tick() {
    clearTimeout(timer);
    if (stopped) return;
    if (busy) { again = true; return; }
    if (o.pauseHidden && pageHidden()) { schedule(); return; }
    busy = true;
    try {
      await run();
    } catch (e) {
      if (!stopped) o.onError?.(e);
    } finally {
      busy = false;
    }
    if (stopped) return;
    if (again) { again = false; void tick(); return; }
    schedule();
  }
  const onVisibility = () => { if (!pageHidden()) void tick(); };
  if (typeof document !== 'undefined') document.addEventListener('visibilitychange', onVisibility);
  void tick();
  return {
    now: () => { void tick(); },
    stop: () => {
      stopped = true;
      clearTimeout(timer);
      if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onVisibility);
    },
  };
}

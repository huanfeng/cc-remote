import { useEffect, useState, useSyncExternalStore } from 'react';

/** ZCode's mobile breakpoint: at or below it the single-column mobile shell is used. */
export const MOBILE_QUERY = '(max-width: 767px)';
export const COARSE_QUERY = '(hover: none) and (pointer: coarse)';

export function useMediaQuery(q: string): boolean {
  return useSyncExternalStore(
    cb => {
      const m = window.matchMedia(q);
      m.addEventListener('change', cb);
      return () => m.removeEventListener('change', cb);
    },
    () => window.matchMedia(q).matches,
    () => false,
  );
}

export const useIsMobile = () => useMediaQuery(MOBILE_QUERY);
export const useIsCoarse = () => useMediaQuery(COARSE_QUERY);

/** Re-renders every `ms` so relative times ("3 分钟前") stay current. */
export function useNow(ms = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), ms);
    return () => window.clearInterval(t);
  }, [ms]);
  return now;
}

/**
 * The single Escape rule: a component that handles Escape itself calls
 * preventDefault() (Radix layers already do). This listener runs in the bubble
 * phase and only fires for an Escape nobody claimed, so an open dialog, menu
 * or popover never lets Escape stop a running turn.
 */
export function useUnclaimedEscape(handler: (() => void) | undefined) {
  useEffect(() => {
    if (!handler) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented || e.isComposing) return;
      const target = e.target as HTMLElement | null;
      if (target?.closest('[data-terminal]')) return; // Escape belongs to the terminal there
      handler();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [handler]);
}

// "Show this call in the transcript" (the task panel dispatches a window event
// 'cc-locate-call' with { toolUseId }). The target lives in a tiny store so only
// the groups and cards that contain it re-render and open themselves.
import { create } from 'zustand';

export const useLocate = create<{ id?: string; nonce: number }>(() => ({ nonce: 0 }));

export function locateCall(id: string) {
  useLocate.setState(s => ({ id, nonce: s.nonce + 1 }));
}

/** The locate nonce when the target is one of these ids, else 0 (open on change). */
export function useLocateNonce(matches: (id: string) => boolean): number {
  return useLocate(s => s.id && matches(s.id) ? s.nonce : 0);
}

/** Scrolls the call's element into view and flashes it (~1.5s fade of the accent colour). */
export function revealCall(root: HTMLElement, id: string): boolean {
  const nodes = root.querySelectorAll<HTMLElement>(`[data-call-id="${CSS.escape(id)}"]`);
  const el = nodes[nodes.length - 1];
  if (!el) return false;
  el.scrollIntoView({ behavior: 'smooth', block: 'center' });
  const accent = getComputedStyle(document.documentElement).getPropertyValue('--color-accent').trim() || 'rgba(127,127,127,0.18)';
  el.animate([{ backgroundColor: accent }, { backgroundColor: accent, offset: 0.3 }, { backgroundColor: 'transparent' }], { duration: 1500, easing: 'ease-out' });
  return true;
}

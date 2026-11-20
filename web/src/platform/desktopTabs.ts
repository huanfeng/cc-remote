import { emitTo, listen } from '@tauri-apps/api/event';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { DESKTOP, DESKTOP_TAB } from './desktop';
import { DesktopTabHistory } from '@/domain/desktopTabHistory';
import { isUsageHistory } from '@/domain/usageHistoryRoute';
import { useApp } from '@/store/app';
import { dismissOverlayShortcut, overlayHistory } from '@/lib/overlayHistory';
import { useConnection, type Phase } from '@/features/auth/connection';
import { attentionCounts } from '@/domain/attention';
import { useCompletionRead } from '@/store/completionRead';
import { hostKey } from '@/lib/hosts';

export const DESKTOP_OPEN_HOST = 'desktop-open-host';
export const DESKTOP_TAB_STATE = 'desktop-tab-state';
export const DESKTOP_TAB_COMMAND = 'desktop-tab-command';
export const DESKTOP_SHORTCUT = 'desktop-shortcut';
export const DESKTOP_ABOUT = 'desktop-open-about';
export type DesktopHostRequest = { hostId: string; hash: string; reload: boolean };
export type DesktopOpenHost = DesktopHostRequest;
export type DesktopTabState = {
  label: string; hash: string; title: string; canBack: boolean; canForward: boolean;
  phase: Phase; error: string; waiting: number; done?: number;
};
export type DesktopTabCommand = { action: 'state' | 'back' | 'forward' | 'reload' | 'navigate' | 'settings'; hash?: string };
export type DesktopShortcut = { action: 'next' | 'previous' | 'close' | 'new' | 'new-window' | 'reopen' };

const shell = () => ({ kind: 'Webview' as const, label: getCurrentWindow().label });
export function requestDesktopHost(hostId: string, hash = '', reload = false) {
  if (!DESKTOP) return;
  void emitTo(shell(), DESKTOP_OPEN_HOST, { hostId, hash, reload } satisfies DesktopHostRequest).catch(console.error);
}
export function requestDesktopAbout() {
  if (DESKTOP) void emitTo(shell(), DESKTOP_ABOUT).catch(console.error);
}

/** Keep stores, drafts and route history inside the server's Webview. */
export function installDesktopTabNavigation() {
  if (!DESKTOP_TAB) return () => {};
  const label = DESKTOP_TAB;
  const storageKey = `ccn-desktop-navigation:${label}`;
  let saved;
  try { saved = JSON.parse(sessionStorage.getItem(storageKey) || 'null'); } catch { /* unavailable */ }
  const journal = new DesktopTabHistory(location.hash, saved);
  let disposed = false;
  const report = () => {
    if (disposed) return;
    journal.record(location.hash);
    try { sessionStorage.setItem(storageKey, JSON.stringify(journal)); } catch { /* unavailable */ }
    const connection = useConnection.getState();
    const app = useApp.getState();
    const records = useCompletionRead.getState().records;
    const attention = app.sessionsLoaded && connection.phase !== 'signed-out' ? attentionCounts(app.sessions, session => records[hostKey(session.id)]) : { waiting: 0, done: 0, total: 0 };
    void emitTo(shell(), DESKTOP_TAB_STATE, {
      label, hash: location.hash, title: document.title, canBack: journal.canBack || overlayHistory().hasOpen, canForward: journal.canForward,
      phase: connection.phase, error: connection.error || connection.offline,
      waiting: attention.waiting, done: attention.done,
    } satisfies DesktopTabState).catch(console.error);
  };
  const navigate = (hash: string) => {
    if (hash && !hash.startsWith('#')) return;
    if (location.hash === hash) { report(); return; }
    location.hash = hash;
  };
  const command = ({ action, hash }: DesktopTabCommand) => {
    if (disposed) return;
    if (action === 'state') { report(); return; }
    if (action === 'reload') { location.reload(); return; }
    if (action === 'settings') { useApp.getState().open({ type: 'settings' }); return; }
    if (action === 'back' && overlayHistory().dismiss()) return;
    if (action === 'navigate') {
      if (hash !== undefined) {
        navigate(hash);
        // A tray request can reopen a history route whose hash did not change.
        useApp.getState().open(isUsageHistory(hash) ? { type: 'status' } : null);
      }
      return;
    }
    const target = journal.move(action === 'back' ? -1 : 1);
    if (target !== undefined) {
      // Replacing the hash still emits hashchange. Avoid mixing browser-global
      // history with this tab's bounded route journal.
      location.replace(location.pathname + location.search + target);
      useApp.getState().open(isUsageHistory(target) ? { type: 'status' } : null);
      report();
    }
  };
  const onKey = (event: KeyboardEvent) => {
    if (dismissOverlayShortcut(event)) return;
    if (event.isComposing) return;
    const modifier = event.ctrlKey || event.metaKey;
    const key = event.key.toLowerCase();
    let action: DesktopShortcut['action'] | undefined;
    if (modifier && !event.altKey) {
      if (key === 'tab') action = event.shiftKey ? 'previous' : 'next';
      else if (key === 'w' && !event.shiftKey) action = 'close';
      else if (key === 't') action = event.shiftKey ? 'reopen' : 'new';
      else if (key === 'n' && event.shiftKey) action = 'new-window';
    }
    if (action) {
      event.preventDefault(); event.stopImmediatePropagation();
      void emitTo(shell(), DESKTOP_SHORTCUT, { action } satisfies DesktopShortcut).catch(console.error);
    } else if ((!modifier && key === 'f5') || (modifier && key === 'r')) {
      event.preventDefault(); event.stopImmediatePropagation(); command({ action: 'reload' });
    } else if (event.altKey && !modifier && (key === 'arrowleft' || key === 'arrowright')) {
      event.preventDefault(); event.stopImmediatePropagation(); command({ action: key === 'arrowleft' ? 'back' : 'forward' });
    }
  };
  const observer = new MutationObserver(report);
  observer.observe(document.head, { childList: true, subtree: true, characterData: true });
  window.addEventListener('hashchange', report);
  window.addEventListener('keydown', onKey, true);
  const stopApp = useApp.subscribe(report);
  const stopConnection = useConnection.subscribe(report);
  const stopOverlays = overlayHistory().subscribe(report);
  const stopCompletion = useCompletionRead.subscribe(report);
  let stopCommands: (() => void) | undefined;
  void listen<DesktopTabCommand>(DESKTOP_TAB_COMMAND, event => command(event.payload), { target: { kind: 'Webview', label } })
    .then(stop => { if (disposed) stop(); else { stopCommands = stop; report(); } }).catch(console.error);
  return () => {
    stopOverlays();
    stopCompletion();
    disposed = true; observer.disconnect(); stopApp(); stopConnection(); stopCommands?.();
    window.removeEventListener('hashchange', report); window.removeEventListener('keydown', onKey, true);
  };
}

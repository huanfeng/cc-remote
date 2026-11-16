import { invoke } from '@tauri-apps/api/core';
import { listen } from '@tauri-apps/api/event';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { create } from 'zustand';
import { DESKTOP, DESKTOP_TAB } from './desktop';
import { requestDesktopAbout } from './desktopTabs';
import { isCurrentUpdate } from '@/domain/desktopUpdate';

export interface DesktopUpdateStatus {
  revision: number;
  currentVersion: string;
  phase: 'idle' | 'checking' | 'available' | 'downloading' | 'installing' | 'updated' | 'error';
  version: string | null;
  notes: string | null;
  downloaded: number;
  total: number | null;
  error: string | null;
}

export const useDesktopUpdate = create<{ status: DesktopUpdateStatus | null; connectionError: string | null; aboutOpen: boolean }>(() => ({ status: null, connectionError: null, aboutOpen: false }));
export const openDesktopAbout = () => {
  if (DESKTOP_TAB) requestDesktopAbout();
  else useDesktopUpdate.setState({ aboutOpen: true });
};
const accept = (status: DesktopUpdateStatus) => useDesktopUpdate.setState(previous =>
  isCurrentUpdate(previous.status, status) ? { status, connectionError: null } : previous);

export function subscribeDesktopUpdate() {
  if (!DESKTOP) return () => {};
  let disposed = false;
  let unlisten: (() => void) | undefined;
  void listen<DesktopUpdateStatus>('desktop-update', event => { if (!disposed) accept(event.payload); }).then(stop => {
    if (disposed) { stop(); return; }
    unlisten = stop;
    return invoke<DesktopUpdateStatus>('desktop_update_status').then(status => { if (!disposed) accept(status); });
  }).catch(error => { if (!disposed) useDesktopUpdate.setState({ connectionError: String(error) }); });
  return () => { disposed = true; unlisten?.(); };
}

export async function checkDesktopUpdate() {
  try { accept(await invoke<DesktopUpdateStatus>('desktop_update_check')); }
  catch (error) { useDesktopUpdate.setState({ connectionError: String(error) }); }
}

export async function installDesktopUpdate() {
  try { await invoke('desktop_update_install'); }
  catch (error) { useDesktopUpdate.setState({ connectionError: String(error) }); }
}

/** Consume tray requests after subscribing, including before the first WebView mount. */
export function subscribeDesktopAbout() {
  if (!DESKTOP || DESKTOP_TAB || getCurrentWindow().label !== 'main') return () => {};
  let disposed = false;
  let unlisten: (() => void) | undefined;
  let pending = false;
  let running = false;
  const drain = async () => {
    pending = true;
    if (running) return;
    running = true;
    try {
      while (pending && !disposed) {
        pending = false;
        const requested = await invoke<boolean>('take_desktop_about_request');
        if (requested && !disposed) openDesktopAbout();
      }
    } catch (error) {
      if (!disposed) useDesktopUpdate.setState({ connectionError: String(error) });
    } finally { running = false; }
  };
  void listen('desktop-about-requested', () => { void drain(); }).then(stop => {
    if (disposed) { stop(); return; }
    unlisten = stop;
    return drain();
  }).catch(error => { if (!disposed) useDesktopUpdate.setState({ connectionError: String(error) }); });
  return () => { disposed = true; unlisten?.(); };
}

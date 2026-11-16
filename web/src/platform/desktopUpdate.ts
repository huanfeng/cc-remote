import { invoke } from '@tauri-apps/api/core';
import { listen } from '@tauri-apps/api/event';
import { create } from 'zustand';
import { DESKTOP } from './desktop';
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

export const useDesktopUpdate = create<{ status: DesktopUpdateStatus | null; connectionError: string | null }>(() => ({ status: null, connectionError: null }));
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

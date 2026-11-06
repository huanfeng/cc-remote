// Saved host connections. Only the standalone page and the desktop app hold
// more than one host; a page served by a host is bound to that host and shows
// no switcher list. Phase 2 ports the old client's host store (hosts.ts: LAN
// switching, relocation, per-host storage keys) behind this same store.
import { create } from 'zustand';

export interface HostSummary {
  id: string;
  name: string;
  url: string;
  color: string;
  /** Last ping: null = not yet known. */
  ping: { online: boolean; running: number; waiting: number } | null;
}

interface HostsState {
  hosts: HostSummary[];
  currentId: string;
  set(patch: Partial<Pick<HostsState, 'hosts' | 'currentId'>>): void;
}

export const useHosts = create<HostsState>()(set => ({
  hosts: [],
  currentId: '',
  set: patch => set(patch),
}));

/** True when this page can hold several hosts (standalone build or desktop app). */
export const multiHost = () =>
  import.meta.env.MODE === 'standalone' || (typeof window !== 'undefined' && !!window.__CC_DESKTOP__);

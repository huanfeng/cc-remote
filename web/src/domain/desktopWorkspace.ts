/** Only host IDs and routes are persisted here; credentials stay in the host store. */
export interface WorkspaceTab {
  label: string;
  hostId: string;
  hash: string;
  title: string;
  canBack: boolean;
  canForward: boolean;
  waiting: number;
  done?: number;
  phase?: 'connecting' | 'signed-out' | 'ready';
  error?: string;
}

export interface Workspace {
  tabs: WorkspaceTab[];
  active: string | null;
  closed: Pick<WorkspaceTab, 'hostId' | 'hash'>[];
}

export const emptyWorkspace = (): Workspace => ({ tabs: [], active: null, closed: [] });

export function addWorkspaceTab(state: Workspace, tab: WorkspaceTab): Workspace {
  const existing = state.tabs.find(x => x.hostId === tab.hostId);
  return { ...state, tabs: existing ? state.tabs : [...state.tabs, tab], active: existing?.label ?? tab.label };
}

export function closeWorkspaceTab(state: Workspace, label: string): Workspace {
  const index = state.tabs.findIndex(x => x.label === label);
  if (index < 0) return state;
  const tabs = state.tabs.filter(x => x.label !== label);
  return { tabs, active: state.active === label ? (tabs[index] ?? tabs[index - 1])?.label ?? null : state.active,
    closed: [...state.closed, { hostId: state.tabs[index].hostId, hash: state.tabs[index].hash }].slice(-20) };
}

export function moveWorkspaceTab(state: Workspace, from: string, to: string): Workspace {
  const source = state.tabs.findIndex(x => x.label === from);
  const target = state.tabs.findIndex(x => x.label === to);
  if (source < 0 || target < 0 || source === target) return state;
  const tabs = [...state.tabs];
  tabs.splice(target, 0, tabs.splice(source, 1)[0]);
  return { ...state, tabs };
}

export function savedWorkspace(state: Workspace): string {
  return JSON.stringify({ tabs: state.tabs.map(({ hostId, hash }) => ({ hostId, hash })),
    activeHost: state.tabs.find(x => x.label === state.active)?.hostId });
}

export function restoreWorkspace(raw: string | null, known: Set<string>): { tabs: { hostId: string; hash: string }[]; activeHost?: string } {
  try {
    const value = JSON.parse(raw ?? 'null');
    if (!value || !Array.isArray(value.tabs)) return { tabs: [] };
    const seen = new Set<string>();
    const tabs: { hostId: string; hash: string }[] = [];
    for (const item of value.tabs.slice(0, 32)) {
      if (!item || typeof item.hostId !== 'string' || !known.has(item.hostId) || seen.has(item.hostId)) continue;
      seen.add(item.hostId);
      tabs.push({ hostId: item.hostId, hash: typeof item.hash === 'string' && item.hash.startsWith('#/') ? item.hash : '' });
    }
    return { tabs, activeHost: seen.has(value.activeHost) ? value.activeHost : undefined };
  } catch { return { tabs: [] }; }
}

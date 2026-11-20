import { invoke } from '@tauri-apps/api/core';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { getAllWebviews } from '@tauri-apps/api/webview';
import { emitTo, listen } from '@tauri-apps/api/event';
import { create } from 'zustand';
import { DESKTOP, DESKTOP_TAB } from './desktop';
import { loadHosts } from '@/lib/hosts';
import { openDesktopAbout, useDesktopUpdate } from './desktopUpdate';
import { addWorkspaceTab, closeWorkspaceTab, emptyWorkspace, moveWorkspaceTab, restoreWorkspace, savedWorkspace, type Workspace, type WorkspaceTab } from '@/domain/desktopWorkspace';
import { DESKTOP_OPEN_HOST, DESKTOP_TAB_STATE, DESKTOP_TAB_COMMAND, DESKTOP_SHORTCUT, DESKTOP_ABOUT, type DesktopTabCommand, type DesktopTabState, type DesktopOpenHost, type DesktopShortcut } from './desktopTabs';

const windowLabel = DESKTOP ? getCurrentWindow().label : 'main';
const KEY = 'cc-desktop-tabs:v1';
let sequence = 0;
const makeTab = (hostId: string, hash = ''): WorkspaceTab => ({
  label: `tab-${windowLabel}-${Date.now().toString(36)}-${++sequence}`, hostId, hash, title: '', canBack: false, canForward: false, waiting: 0, done: 0,
});
function initial(): Workspace {
  if (!DESKTOP || DESKTOP_TAB) return emptyWorkspace();
  const list = loadHosts();
  const explicit = new URLSearchParams(location.search).get('host');
  let restored: ReturnType<typeof restoreWorkspace> = { tabs: [] };
  try { if (windowLabel === 'main' && !explicit) restored = restoreWorkspace(localStorage.getItem(KEY), new Set(list.hosts.map(x => x.id))); } catch { /* storage unavailable */ }
  if (explicit && list.hosts.some(x => x.id === explicit)) restored.tabs = [{ hostId: explicit, hash: location.hash }];
  // On the first launch retain the server selected by previous app versions.
  if (!restored.tabs.length && list.hosts.length && !hasSavedWorkspace()) restored.tabs = [{ hostId: list.hosts.find(x => x.id === list.current)?.id || list.hosts[0].id, hash: '' }];
  const state = restored.tabs.reduce((state, tab) => addWorkspaceTab(state, makeTab(tab.hostId, tab.hash)), emptyWorkspace());
  state.active = state.tabs.find(x => x.hostId === restored.activeHost)?.label ?? state.active;
  return state;
}
function hasSavedWorkspace() { try { return windowLabel === 'main' && localStorage.getItem(KEY) !== null; } catch { return false; } }

export const useDesktopWorkspace = create<Workspace & { panel: 'hosts' | 'menu' | null; error: string }>(() => ({ ...initial(), panel: null, error: '' }));
const opened = new Set<string>();
const ready = new Set<string>();
const commands = new Map<string, DesktopTabCommand[]>();
let writes = Promise.resolve();
let covered = false;
let listenersReady = false;
let scanned = false;
let activated: string | null | undefined;

/** One queue owns native view mutations, including requests arriving during creation. */
export function reconcileDesktopTabs() {
  if (!listenersReady) return;
  writes = writes.catch(() => {}).then(async () => {
    if (!scanned) {
      // A shell refresh leaves native children alive. Reclaim them before
      // recreating the saved workspace rather than leaking hidden contexts.
      for (const view of await getAllWebviews()) if (view.window.label === windowLabel && view.label.startsWith(`tab-${windowLabel}-`)) opened.add(view.label);
      scanned = true;
    }
    const snapshot = useDesktopWorkspace.getState();
    for (const label of opened) {
      if (!snapshot.tabs.some(x => x.label === label)) { await invoke('desktop_tab_close', { label }); opened.delete(label); ready.delete(label); commands.delete(label); }
    }
    for (const tab of snapshot.tabs) {
      if (opened.has(tab.label)) continue;
      await invoke('desktop_tab_open', { label: tab.label, hostId: tab.hostId, hash: tab.hash });
      opened.add(tab.label);
    }
    const current = useDesktopWorkspace.getState();
    const target = covered || current.panel || current.error ? null : current.active;
    // A new request may select a tab while this snapshot's views are still
    // being created. Its queued reconciliation will create and activate it.
    if (target && !opened.has(target)) return;
    if (activated !== target) { await invoke('desktop_tab_activate', { label: target }); activated = target; }
  }).catch(async error => {
    useDesktopWorkspace.setState({ error: String(error) });
    // Native children sit above HTML. Expose the shell's recovery controls.
    try { await invoke('desktop_tab_activate', { label: null }); activated = null; } catch { /* original error remains visible in the window menu */ }
  });
}

function change(patch: Partial<Workspace & { panel: 'hosts' | 'menu' | null; error: string }>, native = true) {
  useDesktopWorkspace.setState(patch);
  if (windowLabel === 'main') { try { localStorage.setItem(KEY, savedWorkspace(useDesktopWorkspace.getState())); } catch { /* storage unavailable */ } }
  if (native) reconcileDesktopTabs();
}

export function activateDesktopTab(label: string) {
  if (!useDesktopWorkspace.getState().tabs.some(x => x.label === label)) return;
  useDesktopUpdate.setState({ aboutOpen: false });
  change({ active: label, panel: null, error: '' });
}
export function showDesktopHosts() { useDesktopUpdate.setState({ aboutOpen: false }); change({ panel: 'hosts', error: '' }); }
export function showDesktopMenu() { change({ panel: useDesktopWorkspace.getState().panel === 'menu' ? null : 'menu' }); }
export function closeDesktopTab(label: string) { change(closeWorkspaceTab(useDesktopWorkspace.getState(), label)); }
export function reorderDesktopTab(from: string, to: string) { change(moveWorkspaceTab(useDesktopWorkspace.getState(), from, to)); }
export function reopenDesktopTab() {
  const state = useDesktopWorkspace.getState();
  const closed = [...state.closed];
  let tab;
  while ((tab = closed.pop())) {
    if (!loadHosts().hosts.some(x => x.id === tab!.hostId)) continue;
    change({ closed });
    openDesktopHost({ ...tab, reload: false });
    return;
  }
  change({ closed: [] });
}
export function openDesktopHost(request: DesktopOpenHost) {
  if (!loadHosts().hosts.some(x => x.id === request.hostId)) return;
  const state = useDesktopWorkspace.getState();
  const existing = state.tabs.find(x => x.hostId === request.hostId);
  useDesktopUpdate.setState({ aboutOpen: false });
  if (existing && request.reload) {
    // Recreate on a token change to discard the previous sign-in's caches.
    change({ ...closeWorkspaceTab(state, existing.label), closed: state.closed });
    change({ ...addWorkspaceTab(useDesktopWorkspace.getState(), makeTab(request.hostId, request.hash)), panel: null, error: '' });
  } else {
    change({ ...addWorkspaceTab(state, makeTab(request.hostId, request.hash)), panel: null, error: '' });
    if (existing && request.hash) void sendDesktopTabCommand({ action: 'navigate', hash: request.hash });
  }
}

export async function sendDesktopTabCommand(command: DesktopTabCommand) {
  const state = useDesktopWorkspace.getState();
  if (!state.active) return;
  // Creation must complete before targeting the child. Its listener-ready state
  // is reported separately; navigation after open uses the initial URL instead.
  await writes;
  if (!ready.has(state.active)) {
    commands.set(state.active, [...(commands.get(state.active) ?? []), command]);
    return;
  }
  if (command.action === 'reload') ready.delete(state.active);
  await emitTo({ kind: 'Webview', label: state.active }, DESKTOP_TAB_COMMAND, command);
}
export function newDesktopWindow() {
  const active = useDesktopWorkspace.getState().tabs.find(x => x.label === useDesktopWorkspace.getState().active);
  const url = new URL(location.href);
  url.searchParams.delete('desktop-tab');
  if (active) url.searchParams.set('host', active.hostId); else url.searchParams.delete('host');
  url.hash = active?.hash ?? '';
  window.open(url.href);
}
export function desktopShortcut(action: DesktopShortcut['action']) {
  const state = useDesktopWorkspace.getState();
  if (action === 'new') showDesktopHosts();
  else if (action === 'new-window') newDesktopWindow();
  else if (action === 'reopen') reopenDesktopTab();
  else if (action === 'close' && state.active) closeDesktopTab(state.active);
  else if ((action === 'next' || action === 'previous') && state.tabs.length) {
    const index = state.tabs.findIndex(x => x.label === state.active);
    const next = (index + (action === 'next' ? 1 : -1) + state.tabs.length) % state.tabs.length;
    activateDesktopTab(state.tabs[next].label);
  }
}

export function subscribeDesktopWorkspace() {
  let disposed = false;
  const stops: (() => void)[] = [];
  const registrations: Promise<unknown>[] = [];
  const register = <T,>(event: string, callback: (payload: T) => void) => {
    registrations.push(listen<T>(event, e => { if (!disposed) callback(e.payload); }, { target: { kind: 'Webview', label: windowLabel } })
      .then(stop => { if (disposed) stop(); else stops.push(stop); }));
  };
  register<DesktopOpenHost>(DESKTOP_OPEN_HOST, openDesktopHost);
  register<DesktopTabState>(DESKTOP_TAB_STATE, state => {
    const current = useDesktopWorkspace.getState();
    if (!current.tabs.some(x => x.label === state.label)) return;
    // Child metadata cannot change the tab's server association.
    const { label, hash, title, canBack, canForward, waiting, done, phase, error } = state;
    ready.add(label);
    change({ tabs: current.tabs.map(x => x.label === label ? { ...x, hash, title, canBack, canForward, waiting, done: done ?? 0, phase, error } : x) }, false);
    const pending = commands.get(label);
    commands.delete(label);
    if (pending) void pending.reduce((previous, command) => previous.then(() => emitTo({ kind: 'Webview', label }, DESKTOP_TAB_COMMAND, command)), Promise.resolve());
  });
  register<DesktopShortcut>(DESKTOP_SHORTCUT, request => desktopShortcut(request.action));
  register(DESKTOP_ABOUT, () => openDesktopAbout());
  const about = useDesktopUpdate.subscribe(state => {
    if (covered === state.aboutOpen) return;
    covered = state.aboutOpen;
    reconcileDesktopTabs();
  });
  covered = useDesktopUpdate.getState().aboutOpen;
  void Promise.all(registrations).then(async () => {
    if (disposed) return;
    listenersReady = true;
    reconcileDesktopTabs();
    await writes;
    if (disposed) return;
    for (const label of opened) void emitTo({ kind: 'Webview', label }, DESKTOP_TAB_COMMAND, { action: 'state' });
  }).catch(error => { if (!disposed) useDesktopWorkspace.setState({ error: String(error) }); });
  const hostChange = () => {
    const known = new Set(loadHosts().hosts.map(x => x.id));
    const state = useDesktopWorkspace.getState();
    let updated: Workspace = state;
    for (const tab of state.tabs) if (!known.has(tab.hostId)) updated = closeWorkspaceTab(updated, tab.label);
    if (updated !== state) change({ ...updated, closed: updated.closed.filter(tab => known.has(tab.hostId)) });
  };
  window.addEventListener('storage', hostChange);
  return () => { disposed = true; listenersReady = false; for (const stop of stops) stop(); about(); window.removeEventListener('storage', hostChange); };
}

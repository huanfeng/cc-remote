import { createStore } from 'zustand/vanilla';
import type { Backend } from '@/backend/backend';
import type { Attachment } from '@/backend/types';
import { splitLine } from './fileView.ts';

export type FileEntry = { path: string; base?: string[] };
export type DockEntry =
  | ({ kind: 'file'; source?: boolean } & FileEntry)
  | { kind: 'attachment'; attachment: Attachment };

export const fileEntryKey = ({ path, base }: FileEntry) => JSON.stringify([path, base ?? []]);

function tabId(entry: DockEntry): string {
  return encodeURIComponent(entry.kind === 'file'
    ? `file:${fileEntryKey({ path: splitLine(entry.path).path, base: entry.base })}`
    : `attachment:${entry.attachment.id}:${entry.attachment.url}`);
}

type DockTab = { id: string; entry: DockEntry };
type Dock = { tabs: DockTab[]; active: string; collapsed: boolean };

function createFileDock() {
  return createStore<{
    docks: ReadonlyMap<string, Dock>;
    open: (key: string, entry: DockEntry) => void;
    select: (key: string, id: string) => void;
    close: (key: string, id: string) => void;
    collapse: (key: string, collapsed: boolean) => void;
  }>((set) => {
    const update = (key: string, change: (dock: Dock | undefined) => Dock | undefined) => {
      set(state => {
        const dock = state.docks.get(key);
        const next = change(dock);
        if (next === dock) return state;
        const docks = new Map(state.docks);
        if (next) docks.set(key, next);
        else docks.delete(key);
        return { docks };
      });
    };
    return {
      docks: new Map(),
      open: (key, entry) => update(key, dock => {
        const id = tabId(entry);
        const tab = { id, entry };
        const tabs = dock?.tabs ?? [];
        return {
          tabs: tabs.some(old => old.id === id) ? tabs.map(old => old.id === id ? tab : old) : [...tabs, tab],
          active: id,
          collapsed: false,
        };
      }),
      select: (key, id) => update(key, dock => dock?.tabs.some(tab => tab.id === id) ? { ...dock, active: id, collapsed: false } : dock),
      close: (key, id) => update(key, dock => {
        const index = dock?.tabs.findIndex(tab => tab.id === id) ?? -1;
        if (!dock || index < 0) return dock;
        const tabs = dock.tabs.filter(tab => tab.id !== id);
        if (tabs.length === 0) return undefined;
        const active = dock.active === id ? (tabs[index] ?? tabs[index - 1]).id : dock.active;
        return { ...dock, tabs, active };
      }),
      collapse: (key, collapsed) => update(key, dock => dock ? { ...dock, collapsed } : undefined),
    };
  });
}

// Surfaces and the workspace share the host's backend. Tabs stay scoped to it
// without retaining a backend or tab contents after a host disconnects.
const stores = new WeakMap<Backend, ReturnType<typeof createFileDock>>();

export function fileDockStore(backend: Backend) {
  const existing = stores.get(backend);
  if (existing) return existing;
  const store = createFileDock();
  stores.set(backend, store);
  return store;
}

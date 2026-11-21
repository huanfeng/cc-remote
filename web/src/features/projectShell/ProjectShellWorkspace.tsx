import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { useBackend } from '@/backend/backend';
import type { ProjectShell } from '@/backend/types';
import { hostKey } from '@/lib/hosts';
import { overlayHistory } from '@/lib/overlayHistory';
import { usePrefs } from '@/lib/prefs';
import { DESKTOP } from '@/platform/desktop';
import { hasFeature, selectionKey, useApp, useSelectedSession } from '@/store/app';
import { matchesProjectShellShortcut } from './projectShellKeys';
import { isOpenProjectShell, matchesShellPath, shellPathKey } from './projectShellStatus';

export interface ProjectShellTarget { cwd: string; shellId?: string; runId?: string }

interface RetainedTab {
  target: ProjectShellTarget;
  shellId?: string;
  runId?: string;
}

interface PanelState {
  tabs: RetainedTab[];
  active: ProjectShellTarget;
  visible: boolean;
}

function targetKey(target: ProjectShellTarget) {
  return JSON.stringify([target.shellId, target.runId, shellPathKey(target.cwd)]);
}

export function findOpenProjectShell(shells: ProjectShell[], cwd: string): ProjectShell | undefined {
  const matches = shells.filter(shell => isOpenProjectShell(shell) && matchesShellPath(shell, cwd));
  return matches.find(shell => shell.default) ?? matches.find(shell => !shell.tabId) ?? matches[0];
}

interface ProjectShellWorkspaceState {
  enabled: boolean;
  pinned: boolean;
  target: ProjectShellTarget | null;
  activeTarget: ProjectShellTarget | null;
  retainedTargets: readonly ProjectShellTarget[];
  shells: ProjectShell[];
  shellListState: 'loading' | 'ready' | 'error';
  open(target: ProjectShellTarget): void;
  retain(target: ProjectShellTarget, activate: boolean): void;
  close(): void;
  hideForConversation(): void;
  togglePinned(): void;
  end(target: ProjectShellTarget, preferred?: ProjectShellTarget): void;
  bind(target: ProjectShellTarget, shellId: string, runId?: string): void;
  shellIdFor(target: ProjectShellTarget): string | undefined;
  targetKey(target: ProjectShellTarget): string;
  toggle(target: ProjectShellTarget): void;
  isOpen(cwd: string): boolean;
  remember(shell: ProjectShell, alias?: string): void;
  finishRun(shellId: string, runId: string, state: 'exited' | 'stopped'): void;
}

const ProjectShellWorkspaceContext = createContext<ProjectShellWorkspaceState>({
  enabled: false, pinned: false, target: null, activeTarget: null, retainedTargets: [], shells: [], shellListState: 'loading',
  open() {}, retain() {}, close() {}, hideForConversation() {}, togglePinned() {}, end() {}, bind() {}, shellIdFor: item => item.shellId, targetKey, toggle() {}, isOpen: () => false, remember() {}, finishRun() {},
});

export const useProjectShellWorkspace = () => useContext(ProjectShellWorkspaceContext);

/** One workspace controls presentation. An individual terminal surface only needs shell/run IDs. */
export function ProjectShellWorkspace({ mobile = false, children }: { mobile?: boolean; children: ReactNode }) {
  const enabled = useApp(s => hasFeature(s.info, 'project-shell'));
  const key = useApp(s => selectionKey(s.selection));
  const cwd = useSelectedSession()?.cwd;
  const backend = useBackend();
  // Backend changes remount this boundary; callbacks below never share state between hosts.
  return <ProjectShellWorkspaceStateProvider key={hostKey(backend.kind)} {...{ enabled, selectionId: key, cwd, mobile, children }} />;
}

function ProjectShellWorkspaceStateProvider({ enabled, selectionId, cwd, mobile, children }: {
  enabled: boolean; selectionId: string; cwd?: string; mobile: boolean; children: ReactNode;
}) {
  const backend = useBackend();
  const [shells, setShells] = useState<ProjectShell[]>([]);
  const [shellListState, setShellListState] = useState<ProjectShellWorkspaceState['shellListState']>('loading');
  const [panel, setPanel] = useState<PanelState | null>(null);
  const [pinned, setPinned] = useState(false);
  const togglePinned = () => setPinned(value => !value);
  const lastTarget = panel?.active ?? null;
  const retainedTargets = panel?.tabs.map(tab => tab.target) ?? [];
  const target = panel?.visible ? panel.active : null;
  const focusBefore = useRef<HTMLElement | null>(null);
  const focusRestore = useRef<number>(0);
  const previousSelection = useRef(selectionId);
  const drawerOpen = useApp(s => s.drawerOpen);

  useEffect(() => {
    if (!enabled) return;
    return backend.watchProjectShells(next => {
      setShellListState('ready');
      setShells(current => {
        const records = new Map(current.map(shell => [shell.id, shell]));
        return next.map(shell => {
          const cached = records.get(shell.id);
          const aliases = [...new Set([...(shell.aliases ?? []), ...(cached?.aliases ?? [])])];
          return { ...shell, aliases };
        });
      });
    }, () => setShellListState('error'));
  }, [backend, enabled]);

  const remember = useCallback((shell: ProjectShell, alias?: string) => {
    setShells(current => {
      const before = current.find(item => item.id === shell.id);
      const aliases = [...new Set([...(before?.aliases ?? []), ...(shell.aliases ?? []), ...(alias ? [alias] : [])])];
      const updated = { ...shell, aliases };
      return before ? current.map(item => item === before ? updated : item) : [...current, updated];
    });
  }, []);

  const finishRun = useCallback((shellId: string, runId: string, state: 'exited' | 'stopped') => {
    setShells(current => current.map(shell =>
      shell.id === shellId && shell.runId === runId ? { ...shell, runId: undefined, state } : shell));
  }, []);

  const bind = useCallback((candidate: ProjectShellTarget, shellId: string, runId?: string) => {
    setPanel(current => {
      const tab = current?.tabs.find(item => item.target === candidate);
      if (!current || !tab || (tab.shellId === shellId && tab.runId === runId)) return current;
      const duplicate = current.tabs.find(item => item !== tab && item.shellId === shellId);
      const tabs = current.tabs.filter(item => item !== duplicate).map(item => item === tab ? { ...item, shellId, runId } : item);
      return { ...current, tabs, active: duplicate?.target === current.active ? candidate : current.active };
    });
  }, []);

  const shellIdFor = useCallback((candidate: ProjectShellTarget) => (
    panel?.tabs.find(tab => tab.target === candidate)?.shellId ?? candidate.shellId
  ), [panel?.tabs]);

  const matchesTarget = useCallback((candidate: ProjectShellTarget, path: string, shellId = candidate.shellId) => {
    if (shellPathKey(path) === shellPathKey(candidate.cwd)) return true;
    const shell = shells.find(item => item.id === shellId) ?? shells.find(item => matchesShellPath(item, candidate.cwd));
    return !!shell && matchesShellPath(shell, path);
  }, [shells]);

  const isOpen = useCallback((path: string) => !!target && matchesTarget(target, path, shellIdFor(target)), [matchesTarget, shellIdFor, target]);

  const restoreFocus = useCallback(() => {
    const before = focusBefore.current;
    focusBefore.current = null;
    cancelAnimationFrame(focusRestore.current);
    focusRestore.current = requestAnimationFrame(() => {
      if (before?.isConnected && !before.closest('[inert]')) before.focus();
      else (document.querySelector<HTMLElement>('[data-session-composer]') ?? document.querySelector<HTMLElement>('[data-project-shell-trigger]'))?.focus();
    });
  }, []);
  const hide = useCallback(() => {
    setPanel(current => current?.visible ? { ...current, visible: false } : current);
  }, []);
  const close = useCallback(() => {
    hide();
    restoreFocus();
  }, [hide, restoreFocus]);
  const hideForConversation = useCallback(() => {
    if (mobile || pinned || !target) return;
    cancelAnimationFrame(focusRestore.current);
    focusBefore.current = null;
    hide();
  }, [hide, mobile, pinned, target]);
  const end = useCallback((ended: ProjectShellTarget, preferred?: ProjectShellTarget) => {
    setPanel(current => {
      if (!current) return current;
      const index = current.tabs.findIndex(tab => tab.target === ended);
      if (index === -1) return current;

      const tab = current.tabs[index];
      const tabs = current.tabs.filter(item => item !== tab);
      if (ended !== current.active) return { ...current, tabs };
      if (preferred && preferred !== ended && preferred.shellId && preferred.shellId !== tab.shellId) {
        const existing = tabs.find(item => item.shellId === preferred.shellId);
        if (existing) return { ...current, tabs, active: existing.target };
        return { ...current, tabs: [...tabs, { target: preferred, shellId: preferred.shellId, runId: preferred.runId }], active: preferred };
      }
      const next = tabs[Math.min(index, tabs.length - 1)];
      if (next) return { ...current, tabs, active: next.target };
      return { tabs, active: { cwd: ended.cwd }, visible: false };
    });
  }, []);
  const ended = panel !== null && panel.tabs.length === 0;
  useLayoutEffect(() => {
    if (ended && focusBefore.current) restoreFocus();
  }, [ended, restoreFocus]);
  useEffect(() => () => cancelAnimationFrame(focusRestore.current), []);

  const retain = useCallback((next: ProjectShellTarget, activate: boolean) => {
    if (!enabled || !next.cwd) return;
    if (activate) {
      cancelAnimationFrame(focusRestore.current);
      if (!target) focusBefore.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      if (mobile) useApp.getState().set({ drawerOpen: false });
    }
    setPanel(current => {
      const active = current?.tabs.find(tab => tab.target === current.active);
      const sameProject = current && matchesTarget(current.active, next.cwd, active?.shellId);
      if (!activate && (!sameProject || !active)) return current;
      const tabs = sameProject ? current.tabs : [];
      if (!next.shellId && active && sameProject) return !activate || current.visible ? current : { ...current, visible: true };
      const saved = next.shellId ? undefined : findOpenProjectShell(shells, next.cwd);
      const item = saved ? { cwd: next.cwd, shellId: saved.id } : next;
      const existing = tabs.find(tab => tab.target === item || (item.shellId && tab.shellId === item.shellId));
      if (existing && (existing.target === item || !item.runId || existing.runId === item.runId)) {
        if (!activate || (current?.visible && current.active === existing.target)) return current;
        return { tabs, active: existing.target, visible: true };
      }
      const tab = { target: item, shellId: item.shellId, runId: item.runId };
      return {
        tabs: existing ? tabs.map(tabItem => tabItem === existing ? tab : tabItem) : [...tabs, tab],
        active: activate || !current || current.active === existing?.target ? item : current.active,
        visible: activate || Boolean(current?.visible),
      };
    });
  }, [enabled, matchesTarget, mobile, shells, target]);

  const open = useCallback((next: ProjectShellTarget) => retain(next, true), [retain]);

  const toggle = useCallback((next: ProjectShellTarget) => {
    if (isOpen(next.cwd)) close();
    else open(next);
  }, [close, isOpen, open]);

  useLayoutEffect(() => {
    const selectionChanged = previousSelection.current !== selectionId;
    previousSelection.current = selectionId;
    if (lastTarget && (!enabled || (selectionChanged && (!cwd || !matchesTarget(lastTarget, cwd, shellIdFor(lastTarget)))))) {
      cancelAnimationFrame(focusRestore.current);
      setPanel(null);
      focusBefore.current = null;
    } else if (selectionChanged && mobile && target) {
      const focus = requestAnimationFrame(() => {
        const section = document.getElementById('project-shell-panel');
        if (section && !section.hidden) {
          const textarea = section.querySelector<HTMLElement>('[data-shell-pane-active] textarea');
          (textarea ?? section).focus({ preventScroll: true });
        }
      });
      return () => cancelAnimationFrame(focus);
    } else if (selectionChanged) {
      hideForConversation();
    }
  }, [cwd, enabled, hideForConversation, lastTarget, matchesTarget, mobile, selectionId, shellIdFor, target]);

  const visible = target !== null;
  useLayoutEffect(() => {
    if (mobile && visible && !drawerOpen) return overlayHistory().add(close);
  }, [close, drawerOpen, mobile, visible]);

  useEffect(() => {
    if (!enabled) return;
    const onKey = (event: KeyboardEvent) => {
      const app = useApp.getState();
      if (app.surface || app.drawerOpen || document.querySelector('[aria-modal="true"][data-state="open"]')) return;
      if (!matchesProjectShellShortcut(event, usePrefs.getState().projectShellShortcut, DESKTOP) || (!cwd && !lastTarget)) return;
      event.preventDefault();
      event.stopPropagation();
      if (event.repeat) return;
      if (target) close();
      else if (lastTarget) open(lastTarget);
      else if (cwd) open({ cwd });
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [close, cwd, enabled, lastTarget, open, target]);

  return <ProjectShellWorkspaceContext.Provider value={{
    enabled, pinned, target, activeTarget: lastTarget, retainedTargets, shells, shellListState,
    open, retain, close, hideForConversation, togglePinned, end, bind, shellIdFor, targetKey, toggle, isOpen, remember, finishRun,
  }}>{children}</ProjectShellWorkspaceContext.Provider>;
}

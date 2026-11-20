import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { useBackend } from '@/backend/backend';
import type { ProjectShell } from '@/backend/types';
import { hostKey } from '@/lib/hosts';
import { overlayHistory } from '@/lib/overlayHistory';
import { usePrefs } from '@/lib/prefs';
import { DESKTOP } from '@/platform/desktop';
import { hasFeature, selectionKey, useApp, useSelectedSession } from '@/store/app';
import { matchesProjectShellShortcut } from './projectShellKeys';

export interface ProjectShellTarget { cwd: string; shellId?: string }

// Path spelling is only a lookup hint. The host owns project identity and returns aliases.
export function shellPathKey(cwd: string) {
  if (/^(?:[a-z]:[\\/]|\\\\)/i.test(cwd)) return cwd.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
  return cwd.replace(/\/+$/, '') || '/';
}

export function matchesShellPath(shell: ProjectShell, cwd: string): boolean {
  const key = shellPathKey(cwd);
  return [shell.cwd, ...(shell.aliases ?? [])].some(path => shellPathKey(path) === key);
}

interface ProjectShellWorkspaceState {
  enabled: boolean;
  target: ProjectShellTarget | null;
  retainedTarget: ProjectShellTarget | null;
  shells: ProjectShell[];
  open(target: ProjectShellTarget): void;
  close(): void;
  toggle(target: ProjectShellTarget): void;
  isOpen(cwd: string): boolean;
  remember(shell: ProjectShell, alias?: string): void;
}

const ProjectShellWorkspaceContext = createContext<ProjectShellWorkspaceState>({
  enabled: false, target: null, retainedTarget: null, shells: [], open() {}, close() {}, toggle() {}, isOpen: () => false, remember() {},
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
  const [panel, setPanel] = useState<{ target: ProjectShellTarget; visible: boolean } | null>(null);
  const retainedTarget = panel?.target ?? null;
  const target = panel?.visible ? panel.target : null;
  const focusBefore = useRef<HTMLElement | null>(null);
  const focusRestore = useRef<number>(0);
  const previousSelection = useRef(selectionId);
  const drawerOpen = useApp(s => s.drawerOpen);

  useEffect(() => {
    if (!enabled) return;
    return backend.watchProjectShells(next => setShells(current => {
      const records = new Map(current.map(shell => [shell.id, shell]));
      for (const shell of next) {
        const cached = records.get(shell.id);
        const aliases = [...new Set([...(shell.aliases ?? []), ...(cached?.aliases ?? [])])];
        records.set(shell.id, { ...shell, aliases });
      }
      return [...records.values()];
    }));
  }, [backend, enabled]);

  const remember = useCallback((shell: ProjectShell, alias?: string) => {
    setShells(current => {
      const before = current.find(item => item.id === shell.id);
      const aliases = [...new Set([...(before?.aliases ?? []), ...(shell.aliases ?? []), ...(alias ? [alias] : [])])];
      return [...current.filter(item => item.id !== shell.id), { ...shell, aliases }];
    });
  }, []);

  const matchesTarget = useCallback((candidate: ProjectShellTarget, path: string) => {
    if (shellPathKey(path) === shellPathKey(candidate.cwd)) return true;
    const shell = shells.find(item => item.id === candidate.shellId || matchesShellPath(item, candidate.cwd));
    return !!shell && matchesShellPath(shell, path);
  }, [shells]);

  const isOpen = useCallback((path: string) => !!target && matchesTarget(target, path), [matchesTarget, target]);

  const close = useCallback(() => {
    setPanel(current => current?.visible ? { ...current, visible: false } : current);
    const before = focusBefore.current;
    focusBefore.current = null;
    cancelAnimationFrame(focusRestore.current);
    focusRestore.current = requestAnimationFrame(() => {
      if (before?.isConnected && !before.closest('[inert]')) before.focus();
      else (document.querySelector<HTMLElement>('[data-session-composer]') ?? document.querySelector<HTMLElement>('[data-project-shell-trigger]'))?.focus();
    });
  }, []);
  useEffect(() => () => cancelAnimationFrame(focusRestore.current), []);

  const open = useCallback((next: ProjectShellTarget) => {
    if (!enabled || !next.cwd || isOpen(next.cwd)) return;
    cancelAnimationFrame(focusRestore.current);
    if (!target) focusBefore.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    if (mobile) useApp.getState().set({ drawerOpen: false });
    setPanel(current => ({
      target: current && ((next.shellId && current.target.shellId === next.shellId) || matchesTarget(current.target, next.cwd)) ? current.target : next,
      visible: true,
    }));
  }, [enabled, isOpen, matchesTarget, mobile, target]);

  const toggle = useCallback((next: ProjectShellTarget) => {
    if (isOpen(next.cwd)) close();
    else open(next);
  }, [close, isOpen, open]);

  useLayoutEffect(() => {
    const selectionChanged = previousSelection.current !== selectionId;
    previousSelection.current = selectionId;
    if (retainedTarget && (!enabled || (selectionChanged && (!cwd || !matchesTarget(retainedTarget, cwd))))) {
      cancelAnimationFrame(focusRestore.current);
      setPanel(null);
      focusBefore.current = null;
    } else if (selectionChanged && target) {
      const focus = requestAnimationFrame(() => {
        const section = document.getElementById('project-shell-panel');
        if (section && !section.hidden) (section.querySelector<HTMLElement>('textarea') ?? section).focus({ preventScroll: true });
      });
      return () => cancelAnimationFrame(focus);
    }
  }, [cwd, enabled, matchesTarget, retainedTarget, selectionId, target]);

  useLayoutEffect(() => {
    if (mobile && target && !drawerOpen) return overlayHistory().add(close);
  }, [close, drawerOpen, mobile, target]);

  useEffect(() => {
    if (!enabled) return;
    const onKey = (event: KeyboardEvent) => {
      const app = useApp.getState();
      if (app.surface || app.drawerOpen || document.querySelector('[aria-modal="true"][data-state="open"]')) return;
      if (!matchesProjectShellShortcut(event, usePrefs.getState().projectShellShortcut, DESKTOP) || (!cwd && !retainedTarget)) return;
      event.preventDefault();
      event.stopPropagation();
      if (event.repeat) return;
      if (target) close();
      else if (retainedTarget) open(retainedTarget);
      else if (cwd) open({ cwd });
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [close, cwd, enabled, open, retainedTarget, target]);

  return <ProjectShellWorkspaceContext.Provider value={{ enabled, target, retainedTarget, shells, open, close, toggle, isOpen, remember }}>{children}</ProjectShellWorkspaceContext.Provider>;
}

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react';
import { ArrowDownToLine, ArrowRightLeft, ChevronLeft, Columns2, Copy, GripHorizontal, Maximize2, Minimize2, Minus, Pencil, Pin, Plus, RotateCcw, Rows2, Search, Square, SquareTerminal } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import { BackendError } from '@/backend/errors';
import type { ProjectShell } from '@/backend/types';
import { cn } from '@/components/lib/utils';
import { Button } from '@/components/ui/button';
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from '@/components/ui/context-menu';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/ui/toast';
import { currentHost } from '@/lib/hosts';
import { usePrefs } from '@/lib/prefs';
import { t } from '@/lib/i18n';
import { hasFeature, useApp } from '@/store/app';
import { createProjectShellTerminal, initialShellTerminalState } from './projectShellTerminal';
import { findOpenProjectShell, useProjectShellWorkspace, type ProjectShellTarget } from './ProjectShellWorkspace';
import { ProjectShellSearch } from './ProjectShellSearch';
import { ProjectShellDivider } from './ProjectShellDivider';
import { defaultProjectShellBindings, projectShellActionForKey, projectShellBindingLabel, type ProjectShellBindings } from './projectShellKeys';
import { projectShellLabel } from './projectShellLabel';
import { projectShellLayout, type PaneRect, type SplitDirection } from './projectShellLayout';
import { isOpenProjectShell, isRunningProjectShell, matchesShellPath } from './projectShellStatus';
import { useProjectShellFrame } from './useProjectShellFrame';

const resizeHandles = [
  ['n', 'inset-x-2 top-0 h-1.5 cursor-ns-resize'],
  ['e', 'inset-y-2 right-0 w-1.5 cursor-ew-resize'],
  ['s', 'inset-x-2 bottom-0 h-1.5 cursor-ns-resize'],
  ['w', 'inset-y-2 left-0 w-1.5 cursor-ew-resize'],
  ['ne', 'right-0 top-0 size-2.5 cursor-nesw-resize'],
  ['nw', 'left-0 top-0 size-2.5 cursor-nwse-resize'],
  ['sw', 'bottom-0 left-0 size-2.5 cursor-nesw-resize'],
] as const;

const shellTarget = (shell: ProjectShell): ProjectShellTarget => ({ cwd: shell.cwd, shellId: shell.id });
const searchLabel = () => t('Search terminal output', '搜索终端输出');

export function ProjectShellPanel({ mobile = false, bindings = defaultProjectShellBindings }: { mobile?: boolean; bindings?: ProjectShellBindings }) {
  const backend = useBackend();
  const { target, activeTarget, retainedTargets, targetKey, open, retain, close, pinned, togglePinned, end, shellIdFor, shells, remember, finishRun } = useProjectShellWorkspace();
  const hostName = useApp(s => s.info?.name);
  const tabsEnabled = useApp(s => hasFeature(s.info, 'project-shell-tabs'));
  const groupsEnabled = useApp(s => hasFeature(s.info, 'project-shell-pane-groups'));
  const layoutEnabled = useApp(s => hasFeature(s.info, 'project-shell-pane-layout'));
  const [localGroups, setLocalGroups] = useState<Record<string, string>>({});
  const [localDirections, setLocalDirections] = useState<Record<string, SplitDirection>>({});
  const [splitRatios, setSplitRatios] = useState<Record<string, Record<string, number>>>({});
  const [maximized, setMaximized] = useState(false);
  const [searchTarget, setSearchTarget] = useState<ProjectShellTarget>();
  const [busy, setBusy] = useState(false);
  const mutation = useRef(false);
  const closingShells = useRef(new Set<string>());
  const region = useRef<HTMLElement>(null);
  const { frame, handlers, reset } = useProjectShellFrame(region, !mobile && retainedTargets.length > 0);
  const activeId = activeTarget && shellIdFor(activeTarget);
  const activeShell = shells.find(shell => shell.id === activeId);
  const directory = activeShell?.cwd ?? activeTarget?.cwd ?? '';
  const groupOf = useCallback((shell: ProjectShell) => shell.tabId ?? localGroups[shell.id] ?? shell.id, [localGroups]);
  const activeGroup = activeShell ? groupOf(activeShell) : activeId;
  const projectShells = shells.filter(shell => isOpenProjectShell(shell) &&
    (shell.id === activeId || shell.projectKey === activeShell?.projectKey || matchesShellPath(shell, directory)));
  const groups = new Map<string, ProjectShell[]>();
  for (const shell of projectShells) {
    const key = groupOf(shell);
    const panes = groups.get(key);
    if (panes) panes.push(shell);
    else groups.set(key, [shell]);
  }
  for (const [key, panes] of groups) {
    panes.sort((a, b) => Number(b.id === key) - Number(a.id === key) || Number(Boolean(a.splitDirection)) - Number(Boolean(b.splitDirection)));
  }
  const activePanes = (activeGroup && groups.get(activeGroup)) || [];
  const legacyDirection = (activeGroup && localDirections[activeGroup]) || activePanes.find(pane => pane.splitDirection)?.splitDirection;
  const layout = projectShellLayout(mobile ? [] : activePanes, legacyDirection, activeGroup ? splitRatios[activeGroup] : undefined);
  const floating = !mobile && !maximized;
  const toggleMaximized = () => setMaximized(value => !value);
  const labelFor = (shell: ProjectShell) => projectShellLabel(shell, projectShells.findIndex(item => item.id === shell.id) + 1);
  const canStop = (shell: ProjectShell) => Boolean(shell.runId) || groupsEnabled;
  const preferredTarget = (shell?: ProjectShell) => shell && (retainedTargets.find(item => shellIdFor(item) === shell.id) ?? shellTarget(shell));

  useEffect(() => {
    if (!target || mobile) return;
    for (const shell of shells) {
      if (shell.id !== activeId && groupOf(shell) === activeGroup && isOpenProjectShell(shell)) retain(shellTarget(shell), false);
    }
  }, [activeGroup, activeId, groupOf, mobile, retain, shells, target]);
  useEffect(() => {
    if (searchTarget !== target) setSearchTarget(undefined);
  }, [searchTarget, target]);

  const manage = async (action: () => Promise<void>) => {
    if (mutation.current) return;
    mutation.current = true;
    setBusy(true);
    try {
      await action();
    } catch (error) {
      toast(error instanceof Error ? error.message : String(error), { variant: 'warning' });
    } finally {
      mutation.current = false;
      setBusy(false);
    }
  };
  const rememberLayout = (result: ProjectShell) => {
    if (result.panePaths) {
      for (const pane of activePanes) {
        const path = result.panePaths[pane.id];
        if (path !== undefined && pane.id !== result.id) remember({ ...pane, panePath: path, splitDirection: undefined }, directory);
      }
    }
    remember(result, directory);
  };
  const add = (splitDirection?: SplitDirection) => void manage(async () => {
    const tabId = splitDirection && activeGroup;
    const shell = await backend.startProjectShell({
      cwd: directory, create: true,
      ...(groupsEnabled && tabId && splitDirection ? { tabId, splitDirection, ...(layoutEnabled && activeId ? { splitFrom: activeId } : {}) } : {}),
    });
    if (tabId && splitDirection) {
      if (!shell.tabId) setLocalGroups(current => ({ ...current, [shell.id]: tabId }));
      if (!layoutEnabled) {
        setLocalDirections(current => ({ ...current, [tabId]: splitDirection }));
        for (const pane of activePanes) {
          if (pane.splitDirection) remember({ ...pane, splitDirection: undefined }, directory);
        }
      }
    }
    rememberLayout(shell);
    const next = shellTarget(shell);
    if (region.current && !region.current.hidden) open(next);
    else retain(next, false);
  });
  const canAdd = tabsEnabled && !!directory && !busy;
  const canSplit = canAdd && !mobile && !!activeShell && isRunningProjectShell(activeShell) && (layoutEnabled || activePanes.length <= 1);
  const onShortcut = (event: KeyboardEvent<HTMLElement>) => {
    const element = event.target;
    if (!target || !(element instanceof HTMLElement) || !event.currentTarget.contains(element) ||
      element.closest('input, [contenteditable], [role="menu"]') ||
      (element instanceof HTMLTextAreaElement && !element.classList.contains('xterm-helper-textarea'))) return;
    const action = projectShellActionForKey(event.nativeEvent, bindings);
    if (!action || (action === 'newTab' ? !canAdd : !canSplit)) return;
    event.preventDefault();
    event.stopPropagation();
    if (!event.repeat) add(action === 'newTab' ? undefined : action === 'splitColumns' ? 'columns' : 'rows');
  };
  const reorient = (direction: SplitDirection) => {
    if (!activeGroup || !activeId) return;
    if (layoutEnabled) void manage(async () => rememberLayout(await backend.reorientProjectShell(activeId, direction)));
    else setLocalDirections(current => ({ ...current, [activeGroup]: direction }));
  };
  const focusNext = () => {
    const index = activePanes.findIndex(pane => pane.id === activeId);
    const next = activePanes[(index + 1) % activePanes.length];
    if (next) open(shellTarget(next));
  };
  const stop = (items: ProjectShell[]) => void manage(async () => {
    items = items.filter(canStop);
    const selected = closingShells.current = new Set(items.map(shell => shell.id));
    const remaining = (shell: ProjectShell) => !selected.has(shell.id);
    const survivor = activePanes.find(remaining) ?? projectShells.find(remaining);
    const results = await Promise.allSettled(items.map(async shell => {
      try {
        if (shell.runId) await backend.stopProjectShell(shell.id, shell.runId);
        else if (groupsEnabled) await backend.dismissProjectShell(shell.id);
      } catch (error) {
        if (!(error instanceof BackendError && (error.status === 404 || error.status === 410))) throw error;
      }
      if (shell.runId) finishRun(shell.id, shell.runId, 'stopped');
      else remember({ ...shell, state: 'stopped' }, directory);
      const entry = retainedTargets.find(item => shellIdFor(item) === shell.id);
      if (entry) end(entry, preferredTarget(survivor));
    }));
    closingShells.current.clear();
    const failure = results.find(result => result.status === 'rejected');
    if (failure?.status === 'rejected') throw failure.reason;
  });

  if (!activeTarget || retainedTargets.length === 0) return null;
  const hostname = currentHost?.name || hostName || location.host;
  const closeLabel = mobile ? t('Back', '返回') : t('Minimize terminal', '最小化终端');
  const maximizeLabel = maximized ? t('Restore terminal size', '还原终端大小') : t('Maximize terminal', '最大化终端');
  const MaximizeIcon = maximized ? Minimize2 : Maximize2;
  const moveLabel = t('Drag the terminal or use arrow keys to move it', '拖动或使用方向键移动终端');
  const pinLabel = t('Pin terminal panel', '固定终端面板');
  const pinHint = pinned ? t('Unpin terminal panel', '取消固定终端面板') : pinLabel;
  const newLabel = t('New terminal', '新建终端');
  const shortcutTitle = (label: string, binding: ProjectShellBindings[keyof ProjectShellBindings]) =>
    [label, projectShellBindingLabel(binding)].filter(Boolean).join(' · ');
  return <section ref={region} data-terminal id="project-shell-panel" hidden={!target} tabIndex={-1}
    aria-label={t('Project terminal', '项目终端')} onKeyDownCapture={onShortcut}
    style={floating && frame ? { left: frame.x, top: frame.y, width: frame.width, height: frame.height } : undefined}
    className={cn('absolute z-30 min-h-0 flex-col overflow-hidden bg-background outline-none', target && 'flex', mobile
      ? 'inset-0 pb-[env(safe-area-inset-bottom)]' : maximized ? 'inset-0' : 'rounded-xl border border-border shadow-xl', floating && !frame && 'inset-x-3 bottom-3 h-[min(62%,30rem)]')}>
    <ContextMenu>
      <ContextMenuTrigger asChild><header className={cn('flex shrink-0 items-center gap-2 border-b border-border/70 bg-background-win-alt px-2', mobile ? 'min-h-12 pt-safe' : 'h-9 select-none', floating && 'touch-none cursor-grab active:cursor-grabbing focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring')}
        {...(floating ? { ...handlers('move'), tabIndex: 0, role: 'group', 'aria-label': moveLabel, title: moveLabel } : {})}>
        {mobile && <Button type="button" variant="ghost" size="icon-lg" onClick={close} aria-label={closeLabel}><ChevronLeft /></Button>}
        {floating ? <GripHorizontal className="size-4 shrink-0 text-foreground-subtle" aria-hidden /> : <SquareTerminal className="size-4 shrink-0 text-foreground-subtle" aria-hidden />}
        {!mobile && <Button type="button" variant={pinned ? 'secondary' : 'ghost'} size="icon-sm"
          aria-label={pinLabel} aria-pressed={pinned} title={pinHint} onClick={togglePinned}><Pin /></Button>}
        <div className="flex min-w-0 flex-1 items-baseline gap-2 truncate" title={`${hostname} · ${directory}`}>
          <h2 className="shrink-0 text-ui-sm font-medium">{t('Project terminal', '项目终端')}</h2>
          <span className="truncate text-ui-xs text-foreground-subtle" dir="auto">{hostname} · {directory}</span>
        </div>
        {!mobile && <>
          <Button type="button" variant="ghost" size="icon-sm" onClick={close} aria-label={closeLabel} title={closeLabel}><Minus /></Button>
          <Button type="button" variant="ghost" size="icon-sm" onClick={toggleMaximized} aria-label={maximizeLabel} title={maximizeLabel}><MaximizeIcon /></Button>
        </>}
      </header></ContextMenuTrigger>
      <ContextMenuContent>
        {!mobile && <ContextMenuItem onSelect={toggleMaximized}><MaximizeIcon />{maximizeLabel}</ContextMenuItem>}
        {floating && <ContextMenuItem onSelect={reset}><RotateCcw />{t('Reset panel size and position', '重置面板大小和位置')}</ContextMenuItem>}
        <ContextMenuItem onSelect={close}><Minus />{closeLabel}</ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem variant="destructive" disabled={busy || !projectShells.some(canStop)} onSelect={() => stop(projectShells)}>
          <Square />{t('End all terminals', '结束所有终端')}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
    {target && <div className="flex shrink-0 items-center gap-1 border-b border-border px-1 py-0.5">
      <div className="flex min-w-0 flex-1 gap-0.5 overflow-x-auto" role="group" aria-label={t('Project shells', '项目 Shell')}>
        {[...groups].map(([id, panes]) => {
          const tab = panes[0];
          const selected = id === activeGroup;
          const canEnd = panes.some(canStop);
          return <ProjectShellTab key={id} {...{ tab, panes, selected, busy, canEnd, labelFor }}
            onOpen={shell => open(shellTarget(shell))}
            onTabClick={() => open(selected ? target : shellTarget(tab))}
            onReorient={selected && !mobile && panes.length > 1 ? reorient : undefined}
            onRename={tabsEnabled ? name => manage(async () => remember(await backend.renameProjectShell(tab.id, name), directory)) : undefined}
            onEnd={() => stop(panes)} />;
        })}
        {tabsEnabled && <ContextMenu>
          <ContextMenuTrigger asChild><div className="min-w-0 flex-1" /></ContextMenuTrigger>
          <ContextMenuContent><ContextMenuItem disabled={!canAdd} onSelect={() => add()}><Plus />{newLabel}</ContextMenuItem></ContextMenuContent>
        </ContextMenu>}
      </div>
      {tabsEnabled && <Button type="button" variant="ghost" size="icon-sm" disabled={!canAdd} title={shortcutTitle(newLabel, bindings.newTab)} aria-label={newLabel}
        aria-keyshortcuts={projectShellBindingLabel(bindings.newTab, true)}
        onClick={() => add()}><Plus /></Button>}
      {tabsEnabled && !mobile && ([
        ['columns', Columns2, t('Split side by side', '左右分屏'), bindings.splitColumns],
        ['rows', Rows2, t('Split top and bottom', '上下分屏'), bindings.splitRows],
      ] as const).map(([axis, Icon, label, binding]) => <Button key={axis} type="button" variant="ghost" size="icon-sm"
        title={shortcutTitle(label, binding)} aria-label={label} aria-keyshortcuts={projectShellBindingLabel(binding, true)}
        disabled={!canSplit} onClick={() => add(axis)}><Icon /></Button>)}
      {activePanes.length > 1 && <Button type="button" variant="ghost" size="icon-sm" disabled={busy}
        aria-label={t('Focus next pane', '切换到下一个窗格')} title={t('Focus next pane', '切换到下一个窗格')} onClick={focusNext}><ArrowRightLeft /></Button>}
      <Button type="button" variant={searchTarget === target ? 'secondary' : 'ghost'} size="icon-sm"
        title={searchLabel()} aria-label={searchLabel()} aria-expanded={searchTarget === target} disabled={!activeShell?.runId}
        onClick={() => setSearchTarget(searchTarget === target ? undefined : target)}><Search /></Button>
    </div>}
    <div data-project-shell-layout={layout.direction ?? 'single'} className="relative min-h-0 flex-1">
      {retainedTargets.map(entry => {
        const active = entry === target;
        const shellId = shellIdFor(entry);
        const pane = shellId ? layout.rects.get(shellId) : undefined;
        const visible = Boolean(target && (active || pane));
        const metadata = shells.find(item => item.id === shellId);
        return <Panel key={targetKey(entry)} target={entry} visible={visible} active={active} mobile={mobile}
          pane={pane} split={Boolean(layout.direction)} label={metadata && labelFor(metadata)}
          searching={searchTarget === entry} onSearch={value => { if (value) open(entry); setSearchTarget(value ? entry : undefined); }}
          onActivate={() => open(entry)} onEnd={() => {
            const remaining = (shell: ProjectShell) => shell.id !== shellId && !closingShells.current.has(shell.id);
            const survivor = (metadata && groups.get(groupOf(metadata))?.find(remaining)) ?? projectShells.find(remaining);
            end(entry, preferredTarget(survivor));
          }} />;
      })}
      {target && activeGroup && layout.dividers.map(divider => <ProjectShellDivider key={JSON.stringify([activeGroup, divider.id])} divider={divider}
        onResize={ratio => setSplitRatios(current => current[activeGroup]?.[divider.id] === ratio ? current : {
          ...current, [activeGroup]: { ...current[activeGroup], [divider.id]: ratio },
        })} />)}
    </div>
    {floating && resizeHandles.map(([handle, position]) => <div key={handle} aria-hidden className={cn('absolute z-10 touch-none', position)} {...handlers(handle)} />)}
  </section>;
}

function ProjectShellTab({ tab, panes, selected, busy, canEnd, labelFor, onOpen, onTabClick, onReorient, onRename, onEnd }: {
  tab: ProjectShell; panes: ProjectShell[]; selected: boolean; busy: boolean; canEnd: boolean;
  labelFor(shell: ProjectShell): string; onOpen(shell: ProjectShell): void; onTabClick(): void;
  onReorient?: (direction: SplitDirection) => void; onRename?: (name: string) => Promise<void>; onEnd(): void;
}) {
  const [name, setName] = useState<string>();
  const input = useRef<HTMLInputElement>(null);
  const renameFromMenu = useRef(false);
  const editing = useRef(false);
  const finish = (save: boolean) => {
    if (!editing.current) return;
    editing.current = false;
    setName(undefined);
    const next = input.current?.value.trim();
    if (save && next !== undefined && next !== (tab.name ?? '')) void onRename?.(next);
  };
  const label = labelFor(tab);
  return <ContextMenu>
    <ContextMenuTrigger asChild>
      <div data-project-shell-tab={tab.id} className="shrink-0"
        onPointerDown={event => { if (event.button === 1) event.preventDefault(); }}
        onAuxClick={event => {
          if (event.button === 1 && !editing.current && canEnd && !busy) {
            event.preventDefault();
            onEnd();
          }
        }}>
        {name === undefined ? <Button type="button" variant={selected ? 'secondary' : 'ghost'} size="sm"
          className="max-w-40" aria-pressed={selected} title={label} onClick={onTabClick}>
          <span aria-hidden className={cn('size-1.5 shrink-0 rounded-full', panes.some(pane => pane.state === 'running') ? 'bg-emerald-500' : 'bg-foreground-subtlest')} />
          <span className="truncate">{label}</span>
        </Button> : <Input ref={input} autoFocus size="sm" className="h-7 w-[clamp(5rem,15ch,10rem)] px-2" value={name}
          maxLength={80} placeholder={label} aria-label={t('Terminal name', '终端名称')}
          onChange={event => setName(event.target.value)} onBlur={() => finish(true)}
          onFocus={event => event.currentTarget.select()} onKeyDown={event => {
            if (event.nativeEvent.isComposing || (event.key !== 'Enter' && event.key !== 'Escape')) return;
            event.preventDefault();
            finish(event.key === 'Enter');
          }} />}
      </div>
    </ContextMenuTrigger>
    <ContextMenuContent onCloseAutoFocus={event => {
      if (!renameFromMenu.current) return;
      renameFromMenu.current = false;
      event.preventDefault();
      input.current?.focus();
    }}>
      {panes.length > 1 && <>
        {panes.map(pane => <ContextMenuItem key={pane.id} onSelect={() => onOpen(pane)}>
          <SquareTerminal />{labelFor(pane)}
        </ContextMenuItem>)}
        {onReorient && <>
          <ContextMenuItem disabled={busy} onSelect={() => onReorient('columns')}><Columns2 />{t('Arrange current split side by side', '将当前分屏改为左右排列')}</ContextMenuItem>
          <ContextMenuItem disabled={busy} onSelect={() => onReorient('rows')}><Rows2 />{t('Arrange current split top and bottom', '将当前分屏改为上下排列')}</ContextMenuItem>
        </>}
        <ContextMenuSeparator />
      </>}
      {onRename && <ContextMenuItem disabled={busy} onSelect={() => {
        renameFromMenu.current = true;
        editing.current = true;
        setName(tab.name ?? '');
      }}><Pencil />{t('Rename terminal', '重命名终端')}</ContextMenuItem>}
      <ContextMenuItem variant="destructive" disabled={busy || !canEnd} onSelect={onEnd}><Square />{t('End terminal', '结束终端')}</ContextMenuItem>
    </ContextMenuContent>
  </ContextMenu>;
}

function Panel({ target, mobile, visible, active, pane, split, label, searching, onSearch, onActivate, onEnd }: {
  target: ProjectShellTarget; mobile: boolean; visible: boolean; active: boolean;
  pane?: PaneRect; split: boolean; label?: string; searching: boolean; onSearch(value: boolean): void; onActivate(): void; onEnd(): void;
}) {
  const backend = useBackend();
  const { bind, remember, finishRun, shellListState, shells } = useProjectShellWorkspace();
  const [shell, setShell] = useState<Pick<ProjectShell, 'id' | 'runId'>>();
  const [busy, setBusy] = useState(!target.runId);
  const [error, setError] = useState('');
  const request = useRef<AbortController>(null);
  const locatorId = useRef(target.shellId);
  const knownShells = useRef(shells);
  useLayoutEffect(() => { knownShells.current = shells; }, [shells]);
  const loading = !target.shellId && shellListState === 'loading';
  const region = useRef<HTMLElement>(null);
  const onEndRef = useRef(onEnd);
  useLayoutEffect(() => { onEndRef.current = onEnd; }, [onEnd]);

  const start = useCallback(() => {
    request.current?.abort();
    const abort = new AbortController();
    request.current = abort;
    setBusy(true);
    setError('');
    const shellId = locatorId.current ?? findOpenProjectShell(knownShells.current, target.cwd)?.id;
    const locator = shellId ? { shellId } : { cwd: target.cwd, restart: true };
    void backend.startProjectShell(locator, abort.signal).then(next => {
      if (abort.signal.aborted) return;
      remember(next, target.cwd);
      if (!next.runId || next.state !== 'running') {
        onEndRef.current();
        return;
      }
      locatorId.current = next.id;
      setShell(next);
      bind(target, next.id, next.runId);
      if (next.recovered) {
        toast(t('Host restarted. A new shell opened in the saved directory.', '宿主已重启，已在保存的目录打开新终端。'), { variant: 'info', durationMs: 3000 });
      }
    }).catch(err => {
      if (!abort.signal.aborted && shellId && err instanceof BackendError && (err.status === 404 || err.status === 410)) {
        onEndRef.current();
        return;
      }
      if (!abort.signal.aborted) setError(err instanceof Error ? err.message : String(err));
    }).finally(() => { if (!abort.signal.aborted) setBusy(false); });
  }, [backend, bind, remember, target]);

  useLayoutEffect(() => {
    if (active && visible && !shell?.runId) region.current?.focus({ preventScroll: true });
  }, [active, visible, shell?.runId]);

  useEffect(() => {
    if (target.shellId && target.runId) {
      setShell({ id: target.shellId, runId: target.runId });
      return () => request.current?.abort();
    }
    if (!loading) start();
    return () => request.current?.abort();
  }, [loading, start, target]);

  const end = useCallback((state: 'exited' | 'stopped') => {
    request.current?.abort();
    if (shell?.runId) finishRun(shell.id, shell.runId, state);
    onEndRef.current();
  }, [finishRun, shell]);

  const recover = useCallback(() => {
    if (!visible || target.runId) {
      if (visible) toast(t('This terminal run is no longer available.', '这次终端运行已不可用。'));
      onEndRef.current();
      return;
    }
    setShell(undefined);
    // Only an interrupted shell can start without an explicit restart request.
    start();
  }, [start, target, visible]);

  const stop = () => {
    if (!shell?.runId || busy) return;
    request.current?.abort();
    const abort = new AbortController();
    request.current = abort;
    setBusy(true);
    setError('');
    void backend.stopProjectShell(shell.id, shell.runId).then(() => {
      if (!abort.signal.aborted) end('stopped');
    }).catch((err: unknown) => {
      if (abort.signal.aborted) return;
      if (err instanceof BackendError && err.status === 410) end('stopped');
      else setError(err instanceof Error ? err.message : String(err));
    }).finally(() => { if (!abort.signal.aborted) setBusy(false); });
  };

  return (
    <section ref={region} hidden={!visible} tabIndex={-1} onFocusCapture={() => { if (!active) onActivate(); }}
      aria-label={label ?? 'Shell'} data-shell-pane data-shell-pane-active={active || undefined}
      style={pane ? { left: `${pane.x}%`, top: `${pane.y}%`, width: `${pane.width}%`, height: `${pane.height}%` } : undefined}
      className={cn('absolute min-h-0 min-w-0 flex-col overflow-hidden outline-none', !pane && 'inset-0', visible && 'flex',
        split && 'border', split && (active ? 'border-ring/70' : 'border-border'))}>
      {error && <div role="alert" className="flex items-center justify-between gap-2 px-3 py-1.5 text-ui-sm text-destructive">
        <span className="min-w-0 break-words">{error}</span>
        {!shell && !busy && <Button type="button" variant="outline" size="sm" onClick={() => start()}>{t('Try again', '重试')}</Button>}
      </div>}
      {shell?.runId ? (
        <ProjectShellPane key={shell.runId} shellId={shell.id} runId={shell.runId} mobile={mobile} visible={visible} active={active}
          searching={searching} onSearch={onSearch} stopping={busy} onStop={stop} onExit={end} onUnavailable={recover} />
      ) : busy && <div className="p-3 text-ui-sm text-foreground-subtle" role="status">{t('Opening terminal…', '正在打开终端…')}</div>}
    </section>
  );
}

/** A single terminal surface can also be mounted in a future split or window. */
function ProjectShellPane({ shellId, runId, mobile, visible, active, searching, onSearch, stopping, onStop, onExit, onUnavailable }: {
  shellId: string; runId: string; mobile: boolean; visible: boolean; active: boolean; onExit(state: 'exited' | 'stopped'): void;
  searching: boolean; onSearch(value: boolean): void; stopping: boolean; onStop(): void;
  onUnavailable(): void;
}) {
  const backend = useBackend();
  const element = useRef<HTMLDivElement>(null);
  const terminal = useRef<ReturnType<typeof createProjectShellTerminal>>(null);
  const visibility = useRef(visible);
  const activePane = useRef(active);
  const [ui, setUi] = useState(initialShellTerminalState);
  const [hasSelection, setHasSelection] = useState(false);
  const searchFromMenu = useRef(false);
  useLayoutEffect(() => {
    visibility.current = visible;
    terminal.current?.setVisible(visible);
  }, [visible]);
  useEffect(() => {
    if (!searching) terminal.current?.clearSearch();
  }, [searching]);
  useLayoutEffect(() => {
    activePane.current = active;
    if (active && visible && !element.current?.closest('[data-shell-pane]')?.contains(document.activeElement)) terminal.current?.focus();
  }, [active, visible]);
  useEffect(() => {
    if (!element.current) return;
    const engine = createProjectShellTerminal({
      backend, shellId, runId, element: element.current, mobile, visible: visibility.current,
      copyOnSelect: () => usePrefs.getState().terminalCopyOnSelect,
      onCopyError: () => toast(t('Could not copy the selection.', '无法复制选中内容。'), { variant: 'warning' }),
      onState: setUi,
    });
    terminal.current = engine;
    if (visibility.current && activePane.current) engine.focus();
    return () => {
      terminal.current = null;
      engine.dispose();
    };
  }, [backend, mobile, runId, shellId]);
  useEffect(() => {
    if (ui.status === 'exited') onExit(ui.detail === 'stopped' ? 'stopped' : 'exited');
    else if (ui.status === 'unavailable') onUnavailable();
  }, [onExit, onUnavailable, ui.detail, ui.status]);
  const live = ui.status === 'online';
  const status = ui.status === 'connecting' ? t('Connecting…', '连接中…')
    : ui.status === 'retrying' ? t('Connection lost. Reconnecting…', '连接已断开，正在重连…')
      : ui.status === 'failed' ? t('Could not connect', '无法连接')
        : live && !ui.control ? t('Read-only', '只读') : '';
  const stopLabel = t('End terminal', '结束终端');
  const closeSearch = () => {
    terminal.current?.clearSearch();
    onSearch(false);
    terminal.current?.focus();
  };
  return (
    <>
      {visible && searching && <ProjectShellSearch terminal={terminal} onClose={closeSearch} />}
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <div className="min-h-0 flex-1 overflow-auto overscroll-contain bg-terminal-bg px-2 pt-1"
            onClick={() => terminal.current?.focus()} onContextMenu={() => setHasSelection(Boolean(terminal.current?.selection()))}>
            <div ref={element} className="h-full min-h-0 w-full" />
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent onCloseAutoFocus={event => {
          if (!searchFromMenu.current) return;
          searchFromMenu.current = false;
          event.preventDefault();
          element.current?.closest('[data-shell-pane]')?.querySelector<HTMLInputElement>('input[type="search"]')?.focus();
        }}>
          <ContextMenuItem disabled={!hasSelection} onSelect={() => terminal.current?.copySelection()}><Copy />{t('Copy selection', '复制选中内容')}</ContextMenuItem>
          <ContextMenuItem onSelect={() => {
            searchFromMenu.current = true;
            onSearch(true);
          }}><Search />{searchLabel()}</ContextMenuItem>
          <ContextMenuItem onSelect={() => terminal.current?.scrollToBottom()}><ArrowDownToLine />{t('Jump to latest output', '回到最新输出')}</ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem variant="destructive" disabled={stopping} onSelect={onStop}><Square />{stopLabel}</ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
      {(status || ui.truncated || mobile) && <div className="flex min-h-8 shrink-0 flex-wrap items-center gap-2 border-t border-border/70 px-2 py-1 text-ui-xs">
        <span className="min-w-0 break-words text-foreground-subtle" role="status">
          {status}{!live && status && ui.detail ? ` · ${ui.detail}` : ''}
          {status && ui.truncated ? ' · ' : ''}{ui.truncated && t('Output history was truncated.', '历史输出已截断。')}
        </span>
        {live && !ui.control && <Button type="button" variant="outline" size="sm" onClick={() => { terminal.current?.take(); terminal.current?.focus(); }}>{t('Take control', '接管输入')}</Button>}
        {ui.status === 'failed' && <Button type="button" variant="outline" size="sm" onClick={() => terminal.current?.retry()}>{t('Reconnect', '重新连接')}</Button>}
        {mobile && live && ui.control && <div className="flex gap-1" aria-label={t('Terminal keys', '终端按键')}>
          {([['Esc', '\x1b'], ['Tab', '\t'], ['Ctrl+C', '\x03']] as const).map(([label, key]) => (
            <Button key={label} type="button" variant="ghost" size="sm" onPointerDown={event => event.preventDefault()} onClick={() => terminal.current?.send(key)}>{label}</Button>
          ))}
        </div>}
      </div>}
    </>
  );
}

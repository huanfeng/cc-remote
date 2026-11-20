import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { ArrowDownToLine, ChevronDown, ChevronLeft, Columns2, Copy, GripHorizontal, MoreHorizontal, Pencil, Plus, RotateCcw, Rows2, Search, Square, SquareTerminal } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import { BackendError } from '@/backend/errors';
import type { ProjectShell } from '@/backend/types';
import { cn } from '@/components/lib/utils';
import { Button } from '@/components/ui/button';
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from '@/components/ui/context-menu';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/ui/toast';
import { copyText } from '@/domain/clipboard';
import { currentHost } from '@/lib/hosts';
import { t } from '@/lib/i18n';
import { usePrefs } from '@/lib/prefs';
import { useResolvedDark } from '@/lib/theme';
import { hasFeature, useApp } from '@/store/app';
import { createProjectShellTerminal, initialShellTerminalState } from './projectShellTerminal';
import { matchesShellPath, useProjectShellWorkspace, type ProjectShellTarget } from './ProjectShellWorkspace';
import { ProjectShellSearch } from './ProjectShellSearch';
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

type SplitDirection = 'columns' | 'rows';
type ShellSplit = { direction: SplitDirection; first: ProjectShellTarget; secondId: string };
const shellLabel = (shell: ProjectShell) => shell.name || (shell.default ? t('Default', '默认') : shell.id.slice(0, 6));

export function ProjectShellPanel({ mobile = false }: { mobile?: boolean }) {
  const { target, activeTarget, retainedTargets, targetKey, open, close, end, shellIdFor, shells } = useProjectShellWorkspace();
  const hostName = useApp(s => s.info?.name);
  const [split, setSplit] = useState<ShellSplit>();
  const region = useRef<HTMLElement>(null);
  const { frame, handlers, reset } = useProjectShellFrame(region, !mobile && retainedTargets.length > 0);
  const first = split && retainedTargets.includes(split.first) ? split.first : undefined;
  const second = split && retainedTargets.find(entry => entry !== first && shellIdFor(entry) === split.secondId);
  const direction = !mobile && first && second && (activeTarget === first || activeTarget === second) ? split?.direction : undefined;

  if (!activeTarget || retainedTargets.length === 0) return null;
  const hostname = currentHost?.name || hostName || location.host;
  const directory = shells.find(item => item.id === shellIdFor(activeTarget))?.cwd || activeTarget.cwd;
  const closeLabel = mobile ? t('Back', '返回') : t('Hide terminal', '收起终端');
  const moveLabel = t('Drag the terminal or use arrow keys to move it', '拖动或使用方向键移动终端');
  return <section ref={region} data-terminal id="project-shell-panel" hidden={!target} tabIndex={-1}
    aria-label={t('Project terminal', '项目终端')}
    style={frame ? { left: frame.x, top: frame.y, width: frame.width, height: frame.height } : undefined}
    className={cn('absolute z-30 min-h-0 flex-col overflow-hidden bg-background outline-none', target && 'flex', mobile
      ? 'inset-0 pb-[env(safe-area-inset-bottom)]' : 'rounded-xl border border-border shadow-xl', !mobile && !frame && 'inset-x-3 bottom-3 h-[min(62%,30rem)]')}>
    <ContextMenu>
    <ContextMenuTrigger asChild><header className={cn('flex shrink-0 items-center gap-2 border-b border-border/70 bg-background-win-alt px-2', mobile ? 'min-h-12 pt-safe' : 'h-9 touch-none cursor-grab select-none active:cursor-grabbing focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring')}
      {...(!mobile ? { ...handlers('move'), tabIndex: 0, role: 'group', 'aria-label': moveLabel, title: moveLabel } : {})}>
      {mobile && <Button type="button" variant="ghost" size="icon-lg" onClick={close} aria-label={closeLabel}><ChevronLeft /></Button>}
      {mobile ? <SquareTerminal className="size-4 shrink-0 text-foreground-subtle" aria-hidden /> : <GripHorizontal className="size-4 shrink-0 text-foreground-subtle" aria-hidden />}
      <div className="flex min-w-0 flex-1 items-baseline gap-2 truncate" title={`${hostname} · ${directory}`}>
        <h2 className="shrink-0 text-ui-sm font-medium">{t('Project terminal', '项目终端')}</h2>
        <span className="truncate text-ui-xs text-foreground-subtle" dir="auto">{hostname} · {directory}</span>
      </div>
      {!mobile && <Button type="button" variant="ghost" size="icon-sm" onClick={reset} title={t('Reset panel size and position', '重置面板大小和位置')}
        aria-label={t('Reset panel size and position', '重置面板大小和位置')}><RotateCcw /></Button>}
      {!mobile && <Button type="button" variant="ghost" size="icon-sm" onClick={close} aria-label={closeLabel} title={closeLabel}><ChevronDown /></Button>}
    </header></ContextMenuTrigger>
      <ContextMenuContent>
        {!mobile && <ContextMenuItem onSelect={reset}><RotateCcw />{t('Reset panel size and position', '重置面板大小和位置')}</ContextMenuItem>}
        <ContextMenuItem onSelect={close}><ChevronDown />{closeLabel}</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
    {target && <ProjectShellTabs key={targetKey(retainedTargets[0])} target={activeTarget} splitDirection={direction} onUnsplit={() => setSplit(undefined)}
      onSplit={mobile ? undefined : (next, newDirection) => {
        const other = activeTarget === second ? first : second;
        if (direction && other && shellIdFor(other) === next.id) {
          setSplit(current => current && { ...current, direction: newDirection });
        } else {
          setSplit({ direction: newDirection, first: activeTarget, secondId: next.id });
          open({ cwd: next.cwd, shellId: next.id });
        }
      }} />}
    <div data-project-shell-layout={direction ?? 'single'} className={cn('grid min-h-0 flex-1', direction === 'columns' ? 'grid-cols-2 grid-rows-1' : direction === 'rows' ? 'grid-cols-1 grid-rows-2' : 'grid-cols-1 grid-rows-1')}>
      {retainedTargets.map(entry => {
        const active = entry === target;
        const visible = Boolean(target && (active || (direction && (entry === first || entry === second))));
        const sibling = entry === first ? second : first;
        const metadata = shells.find(item => item.id === shellIdFor(entry));
        return <Panel key={targetKey(entry)} target={entry} visible={visible} active={active} mobile={mobile}
          splitPosition={direction ? (entry === first ? 1 : 2) : undefined} label={metadata && shellLabel(metadata)}
          onActivate={() => open(entry)} onEnd={() => {
            if (entry === first || entry === second) setSplit(undefined);
            end(entry, direction ? sibling : undefined);
          }} />;
      })}
    </div>
    {!mobile && resizeHandles.map(([handle, position]) => <div key={handle} aria-hidden className={cn('absolute z-10 touch-none', position)} {...handlers(handle)} />)}
  </section>;
}

function ProjectShellTabs({ target, splitDirection, onSplit, onUnsplit }: {
  target: ProjectShellTarget;
  splitDirection?: SplitDirection;
  onSplit?: (shell: ProjectShell, direction: SplitDirection) => void;
  onUnsplit(): void;
}) {
  const backend = useBackend();
  const enabled = useApp(s => hasFeature(s.info, 'project-shell-tabs'));
  const { shells, open, remember, shellIdFor, finishRun } = useProjectShellWorkspace();
  const [busy, setBusy] = useState(false);
  const [rename, setRename] = useState<{ id: string; name: string }>();
  const renameInput = useRef<HTMLInputElement>(null);
  const renameFromMenu = useRef(false);
  const abort = useRef<AbortController>(null);
  useEffect(() => () => abort.current?.abort(), []);
  if (!enabled) return null;

  const selected = shellIdFor(target);
  const list = shells.filter(shell => shell.id === selected || matchesShellPath(shell, target.cwd));
  const ordered = [...list].sort((a, b) => Number(!a.default) - Number(!b.default));
  const manage = async (action: (signal: AbortSignal) => Promise<ProjectShell>, onSuccess?: (shell: ProjectShell) => void) => {
    const request = new AbortController();
    abort.current = request;
    setBusy(true);
    try {
      const shell = await action(request.signal);
      if (request.signal.aborted) return;
      remember(shell, target.cwd);
      setRename(undefined);
      onSuccess?.(shell);
    } catch (error) {
      if (!request.signal.aborted) toast(String(error instanceof Error ? error.message : error), { variant: 'warning' });
    } finally {
      if (!request.signal.aborted) setBusy(false);
    }
  };
  const openShell = (shell: ProjectShell) => open({ cwd: shell.cwd, shellId: shell.id });
  const addShell = () => void manage(signal => backend.startProjectShell({ cwd: target.cwd, create: true }, signal), openShell);
  const renameShell = (shell: ProjectShell) => setRename({ id: shell.id, name: shell.name ?? '' });
  const stopShell = async (shell: ProjectShell) => {
    if (!shell.runId || busy) return;
    setBusy(true);
    try {
      await backend.stopProjectShell(shell.id, shell.runId);
      finishRun(shell.id, shell.runId, 'stopped');
    } catch (error) {
      if (error instanceof BackendError && error.status === 410) finishRun(shell.id, shell.runId, 'stopped');
      else toast(error instanceof Error ? error.message : String(error), { variant: 'warning' });
    } finally {
      setBusy(false);
    }
  };

  return <div className="flex shrink-0 items-center gap-1 border-b border-border px-1 py-0.5">
    {rename ? <form className="flex min-w-0 flex-1 items-center gap-1" onSubmit={event => {
      event.preventDefault();
      if (!busy) void manage(signal => backend.renameProjectShell(rename.id, rename.name.trim(), signal));
    }}>
      <Input ref={renameInput} autoFocus size="sm" disabled={busy} maxLength={80} value={rename.name} onChange={event => setRename({ ...rename, name: event.target.value })}
        aria-label={t('Terminal name', '终端名称')} onKeyDown={event => { if (event.key === 'Escape') setRename(undefined); }} />
      <Button type="submit" size="sm" disabled={busy}>{t('Save', '保存')}</Button>
      <Button type="button" variant="ghost" size="sm" onClick={() => setRename(undefined)}>{t('Cancel', '取消')}</Button>
    </form> : <>
      <div className="flex min-w-0 flex-1 gap-0.5 overflow-x-auto" role="group" aria-label={t('Project shells', '项目 Shell')}>
        {ordered.map(shell => <ContextMenu key={shell.id}>
          <ContextMenuTrigger asChild><Button type="button" variant={selected === shell.id ? 'secondary' : 'ghost'} size="sm"
          className="max-w-40 shrink-0" aria-pressed={selected === shell.id} title={shellLabel(shell)} disabled={busy}
          onClick={() => openShell(shell)}>
          <span aria-hidden className={cn('size-1.5 shrink-0 rounded-full', shell.state === 'running' ? 'bg-emerald-500' : 'bg-foreground-subtlest')} />
          <span className="truncate">{shellLabel(shell)}</span>
          </Button></ContextMenuTrigger>
          <ContextMenuContent onCloseAutoFocus={event => {
            if (!renameFromMenu.current) return;
            renameFromMenu.current = false;
            event.preventDefault();
            renameInput.current?.focus();
          }}>
            <ContextMenuItem onSelect={() => openShell(shell)}><SquareTerminal />{t('Open terminal', '打开终端')}</ContextMenuItem>
            <ContextMenuItem disabled={busy} onSelect={() => {
              renameFromMenu.current = true;
              renameShell(shell);
            }}><Pencil />{t('Rename terminal', '重命名终端')}</ContextMenuItem>
            {onSplit && selected && shell.id !== selected && <>
              <ContextMenuItem disabled={busy} onSelect={() => onSplit(shell, 'columns')}><Columns2 />{t('Split side by side', '左右分屏')}</ContextMenuItem>
              <ContextMenuItem disabled={busy} onSelect={() => onSplit(shell, 'rows')}><Rows2 />{t('Split top and bottom', '上下分屏')}</ContextMenuItem>
            </>}
            {shell.state === 'running' && shell.runId && <>
              <ContextMenuSeparator />
              <ContextMenuItem variant="destructive" disabled={busy} onSelect={() => void stopShell(shell)}><Square />{t('End terminal', '结束终端')}</ContextMenuItem>
            </>}
          </ContextMenuContent>
        </ContextMenu>)}
        <ContextMenu>
          <ContextMenuTrigger asChild><div className="min-w-0 flex-1" /></ContextMenuTrigger>
          <ContextMenuContent><ContextMenuItem disabled={busy} onSelect={addShell}><Plus />{t('New terminal', '新建终端')}</ContextMenuItem></ContextMenuContent>
        </ContextMenu>
      </div>
      <Button type="button" variant="ghost" size="icon-sm" disabled={busy || !selected} title={t('Rename terminal', '重命名终端')} aria-label={t('Rename terminal', '重命名终端')}
        onClick={() => { const shell = list.find(item => item.id === selected); if (shell) renameShell(shell); }}><Pencil /></Button>
      <Button type="button" variant="ghost" size="icon-sm" disabled={busy} title={t('New terminal', '新建终端')} aria-label={t('New terminal', '新建终端')}
        onClick={addShell}><Plus /></Button>
      {onSplit && <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button type="button" variant={splitDirection ? 'secondary' : 'ghost'} size="icon-sm" disabled={busy || !selected}
            aria-label={t('Split terminal', '终端分屏')} title={t('Split terminal', '终端分屏')}><Columns2 /></Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {([['columns', Columns2, t('Split side by side', '左右分屏')], ['rows', Rows2, t('Split top and bottom', '上下分屏')]] as const).map(([direction, Icon, title]) => (
            <DropdownMenuSub key={direction}>
              <DropdownMenuSubTrigger><Icon className="mr-2 size-4" />{title}</DropdownMenuSubTrigger>
              <DropdownMenuPortal>
                <DropdownMenuSubContent>
                  <DropdownMenuItem onSelect={() => void manage(signal => backend.startProjectShell({ cwd: target.cwd, create: true }, signal), shell => onSplit(shell, direction))}>
                    <Plus />{t('New terminal', '新建终端')}
                  </DropdownMenuItem>
                  {ordered.filter(shell => shell.id !== selected).map(shell => <DropdownMenuItem key={shell.id} onSelect={() => onSplit(shell, direction)}>{shellLabel(shell)}</DropdownMenuItem>)}
                </DropdownMenuSubContent>
              </DropdownMenuPortal>
            </DropdownMenuSub>
          ))}
          {splitDirection && <DropdownMenuItem onSelect={onUnsplit}>{t('Return to one pane', '返回单终端')}</DropdownMenuItem>}
        </DropdownMenuContent>
      </DropdownMenu>}
    </>}
  </div>;
}

function Panel({ target, mobile, visible, active, splitPosition, label, onActivate, onEnd }: {
  target: ProjectShellTarget; mobile: boolean; visible: boolean; active: boolean;
  splitPosition?: number; label?: string; onActivate(): void; onEnd(): void;
}) {
  const backend = useBackend();
  const { bind, remember, finishRun } = useProjectShellWorkspace();
  const [shell, setShell] = useState<Pick<ProjectShell, 'id' | 'runId'>>();
  const [busy, setBusy] = useState(!target.runId);
  const [error, setError] = useState('');
  const request = useRef<AbortController>(null);
  const region = useRef<HTMLElement>(null);
  const onEndRef = useRef(onEnd);
  useLayoutEffect(() => { onEndRef.current = onEnd; }, [onEnd]);

  const start = useCallback((restart = true) => {
    request.current?.abort();
    const abort = new AbortController();
    request.current = abort;
    setBusy(true);
    setError('');
    const locator = target.shellId ? { shellId: target.shellId } : { cwd: target.cwd };
    void backend.startProjectShell({ ...locator, restart }, abort.signal).then(next => {
      if (abort.signal.aborted) return;
      remember(next, target.cwd);
      if (!next.runId || next.state !== 'running') {
        onEndRef.current();
        return;
      }
      setShell(next);
      bind(target, next.id, next.runId);
      if (next.recovered) {
        toast(t('Host restarted. A new shell opened in the saved directory.', '宿主已重启，已在保存的目录打开新终端。'), { variant: 'info', durationMs: 3000 });
      }
    }).catch(err => {
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
    start();
    return () => request.current?.abort();
  }, [start, target]);

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
    start(false);
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
      style={splitPosition ? { order: splitPosition } : undefined}
      className={cn('min-h-0 min-w-0 flex-col overflow-hidden outline-none', visible && 'flex',
        splitPosition && 'border border-border', splitPosition && active && 'border-ring/70')}>
      {error && <div role="alert" className="flex items-center justify-between gap-2 px-3 py-1.5 text-ui-sm text-destructive">
        <span className="min-w-0 break-words">{error}</span>
        {!shell && !busy && <Button type="button" variant="outline" size="sm" onClick={() => start()}>{t('Try again', '重试')}</Button>}
      </div>}
      {shell?.runId ? (
        <ProjectShellPane key={shell.runId} shellId={shell.id} runId={shell.runId} mobile={mobile} visible={visible} active={active}
          label={splitPosition ? label ?? 'Shell' : undefined} stopping={busy} onStop={stop} onExit={end} onUnavailable={recover} />
      ) : busy && <div className="p-3 text-ui-sm text-foreground-subtle" role="status">{t('Opening terminal…', '正在打开终端…')}</div>}
    </section>
  );
}

/** A single terminal surface can also be mounted in a future split or window. */
function ProjectShellPane({ shellId, runId, mobile, visible, active, label, stopping, onStop, onExit, onUnavailable }: {
  shellId: string; runId: string; mobile: boolean; visible: boolean; active: boolean; onExit(state: 'exited' | 'stopped'): void;
  label?: string; stopping: boolean; onStop(): void;
  onUnavailable(): void;
}) {
  const backend = useBackend();
  const element = useRef<HTMLDivElement>(null);
  const terminal = useRef<ReturnType<typeof createProjectShellTerminal>>(null);
  const visibility = useRef(visible);
  const activePane = useRef(active);
  const [ui, setUi] = useState(initialShellTerminalState);
  const [searching, setSearching] = useState(false);
  const [hasSelection, setHasSelection] = useState(false);
  const searchFromMenu = useRef(false);
  const dark = useResolvedDark();
  const palette = usePrefs(s => s.palette);
  useLayoutEffect(() => {
    visibility.current = visible;
    terminal.current?.setVisible(visible);
    if (!visible && terminal.current) {
      terminal.current.clearSearch();
      setSearching(false);
    }
  }, [visible]);
  useLayoutEffect(() => {
    activePane.current = active;
    if (active && visible && !element.current?.closest('[data-shell-pane]')?.contains(document.activeElement)) terminal.current?.focus();
  }, [active, visible]);
  useEffect(() => {
    if (!element.current) return;
    const engine = createProjectShellTerminal({ backend, shellId, runId, element: element.current, mobile, visible: visibility.current, onState: setUi });
    terminal.current = engine;
    if (visibility.current && activePane.current) engine.focus();
    return () => {
      terminal.current = null;
      engine.dispose();
    };
  }, [backend, mobile, runId, shellId]);
  useEffect(() => { terminal.current?.applyTheme(); }, [dark, palette]);
  useEffect(() => {
    if (ui.status === 'exited') onExit(ui.detail === 'stopped' ? 'stopped' : 'exited');
    else if (ui.status === 'unavailable') onUnavailable();
  }, [onExit, onUnavailable, ui.detail, ui.status]);
  const live = ui.status === 'online';
  const status = ui.status === 'connecting' ? t('Connecting…', '连接中…')
    : ui.status === 'retrying' ? t('Connection lost. Reconnecting…', '连接已断开，正在重连…')
      : ui.status === 'failed' ? t('Could not connect', '无法连接')
        : live && !ui.control ? t('Read-only', '只读') : '';
  const searchLabel = t('Search terminal output', '搜索终端输出');
  const stopLabel = t('End terminal', '结束终端');
  const closeSearch = () => {
    terminal.current?.clearSearch();
    setSearching(false);
    terminal.current?.focus();
  };
  return (
    <>
      <div className="flex h-7 shrink-0 items-center justify-end gap-1 border-b border-border/70 bg-background-win-alt px-2">
        {label && <span className="mr-auto truncate text-ui-xs">{label}</span>}
        <Button type="button" variant={searching ? 'secondary' : 'ghost'} size="icon-sm" aria-label={searchLabel}
          title={searchLabel} aria-expanded={searching} onClick={() => searching ? closeSearch() : setSearching(true)}><Search /></Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button type="button" variant="ghost" size="icon-sm" aria-label={t('Terminal actions', '终端操作')}><MoreHorizontal /></Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={onStop} disabled={stopping}><Square />{stopLabel}</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
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
          <ContextMenuItem disabled={!hasSelection} onSelect={() => {
            const selected = terminal.current?.selection();
            if (selected) void copyText(selected).catch(() => toast(t('Could not copy the selection.', '无法复制选中内容。'), { variant: 'warning' }));
          }}><Copy />{t('Copy selection', '复制选中内容')}</ContextMenuItem>
          <ContextMenuItem onSelect={() => {
            searchFromMenu.current = true;
            setSearching(true);
          }}><Search />{searchLabel}</ContextMenuItem>
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

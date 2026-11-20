import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { ChevronDown, ChevronLeft, GripHorizontal, MoreHorizontal, MoveDiagonal2, Pencil, Plus, Search, Square, SquareTerminal } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import { BackendError } from '@/backend/errors';
import type { ProjectShell } from '@/backend/types';
import { cn } from '@/components/lib/utils';
import { Button } from '@/components/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/ui/toast';
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

export function ProjectShellPanel({ mobile = false }: { mobile?: boolean }) {
  const { target, retainedTargets, targetKey } = useProjectShellWorkspace();
  const region = useRef<HTMLElement>(null);
  const frame = useProjectShellFrame(region, !mobile && retainedTargets.length > 0);
  return retainedTargets.map((entry, index) => (
    <Panel key={targetKey(entry)} target={entry} visible={target === entry} mobile={mobile}
      frame={frame} observe={index === 0 ? node => { region.current = node; } : undefined}
      tabs={target === entry ? <ProjectShellTabs target={entry} /> : undefined} />
  ));
}

function ProjectShellTabs({ target }: { target: ProjectShellTarget }) {
  const backend = useBackend();
  const enabled = useApp(s => hasFeature(s.info, 'project-shell-tabs'));
  const { shells, open, remember, shellIdFor } = useProjectShellWorkspace();
  const [busy, setBusy] = useState(false);
  const [rename, setRename] = useState<{ id: string; name: string }>();
  const abort = useRef<AbortController>(null);
  useEffect(() => () => abort.current?.abort(), []);
  if (!enabled) return null;

  const selected = shellIdFor(target);
  const list = shells.filter(shell => shell.id === selected || matchesShellPath(shell, target.cwd));
  const ordered = [...list].sort((a, b) => Number(!a.default) - Number(!b.default));
  const label = (shell: ProjectShell) => shell.name || (shell.default ? t('Default', '默认') : shell.id.slice(0, 6));
  const manage = async (action: (signal: AbortSignal) => Promise<ProjectShell>, activate = false) => {
    const request = new AbortController();
    abort.current = request;
    setBusy(true);
    try {
      const shell = await action(request.signal);
      if (request.signal.aborted) return;
      remember(shell, target.cwd);
      setRename(undefined);
      if (activate) open({ cwd: shell.cwd, shellId: shell.id });
    } catch (error) {
      if (!request.signal.aborted) toast(String(error instanceof Error ? error.message : error), { variant: 'warning' });
    } finally {
      if (!request.signal.aborted) setBusy(false);
    }
  };

  return <div className="flex shrink-0 items-center gap-1 border-b border-border px-1 py-0.5">
    {rename ? <form className="flex min-w-0 flex-1 items-center gap-1" onSubmit={event => {
      event.preventDefault();
      if (!busy) void manage(signal => backend.renameProjectShell(rename.id, rename.name.trim(), signal));
    }}>
      <Input autoFocus size="sm" disabled={busy} maxLength={80} value={rename.name} onChange={event => setRename({ ...rename, name: event.target.value })}
        aria-label={t('Terminal name', '终端名称')} onKeyDown={event => { if (event.key === 'Escape') setRename(undefined); }} />
      <Button type="submit" size="sm" disabled={busy}>{t('Save', '保存')}</Button>
      <Button type="button" variant="ghost" size="sm" onClick={() => setRename(undefined)}>{t('Cancel', '取消')}</Button>
    </form> : <>
      <div className="flex min-w-0 flex-1 gap-0.5 overflow-x-auto" role="group" aria-label={t('Project shells', '项目 Shell')}>
        {ordered.map(shell => <Button key={shell.id} type="button" variant={selected === shell.id ? 'secondary' : 'ghost'} size="sm"
          className="max-w-40 shrink-0" aria-pressed={selected === shell.id} title={label(shell)} disabled={busy}
          onClick={() => open({ cwd: shell.cwd, shellId: shell.id })}>
          <span aria-hidden className={cn('size-1.5 shrink-0 rounded-full', shell.state === 'running' ? 'bg-emerald-500' : 'bg-foreground-subtlest')} />
          <span className="truncate">{label(shell)}</span>
        </Button>)}
      </div>
      <Button type="button" variant="ghost" size="icon-sm" disabled={busy || !selected} title={t('Rename terminal', '重命名终端')} aria-label={t('Rename terminal', '重命名终端')}
        onClick={() => { if (selected) setRename({ id: selected, name: list.find(shell => shell.id === selected)?.name ?? '' }); }}><Pencil /></Button>
      <Button type="button" variant="ghost" size="icon-sm" disabled={busy} title={t('New terminal', '新建终端')} aria-label={t('New terminal', '新建终端')}
        onClick={() => void manage(signal => backend.startProjectShell({ cwd: target.cwd, create: true }, signal), true)}><Plus /></Button>
    </>}
  </div>;
}

function Panel({ target, mobile, visible, frame: frameControls, observe, tabs }: {
  target: ProjectShellTarget; mobile: boolean; visible: boolean; frame: ReturnType<typeof useProjectShellFrame>;
  observe?: (node: HTMLElement | null) => void; tabs?: ReactNode;
}) {
  const backend = useBackend();
  const { bind, close, end: closeEndedPanel, remember, finishRun } = useProjectShellWorkspace();
  const hostName = useApp(s => s.info?.name);
  const [shell, setShell] = useState<Pick<ProjectShell, 'id' | 'runId' | 'cwd' | 'state'>>();
  const [busy, setBusy] = useState(!target.runId);
  const [error, setError] = useState('');
  const request = useRef<AbortController>(null);
  const region = useRef<HTMLElement>(null);
  const { frame, handlers, reset } = frameControls;

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
        closeEndedPanel(target);
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
  }, [backend, bind, closeEndedPanel, remember, target]);

  useLayoutEffect(() => {
    if (visible && !shell?.runId) region.current?.focus({ preventScroll: true });
  }, [visible, shell?.runId]);

  useEffect(() => {
    if (target.shellId && target.runId) {
      setShell({ id: target.shellId, runId: target.runId, cwd: target.cwd, state: 'running' });
      return () => request.current?.abort();
    }
    start();
    return () => request.current?.abort();
  }, [start, target]);

  const end = useCallback((state: 'exited' | 'stopped') => {
    request.current?.abort();
    if (shell?.runId) finishRun(shell.id, shell.runId, state);
    closeEndedPanel(target);
  }, [closeEndedPanel, finishRun, shell, target]);

  const recover = useCallback(() => {
    if (!visible || target.runId) {
      if (visible) toast(t('This terminal run is no longer available.', '这次终端运行已不可用。'));
      closeEndedPanel(target);
      return;
    }
    setShell(undefined);
    // Only an interrupted shell can start without an explicit restart request.
    start(false);
  }, [closeEndedPanel, start, target, visible]);

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

  const running = shell?.state === 'running';
  const directory = shell?.cwd || target.cwd;
  const hostname = currentHost?.name || hostName || location.host;
  const closeLabel = mobile ? t('Back', '返回') : t('Hide terminal', '收起终端');
  const moveLabel = t('Drag the terminal or use arrow keys to move it', '拖动或使用方向键移动终端');
  const resizeLabel = t('Drag the corner or use arrow keys to resize the terminal', '拖动边角或使用方向键调整终端大小');
  return (
    <section
      ref={node => { region.current = node; observe?.(node); }}
      data-terminal
      id={visible ? 'project-shell-panel' : undefined}
      hidden={!visible}
      tabIndex={-1}
      aria-label={t('Project terminal', '项目终端')}
      style={frame ? { left: frame.x, top: frame.y, width: frame.width, height: frame.height } : undefined}
      className={cn('absolute z-30 min-h-0 flex-col overflow-hidden bg-background outline-none', visible && 'flex', mobile
        ? 'inset-0 pb-[env(safe-area-inset-bottom)]'
        : 'rounded-xl border border-border shadow-xl', !mobile && !frame && 'inset-x-3 bottom-3 h-[min(62%,30rem)]')}
    >
      <header className={cn('flex shrink-0 items-center gap-2 border-b border-border/70 bg-background-win-alt px-2', mobile ? 'min-h-12 pt-safe' : 'h-9 touch-none cursor-grab select-none active:cursor-grabbing focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring')}
        {...(!mobile ? { ...handlers('move'), tabIndex: 0, role: 'group', 'aria-label': moveLabel, title: moveLabel } : {})}>
        {mobile && <Button type="button" variant="ghost" size="icon-lg" onClick={close} aria-label={closeLabel}><ChevronLeft /></Button>}
        {mobile ? <SquareTerminal className="size-4 shrink-0 text-foreground-subtle" aria-hidden /> : <GripHorizontal className="size-4 shrink-0 text-foreground-subtle" aria-hidden />}
        <div className="flex min-w-0 flex-1 items-baseline gap-2 truncate" title={`${hostname} · ${directory}`}>
          <h2 className="shrink-0 text-ui-sm font-medium">{t('Project terminal', '项目终端')}</h2>
          <span className="truncate text-ui-xs text-foreground-subtle" dir="auto">{hostname} · {directory}</span>
        </div>
        {(running || !mobile) && <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button type="button" variant="ghost" size="icon-sm" aria-label={t('Terminal actions', '终端操作')}><MoreHorizontal /></Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {!mobile && <DropdownMenuItem onSelect={reset}>{t('Reset panel size and position', '重置面板大小和位置')}</DropdownMenuItem>}
            {running && <DropdownMenuItem onSelect={stop} disabled={busy}><Square />{t('End terminal', '结束终端')}</DropdownMenuItem>}
          </DropdownMenuContent>
        </DropdownMenu>}
        {!mobile && <Button type="button" variant="ghost" size="icon-sm" onClick={close} aria-label={closeLabel} title={closeLabel}><ChevronDown /></Button>}
      </header>

      {tabs}
      {error && <div role="alert" className="flex items-center justify-between gap-2 px-3 py-1.5 text-ui-sm text-destructive">
        <span className="min-w-0 break-words">{error}</span>
        {!shell && !busy && <Button type="button" variant="outline" size="sm" onClick={() => start()}>{t('Try again', '重试')}</Button>}
      </div>}
      {shell?.runId ? (
        <ProjectShellPane key={`${shell.id}/${shell.runId}`} shellId={shell.id} runId={shell.runId} mobile={mobile} visible={visible} onExit={end} onUnavailable={recover} />
      ) : busy && <div className="p-3 text-ui-sm text-foreground-subtle" role="status">{t('Opening terminal…', '正在打开终端…')}</div>}
      {!mobile && <>
        {resizeHandles.map(([handle, position]) => <div key={handle} aria-hidden className={cn('absolute z-10 touch-none', position)} {...handlers(handle)} />)}
        <button type="button" aria-label={resizeLabel} title={resizeLabel} {...handlers('se')}
          className="absolute bottom-0 right-0 z-10 grid size-4 touch-none cursor-nwse-resize place-items-center rounded-tl bg-background-win-alt text-foreground-subtle focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring">
          <MoveDiagonal2 className="size-3" aria-hidden />
        </button>
      </>}
    </section>
  );
}

/** A single terminal surface can also be mounted in a future split or window. */
function ProjectShellPane({ shellId, runId, mobile, visible, onExit, onUnavailable }: {
  shellId: string; runId: string; mobile: boolean; visible: boolean; onExit(state: 'exited' | 'stopped'): void;
  onUnavailable(): void;
}) {
  const backend = useBackend();
  const element = useRef<HTMLDivElement>(null);
  const terminal = useRef<ReturnType<typeof createProjectShellTerminal>>(null);
  const visibility = useRef(visible);
  const [ui, setUi] = useState(initialShellTerminalState);
  const [searching, setSearching] = useState(false);
  const dark = useResolvedDark();
  const palette = usePrefs(s => s.palette);
  useLayoutEffect(() => {
    visibility.current = visible;
    terminal.current?.setVisible(visible);
    if (visible) terminal.current?.focus();
    else if (terminal.current) {
      terminal.current.clearSearch();
      setSearching(false);
    }
  }, [visible]);
  useEffect(() => {
    if (!element.current) return;
    const engine = createProjectShellTerminal({ backend, shellId, runId, element: element.current, mobile, visible: visibility.current, onState: setUi });
    terminal.current = engine;
    if (visibility.current) engine.focus();
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
  return (
    <>
      {visible && searching && <ProjectShellSearch terminal={terminal} onClose={() => {
        setSearching(false);
        terminal.current?.focus();
      }} />}
      <div className="min-h-0 flex-1 overflow-auto overscroll-contain bg-terminal-bg px-2 pt-1" onClick={() => terminal.current?.focus()}>
        <div ref={element} className="h-full min-h-0 w-full" />
      </div>
      <div className="flex min-h-8 shrink-0 flex-wrap items-center gap-2 border-t border-border/70 px-2 py-1 text-ui-xs">
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
        <Button type="button" variant="ghost" size="icon-sm" className="ml-auto" aria-label={t('Search terminal output', '搜索终端输出')}
          title={t('Search terminal output', '搜索终端输出')} aria-expanded={searching} onClick={() => {
            if (searching) {
              terminal.current?.clearSearch();
              terminal.current?.focus();
            }
            setSearching(!searching);
          }}><Search /></Button>
      </div>
    </>
  );
}

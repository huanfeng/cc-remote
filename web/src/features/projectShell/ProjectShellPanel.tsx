import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { ChevronDown, ChevronLeft, GripHorizontal, MoreHorizontal, MoveDiagonal2, Square, SquareTerminal } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import type { ProjectShell } from '@/backend/types';
import { cn } from '@/components/lib/utils';
import { Button } from '@/components/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { currentHost } from '@/lib/hosts';
import { t } from '@/lib/i18n';
import { usePrefs } from '@/lib/prefs';
import { useResolvedDark } from '@/lib/theme';
import { useApp } from '@/store/app';
import { createProjectShellTerminal, initialShellTerminalState } from './projectShellTerminal';
import { shellPathKey, useProjectShellWorkspace, type ProjectShellTarget } from './ProjectShellWorkspace';
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
  const { target, retainedTarget } = useProjectShellWorkspace();
  if (!retainedTarget) return null;
  return <Panel key={retainedTarget.shellId ?? shellPathKey(retainedTarget.cwd)} target={retainedTarget} visible={!!target} mobile={mobile} />;
}

function Panel({ target, mobile, visible }: { target: ProjectShellTarget; mobile: boolean; visible: boolean }) {
  const backend = useBackend();
  const { close, remember } = useProjectShellWorkspace();
  const hostName = useApp(s => s.info?.name);
  const [shell, setShell] = useState<ProjectShell>();
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [exit, setExit] = useState<'exited' | 'stopped'>();
  const request = useRef<AbortController>(null);
  const region = useRef<HTMLElement>(null);
  const { frame, handlers, reset } = useProjectShellFrame(region, !mobile);

  const start = useCallback((restart?: boolean) => {
    request.current?.abort();
    const abort = new AbortController();
    request.current = abort;
    setBusy(true);
    setError('');
    const locator = target.shellId ? { shellId: target.shellId } : { cwd: target.cwd };
    void backend.startProjectShell({ ...locator, restart }, abort.signal).then(next => {
      if (abort.signal.aborted) return;
      remember(next, target.cwd);
      setShell(next);
      setExit(undefined);
    }).catch(err => {
      if (!abort.signal.aborted) setError(err instanceof Error ? err.message : String(err));
    }).finally(() => { if (!abort.signal.aborted) setBusy(false); });
  }, [backend, remember, target]);

  useLayoutEffect(() => {
    if (visible && !shell?.runId) region.current?.focus({ preventScroll: true });
  }, [visible, shell?.runId]);

  useEffect(() => {
    start();
    return () => request.current?.abort();
  }, [start]);

  const end = useCallback((state: 'exited' | 'stopped') => {
    if (shell) remember({ ...shell, state, runId: undefined });
    setExit(state);
  }, [remember, shell]);

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
      if (!abort.signal.aborted) setError(err instanceof Error ? err.message : String(err));
    }).finally(() => { if (!abort.signal.aborted) setBusy(false); });
  };

  const running = shell?.state === 'running' && !exit;
  const canRestart = shell && !running && !busy;
  const directory = shell?.cwd || target.cwd;
  const hostname = currentHost?.name || hostName || location.host;
  const closeLabel = mobile ? t('Back', '返回') : t('Hide terminal', '收起终端');
  const moveLabel = t('Drag the terminal or use arrow keys to move it', '拖动或使用方向键移动终端');
  const resizeLabel = t('Drag the corner or use arrow keys to resize the terminal', '拖动边角或使用方向键调整终端大小');
  return (
    <section
      ref={region}
      data-terminal
      id="project-shell-panel"
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

      {shell?.recovered && <p className="px-3 py-1 text-ui-xs text-foreground-subtle" role="status">{t('The host restarted. A new shell started in the saved directory.', '宿主已重启，新的 Shell 已在保存的目录启动。')}</p>}
      {error && <div role="alert" className="flex items-center justify-between gap-2 px-3 py-1.5 text-ui-sm text-destructive">
        <span className="min-w-0 break-words">{error}</span>
        {!shell && !busy && <Button type="button" variant="outline" size="sm" onClick={() => start()}>{t('Try again', '重试')}</Button>}
      </div>}
      {shell?.runId ? (
        <ProjectShellPane key={`${shell.id}/${shell.runId}`} shellId={shell.id} runId={shell.runId} mobile={mobile} visible={visible} onExit={end} />
      ) : busy && <div className="p-3 text-ui-sm text-foreground-subtle" role="status">{t('Opening terminal…', '正在打开终端…')}</div>}
      {canRestart && <div className="flex shrink-0 flex-wrap items-center gap-2 border-t border-border/70 px-3 py-2 text-ui-sm text-foreground-subtle">
        {t('This shell has ended.', '本次 Shell 进程已结束。')}
        <Button type="button" variant="outline" size="sm" onClick={() => start(true)}>{t('Start a new shell', '启动新终端')}</Button>
      </div>}
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
function ProjectShellPane({ shellId, runId, mobile, visible, onExit }: {
  shellId: string; runId: string; mobile: boolean; visible: boolean; onExit(state: 'exited' | 'stopped'): void;
}) {
  const backend = useBackend();
  const element = useRef<HTMLDivElement>(null);
  const terminal = useRef<ReturnType<typeof createProjectShellTerminal>>(null);
  const visibility = useRef(visible);
  const [ui, setUi] = useState(initialShellTerminalState);
  const dark = useResolvedDark();
  const palette = usePrefs(s => s.palette);
  useLayoutEffect(() => {
    visibility.current = visible;
    terminal.current?.setVisible(visible);
    if (visible) terminal.current?.focus();
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
  }, [onExit, ui.detail, ui.status]);
  const live = ui.status === 'online';
  const status = ui.status === 'connecting' ? t('Connecting…', '连接中…')
    : ui.status === 'retrying' ? t('Connection lost. Reconnecting…', '连接已断开，正在重连…')
      : ui.status === 'failed' ? t('Could not connect', '无法连接')
        : ui.status === 'exited' ? t('Ended', '已结束')
          : !ui.control ? t('Read-only', '只读') : '';
  return (
    <>
      <div className="min-h-0 flex-1 overflow-auto overscroll-contain bg-terminal-bg px-2 pt-1" onClick={() => terminal.current?.focus()}>
        <div ref={element} className="h-full min-h-0 w-full" />
      </div>
      {(status || ui.truncated || mobile) && <div className="flex min-h-8 shrink-0 flex-wrap items-center gap-2 border-t border-border/70 px-2 py-1 text-ui-xs">
        <span className="min-w-0 break-words text-foreground-subtle" role="status">
          {status}{!live && ui.status !== 'exited' && ui.detail ? ` · ${ui.detail}` : ''}
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

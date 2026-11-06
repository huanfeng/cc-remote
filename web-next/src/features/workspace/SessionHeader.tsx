// Conversation header. Desktop: one 48px row, also the window drag region.
// Phone: back/menu, title + status, and the usage orb; the rest moves into ⋯.
import {
  ChevronLeft, ListTree, Menu, MessageSquare, MoreHorizontal, PanelLeftOpen, Square, SquareTerminal,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import {
  DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { cn } from '@/components/lib/utils';
import { useBackend } from '@/backend/backend';
import type { Session } from '@/backend/types';
import { select, useApp } from '@/store/app';
import { usePrefs } from '@/lib/prefs';
import { t } from '@/lib/i18n';
import { shortPath, statusLabel, statusTone, tonePill } from '@/features/session/status';
import { SessionStatusPopover } from '@/features/header/SessionStatusPopover';
import { ModelChips } from '@/features/header/ModelChips';
import { UsageMeter } from '@/features/header/UsageMeter';

export function SessionHeader({ session, mobile, terminalView, native }: { session: Session; mobile: boolean; terminalView?: boolean; native?: boolean }) {
  const backend = useBackend();
  const open = useApp(s => s.open);
  const setApp = useApp(s => s.set);
  const sidebarCollapsed = usePrefs(s => s.sidebarCollapsed);
  const toolsExpanded = usePrefs(s => s.toolsExpanded);
  const sendKey = usePrefs(s => s.sendKey);
  const setPrefs = usePrefs(s => s.set);
  const tone = statusTone(session);
  const running = session.status === 'running';
  const tasks = session.activeTasks ?? 0;
  const isTerminal = session.mode === 'terminal';

  const toggleView = () => setPrefs({ terminalView: rememberView(usePrefs.getState().terminalView, session.id, !terminalView) });

  const status = (
    <span className={cn('inline-flex h-5 shrink-0 items-center gap-1 rounded-full px-2 text-ui-xs font-medium', tonePill[tone])}>
      {statusLabel(session.status, tasks)}
    </span>
  );

  return (
    <header
      data-tauri-drag-region
      className={cn('flex shrink-0 items-center gap-1 border-b border-border/60 bg-background', mobile ? 'h-12 px-1 pt-safe' : 'h-12 px-3')}
    >
      {mobile ? (
        <>
          <Button variant="ghost" size="icon-lg" aria-label={t('Back', '返回')} onClick={() => select(null)}>
            <ChevronLeft className="size-5" />
          </Button>
          <Button variant="ghost" size="icon-lg" aria-label={t('Sessions', '会话')} onClick={() => setApp({ drawerOpen: true })} className="-ml-1">
            <Menu className="size-4" />
          </Button>
        </>
      ) : sidebarCollapsed ? (
        <Button variant="ghost" size="icon-sm" aria-label={t('Expand sidebar', '展开侧栏')} onClick={() => setPrefs({ sidebarCollapsed: false })} className="desktop:ml-16">
          <PanelLeftOpen className="size-4" />
        </Button>
      ) : null}

      <div className="flex min-w-0 flex-1 items-center gap-2" data-tauri-drag-region>
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-2">
            <h1 className="truncate text-ui-lg font-medium">{session.title}</h1>
            {!native && !mobile && <SessionStatusPopover session={session}>{status}</SessionStatusPopover>}
          </div>
          {mobile ? (
            !native && <SessionStatusPopover session={session}><span className="flex items-center gap-1 text-ui-xs text-foreground-subtle">{statusLabel(session.status, tasks)}</span></SessionStatusPopover>
          ) : null}
        </div>
        {!mobile && !native && <ModelChips session={session} />}
        {!mobile && session.cwd && (
          <span className="hidden truncate text-ui-sm text-foreground-subtlest lg:inline" title={session.cwd}>{shortPath(session.cwd)}</span>
        )}
      </div>

      {!native && <UsageMeter session={session} mobile={mobile} />}

      {!native && running && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="outline" size={mobile ? 'icon-lg' : 'sm'} onClick={() => void backend.stop(session.id)} aria-label={t('Stop', '停止')}>
              <Square className="size-3 fill-current" />
              {!mobile && t('Stop', '停止')}
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('Stop the running turn (Esc)', '停止当前任务（Esc）')}</TooltipContent>
        </Tooltip>
      )}

      {!native && isTerminal && !mobile && (
        <div role="tablist" className="flex h-7 items-center rounded-lg bg-surface p-0.5">
          <ViewTab active={!terminalView} onClick={toggleView} icon={<MessageSquare className="size-3.5" />} label={t('Chat', '对话')} />
          <ViewTab active={!!terminalView} onClick={toggleView} icon={<SquareTerminal className="size-3.5" />} label={t('Terminal', '终端')} />
        </div>
      )}

      {!native && !mobile && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-md" aria-label={t('Tasks', '任务')} onClick={() => open({ type: 'tasks', id: session.id })} className="relative">
              <ListTree className="size-4" />
              {tasks > 0 && <span className="absolute right-0.5 top-0.5 flex size-3.5 items-center justify-center rounded-full bg-icon-blue text-ui-2xs font-semibold text-foreground-inverse">{tasks}</span>}
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('Subagents and background tasks', '子代理与后台任务')}</TooltipContent>
        </Tooltip>
      )}

      {!native && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size={mobile ? 'icon-lg' : 'icon-md'} aria-label={t('More', '更多')}>
              <MoreHorizontal className="size-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            {mobile && isTerminal && (
              <DropdownMenuItem onSelect={toggleView}>
                {terminalView ? <MessageSquare /> : <SquareTerminal />} {terminalView ? t('Show chat', '显示对话') : t('Show terminal', '显示终端')}
              </DropdownMenuItem>
            )}
            {mobile && <DropdownMenuItem onSelect={() => open({ type: 'tasks', id: session.id })}><ListTree /> {t('Tasks', '任务')}</DropdownMenuItem>}
            <DropdownMenuItem onSelect={() => open({ type: 'session-details', id: session.id })}>{t('Session details', '会话详情')}</DropdownMenuItem>
            {isTerminal && <DropdownMenuItem onSelect={() => open({ type: 'attach', id: session.id })}>{t('Attach from a local terminal', '从本地终端接入')}</DropdownMenuItem>}
            <DropdownMenuSeparator />
            <DropdownMenuCheckboxItem checked={toolsExpanded} onCheckedChange={v => setPrefs({ toolsExpanded: !!v })}>
              {t('Expand tool calls', '展开工具调用')}
            </DropdownMenuCheckboxItem>
            <DropdownMenuCheckboxItem checked={sendKey === 'mod-enter'} onCheckedChange={v => setPrefs({ sendKey: v ? 'mod-enter' : 'enter' })}>
              {t('Send with Ctrl+Enter', '用 Ctrl+Enter 发送')}
            </DropdownMenuCheckboxItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </header>
  );
}

function ViewTab({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={active ? undefined : onClick}
      className={cn(
        'flex h-6 items-center gap-1 rounded-md px-2 text-ui-sm transition-colors',
        active ? 'bg-background text-foreground shadow-xs' : 'text-foreground-subtle hover:text-foreground',
      )}
    >
      {icon}{label}
    </button>
  );
}

/** Keeps which terminal sessions show the terminal view, capped at the 100 most recent (as before). */
function rememberView(views: Record<string, boolean>, id: string, on: boolean): Record<string, boolean> {
  const next = { ...views };
  delete next[id];
  if (on) next[id] = true;
  const keys = Object.keys(next);
  for (const k of keys.slice(0, Math.max(0, keys.length - 100))) delete next[k];
  return next;
}

// The session navigator. On desktop it is the left column; on phones the same
// component is the home page (and the drawer over a conversation).
import { useMemo, useRef, useState } from 'react';
import {
  Activity, ChevronDown, History, MoreHorizontal, PanelLeftClose, Plus, Search, Settings, SquareTerminal, Trash2, X,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { cn } from '@/components/lib/utils';
import { useBackend } from '@/backend/backend';
import type { NativeSession, Provider, Session } from '@/backend/types';
import { hasFeature, select, selectionKey, useApp } from '@/store/app';
import { releaseSession, useTranscripts } from '@/store/transcripts';
import { usePrefs } from '@/lib/prefs';
import { t, lang } from '@/lib/i18n';
import { useNow } from '@/lib/hooks';
import { conversationOrder, conversationTime } from '@/domain/conversationTime';
import { hostProviders, providerName } from '@/domain/providers';
import { folderName, sessionMeta, shortPath, statusLabel, statusTone, toneDot } from '@/features/session/status';
import { HostSwitcher } from './HostSwitcher';
import { useNativeSessions } from './useNativeSessions';

const PAGE = 200;

export function Sidebar({ variant = 'desktop', onClose }: { variant?: 'desktop' | 'mobile'; onClose?: () => void }) {
  const tab = useApp(s => s.sidebarTab);
  const set = useApp(s => s.set);
  const open = useApp(s => s.open);
  const setPrefs = usePrefs(s => s.set);
  const cached = useTranscripts(s => Object.keys(s.byKey).length);
  const [query, setQuery] = useState('');
  const mobile = variant === 'mobile';

  return (
    <div data-sidebar className="flex h-full min-h-0 flex-col bg-background-win-alt text-foreground">
      {/* Brand / host row. On desktop it doubles as the window drag region. */}
      <div data-tauri-drag-region className={cn('flex shrink-0 items-center gap-1 px-2', mobile ? 'h-14 pt-safe' : 'h-12 platform-mac-desktop:pl-[78px]')}>
        <HostSwitcher />
        <div className="flex-1" data-tauri-drag-region />
        {!mobile && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="icon-sm" aria-label={t('Collapse sidebar', '收起侧栏')} onClick={() => setPrefs({ sidebarCollapsed: true })}>
                <PanelLeftClose className="size-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">{t('Collapse sidebar', '收起侧栏')} · Ctrl B</TooltipContent>
          </Tooltip>
        )}
        {onClose && (
          <Button variant="ghost" size="icon-lg" aria-label={t('Close', '关闭')} onClick={onClose} className="size-10">
            <X className="size-4" />
          </Button>
        )}
      </div>

      <div className="flex shrink-0 flex-col gap-0.5 px-2 pb-2">
        <NavButton mobile={mobile} icon={<Plus className="size-4" />} label={t('New session', '新建会话')} shortcut="Ctrl N" onClick={() => open({ type: 'new-session' })} />
        <NavButton mobile={mobile} icon={<Search className="size-4" />} label={t('Search', '搜索')} shortcut="Ctrl K" onClick={() => open({ type: 'palette' })} />
      </div>

      <div role="tablist" className="mx-2 flex shrink-0 items-center gap-1 border-t border-border/60 pt-2">
        <TabButton mobile={mobile} active={tab === 'sessions'} onClick={() => set({ sidebarTab: 'sessions' })}>{t('Sessions', '会话')}</TabButton>
        <TabButton mobile={mobile} active={tab === 'history'} onClick={() => set({ sidebarTab: 'history' })}>{t('Terminal history', '终端历史')}</TabButton>
        <div className="flex-1" />
        {cached > 0 && (
          <span className="px-1 text-ui-xs text-foreground-subtlest" title={t('Conversations kept in memory for instant switching', '保存在内存中、可即时切换的会话')}>
            {t(`${cached} cached`, `已缓存 ${cached}`)}
          </span>
        )}
      </div>

      <div className="px-2 pt-1.5 pb-1">
        <label className={cn('flex items-center gap-1.5 rounded-md bg-surface px-2 text-foreground-subtle focus-within:bg-input-focused focus-within:ring-1 focus-within:ring-border', mobile ? 'h-10' : 'h-7')}>
          <Search className="size-3.5 shrink-0" />
          <input
            value={query}
            onChange={e => setQuery(e.target.value)}
            onKeyDown={e => { if (e.key === 'Escape' && query) { e.preventDefault(); setQuery(''); } }}
            placeholder={t('Filter by title or path', '按标题或路径筛选')}
            aria-label={t('Filter sessions', '筛选会话')}
            className={cn('min-w-0 flex-1 bg-transparent text-foreground outline-none placeholder:text-foreground-subtlest', mobile ? 'text-mobile-input-safe' : 'text-ui-sm')}
          />
        </label>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
        {tab === 'sessions' ? <SessionList query={query} mobile={mobile} /> : <HistoryList query={query} mobile={mobile} />}
      </div>

      <div className="flex shrink-0 items-center gap-0.5 border-t border-border/60 px-2 pt-1.5 pb-[max(0.375rem,env(safe-area-inset-bottom))]">
        <FooterButton mobile={mobile} icon={<Settings className="size-4" />} label={t('Settings', '设置')} onClick={() => open({ type: 'settings' })} />
        <FooterButton mobile={mobile} icon={<Activity className="size-4" />} label={t('Host status', '宿主状态')} onClick={() => open({ type: 'status' })} />
      </div>
    </div>
  );
}

function NavButton({ icon, label, shortcut, onClick, mobile }: { icon: React.ReactNode; label: string; shortcut?: string; onClick: () => void; mobile?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn('group flex items-center gap-2 rounded-lg px-2 text-left text-ui-base text-foreground hover:bg-surface-hover', mobile ? 'h-10' : 'h-8')}
    >
      <span className="text-foreground-subtle group-hover:text-foreground">{icon}</span>
      <span className="flex-1 truncate">{label}</span>
      {shortcut && !mobile && <span className="text-ui-xs text-foreground-subtlest">{shortcut}</span>}
    </button>
  );
}

function TabButton({ active, children, onClick, mobile }: { active: boolean; children: React.ReactNode; onClick: () => void; mobile?: boolean }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        'rounded-md px-2 text-ui-sm font-medium transition-colors',
        mobile ? 'h-9 px-3' : 'h-6',
        active ? 'bg-selected text-foreground' : 'text-foreground-subtle hover:text-foreground',
      )}
    >
      {children}
    </button>
  );
}

function FooterButton({ icon, label, onClick, mobile }: { icon: React.ReactNode; label: string; onClick: () => void; mobile?: boolean }) {
  return (
    <button type="button" onClick={onClick} className={cn('flex flex-1 items-center justify-center gap-2 rounded-lg px-2 text-ui-sm text-foreground-subtle hover:bg-surface-hover hover:text-foreground', mobile ? 'h-10' : 'h-8')}>
      {icon}
      <span className="truncate">{label}</span>
    </button>
  );
}

function matches(q: string, ...fields: (string | undefined)[]) {
  if (!q) return true;
  const needle = q.toLowerCase();
  return fields.some(f => f?.toLowerCase().includes(needle));
}

/** Sessions grouped by project folder, like Codex/ZCode's project sections. */
function SessionList({ query, mobile }: { query: string; mobile: boolean }) {
  const sessions = useApp(s => s.sessions);
  const loaded = useApp(s => s.sessionsLoaded);
  const selection = useApp(s => s.selection);
  const clocks = useRef(new Map<string, number>());
  const [limit, setLimit] = useState(PAGE);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  useNow(30_000);

  const ordered = useMemo(
    () => conversationOrder(sessions.filter(s => matches(query, s.title, s.cwd)), clocks.current),
    [sessions, query],
  );
  const shown = ordered.slice(0, limit);
  const groups = useMemo(() => {
    const map = new Map<string, Session[]>();
    for (const s of shown) {
      const k = s.cwd.replace(/\\/g, '/').toLowerCase();
      const list = map.get(k);
      if (list) list.push(s); else map.set(k, [s]);
    }
    return [...map.values()];
  }, [shown]);

  if (!loaded) return <ListSkeleton />;
  if (!ordered.length) {
    return <p className="px-2 py-6 text-center text-ui-sm text-foreground-subtlest">{query ? t('No matching sessions', '没有匹配的会话') : t('No sessions yet', '还没有会话')}</p>;
  }
  const currentKey = selectionKey(selection);
  return (
    <div className="flex flex-col gap-2">
      {groups.map(list => {
        const cwd = list[0].cwd;
        const key = cwd.toLowerCase();
        const isCollapsed = collapsed[key];
        return (
          <section key={key}>
            <button
              type="button"
              onClick={() => setCollapsed(c => ({ ...c, [key]: !c[key] }))}
              title={cwd}
              aria-expanded={!isCollapsed}
              className={cn('group flex w-full items-center gap-1 rounded-md px-2 text-left text-ui-xs font-medium text-foreground-subtlest hover:text-foreground-subtle', mobile ? 'h-8' : 'h-6')}
            >
              <ChevronDown className={cn('size-3 shrink-0 transition-transform', isCollapsed && '-rotate-90')} />
              <span className="truncate">{folderName(cwd)}</span>
              <span className="ml-auto opacity-0 group-hover:opacity-100">{list.length}</span>
            </button>
            {!isCollapsed && (
              <div className="flex flex-col gap-px">
                {list.map(s => <SessionRow key={s.id} session={s} active={currentKey === `session:${s.id}`} mobile={mobile} />)}
              </div>
            )}
          </section>
        );
      })}
      {ordered.length > limit && (
        <Button variant="ghost" size="sm" className="self-center" onClick={() => setLimit(l => l + PAGE)}>
          {t(`Show more (${ordered.length - limit})`, `显示更多（${ordered.length - limit}）`)}
        </Button>
      )}
    </div>
  );
}

function SessionRow({ session: s, active, mobile }: { session: Session; active: boolean; mobile: boolean }) {
  const backend = useBackend();
  const info = useApp(st => st.info);
  const open = useApp(st => st.open);
  const key = selectionKey({ kind: 'session', id: s.id });
  const isCached = useTranscripts(st => key in st.byKey);
  const tone = statusTone(s);
  const time = conversationTime(s, Date.now(), lang);
  const canExit = hasFeature(info, 'queueControls') && (s.online || s.status === 'running');
  const queued = s.queued?.length ?? 0;
  const choose = () => select({ kind: 'session', id: s.id });
  return (
    <div
      role="button"
      tabIndex={0}
      aria-current={active ? 'page' : undefined}
      onClick={choose}
      onKeyDown={e => {
        // Only the row itself: Enter on the menu button inside must not also select.
        if (e.target !== e.currentTarget) return;
        if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); choose(); }
      }}
      className={cn(
        'group/row relative flex cursor-pointer items-start gap-2 rounded-lg pl-2.5 pr-1 transition-colors',
        mobile ? 'py-2.5' : 'py-1.5',
        active ? 'bg-selected' : 'hover:bg-surface-hover',
      )}
    >
      <span className="mt-[7px] flex size-2 shrink-0 items-center justify-center">
        <span className={cn('size-[7px] rounded-full', toneDot[tone])} title={statusLabel(s.status, s.activeTasks)} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className={cn('min-w-0 flex-1 truncate text-ui-base', active ? 'font-medium text-foreground' : 'text-foreground')}>{s.title}</span>
          <span className={cn('shrink-0 text-ui-xs text-foreground-subtlest', !mobile && 'group-hover/row:invisible')} title={time.title}>{time.label}</span>
        </div>
        <div className="flex items-center gap-1.5 text-ui-xs text-foreground-subtlest">
          {s.mode === 'terminal' && <SquareTerminal className="size-3 shrink-0" aria-label={t('Terminal', '终端')} />}
          <span className="truncate">{sessionMeta(s)}</span>
          {tone === 'waiting' && <span className="shrink-0 text-warning">· {statusLabel(s.status)}</span>}
          {queued > 0 && <span className="shrink-0">· {t(`${queued} queued`, `${queued} 条排队`)}</span>}
        </div>
      </div>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size={mobile ? 'icon-lg' : 'icon-xs'}
            aria-label={t('Session actions', '会话操作')}
            onClick={e => e.stopPropagation()}
            className={cn(
              mobile ? '-my-1.5 size-9 shrink-0 text-foreground-subtlest' : 'absolute right-1 top-1.5 opacity-0 focus-visible:opacity-100 group-hover/row:opacity-100 data-[state=open]:opacity-100',
            )}
          >
            <MoreHorizontal className="size-3.5" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" onClick={e => e.stopPropagation()}>
          <DropdownMenuItem onSelect={() => open({ type: 'session-details', id: s.id })}>{t('Session details', '会话详情')}</DropdownMenuItem>
          <DropdownMenuItem disabled={!isCached || active} onSelect={() => releaseSession(s.id)}>
            {t('Release page cache', '释放页面缓存')}
          </DropdownMenuItem>
          {canExit && <DropdownMenuItem onSelect={() => void backend.exit(s.id)}>{t('Exit session', '退出会话')}</DropdownMenuItem>}
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onSelect={() => open({ type: 'session-details', id: s.id })}>
            <Trash2 className="size-3.5" /> {t('Delete…', '删除…')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

function HistoryList({ query, mobile }: { query: string; mobile: boolean }) {
  const info = useApp(s => s.info);
  const provider = useApp(s => s.historyProvider);
  const set = useApp(s => s.set);
  const selection = useApp(s => s.selection);
  const { list, loading, error } = useNativeSessions(provider);
  const codex = hostProviders(info).includes('codex');
  const filtered = (list ?? []).filter(n => matches(query, n.title, n.cwd));
  const currentKey = selectionKey(selection);
  return (
    <div className="flex flex-col gap-px">
      {codex && (
        <div className="mb-1 flex gap-1 px-1">
          {(['claude', 'codex'] as Provider[]).map(p => (
            <TabButton key={p} mobile={mobile} active={provider === p} onClick={() => set({ historyProvider: p })}>{providerName(p)}</TabButton>
          ))}
        </div>
      )}
      {error ? (
        <p role="alert" className="px-2 py-6 text-center text-ui-sm text-destructive">{t('Could not load terminal history.', '无法加载终端历史。')} {error}</p>
      ) : loading && !list ? <ListSkeleton /> : filtered.length === 0 ? (
        <p className="px-2 py-6 text-center text-ui-sm text-foreground-subtlest">{t('No terminal history', '没有终端历史')}</p>
      ) : filtered.map(n => (
        <HistoryRow key={n.id} item={n} provider={provider} mobile={mobile}
          active={currentKey === selectionKey({ kind: 'native', id: n.id, provider })} />
      ))}
    </div>
  );
}

function HistoryRow({ item, provider, active, mobile }: { item: NativeSession; provider: Provider; active: boolean; mobile: boolean }) {
  const time = conversationTime({ ...item, lastConversationAt: item.lastConversationAt ?? item.updatedAt }, Date.now(), lang);
  return (
    <button
      type="button"
      onClick={() => select({ kind: 'native', id: item.id, provider })}
      className={cn('flex items-start gap-2 rounded-lg pl-2.5 pr-2 text-left', mobile ? 'py-2.5' : 'py-1.5', active ? 'bg-selected' : 'hover:bg-surface-hover')}
    >
      <History className="mt-0.5 size-3.5 shrink-0 text-foreground-subtlest" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="min-w-0 flex-1 truncate text-ui-base">{item.title}</span>
          <span className="shrink-0 text-ui-xs text-foreground-subtlest" title={time.title}>{time.label}</span>
        </div>
        <div className="truncate text-ui-xs text-foreground-subtlest">{shortPath(item.cwd)}</div>
      </div>
    </button>
  );
}

function ListSkeleton() {
  return (
    <div className="flex flex-col gap-1 px-1 pt-1" aria-hidden>
      {Array.from({ length: 6 }, (_, i) => (
        <div key={i} className="flex h-11 flex-col justify-center gap-1.5 rounded-lg px-2">
          <div className="h-2.5 rounded bg-surface" style={{ width: `${70 - i * 6}%` }} />
          <div className="h-2 w-2/5 rounded bg-surface/70" />
        </div>
      ))}
    </div>
  );
}

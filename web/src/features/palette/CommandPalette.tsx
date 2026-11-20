// Ctrl+K searches the host. Ctrl+F uses the same palette for one conversation.
// Derived from ZCode (Apache-2.0) quickpick/QuickPickCommandDialog.tsx and
// quickpick/quickPickStyles.ts; modified by cc-remote.
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { defaultFilter } from 'cmdk';
import { Activity, History, MessageSquare, MonitorCog, PanelLeft, Plus, Settings, SunMoon, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Command, CommandDialog, CommandGroup, CommandInput, CommandItem, CommandList, CommandShortcut } from '@/components/ui/command';
import { cn } from '@/components/lib/utils';
import { useBackend } from '@/backend/backend';
import type { MessageSearchHit, MessageSearchQuery, NativeSession, Provider } from '@/backend/types';
import { hasFeature, select, useApp } from '@/store/app';
import { usePrefs } from '@/lib/prefs';
import { useResolvedDark } from '@/lib/theme';
import { t, lang } from '@/lib/i18n';
import { conversationOrder, conversationTime } from '@/domain/conversationTime';
import { hostProviders, providerName } from '@/domain/providers';
import { folderName, sessionMeta, shortPath, statusTone, toneDot } from '@/features/session/status';
import { MessageContext, messageRole, messageTime } from './MessageContext';
import { useMessageSearch } from './useMessageSearch';

const itemClass = 'min-h-9 items-center rounded-xl px-2.5 text-ui-base max-md:min-h-11';
const filterClass = 'block min-h-8 min-w-0 rounded-md border border-border/70 bg-surface px-1.5 text-ui-xs text-foreground max-md:min-h-9';
const scopeLabels = {
  all: () => t('All', '全部'),
  sessions: () => t('Sessions', '会话'),
  messages: () => t('Messages', '消息'),
  actions: () => t('Actions', '操作'),
};
type Scope = keyof typeof scopeLabels;

export function CommandPalette({ open, onOpenChange, mobile, sessionId, native }: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  mobile: boolean;
  sessionId?: string;
  native?: Provider;
}) {
  const backend = useBackend();
  const sessions = useApp(s => s.sessions);
  const info = useApp(s => s.info);
  const setPrefs = usePrefs(s => s.set);
  const sidebarCollapsed = usePrefs(s => s.sidebarCollapsed);
  const dark = useResolvedDark();
  const [history, setHistory] = useState<(NativeSession & { provider: Provider })[]>([]);
  const [query, setQuery] = useState('');
  const [scope, setScope] = useState<Scope>('all');
  const [provider, setProvider] = useState<Provider | ''>('');
  const [cwd, setCwd] = useState('');
  const [days, setDays] = useState('');
  const [selectedHit, setSelectedHit] = useState<MessageSearchHit>();
  const [historyError, setHistoryError] = useState(false);
  const [historyLoading, setHistoryLoading] = useState(false);
  const results = useRef<HTMLDivElement>(null);
  const savedScroll = useRef(0);
  const canSearchMessages = hasFeature(info, 'message-search');
  const q = query.trim();
  const scoped = sessionId !== undefined;
  const messagesShown = canSearchMessages && (scoped || scope === 'all' || scope === 'messages');
  const sessionsShown = !scoped && (scope === 'all' || scope === 'sessions');
  const actionsShown = !scoped && (scope === 'all' || scope === 'actions');
  const since = useMemo(() => {
    if (!days) return undefined;
    const date = new Date();
    date.setDate(date.getDate() - Number(days));
    return date.toISOString();
  }, [days]);
  const messageQuery = useMemo<MessageSearchQuery | undefined>(
    () => open && messagesShown && q ? { q, sessionId, native, provider: provider || undefined, cwd: cwd || undefined, since } : undefined,
    [open, messagesShown, q, sessionId, native, provider, cwd, since],
  );
  const messages = useMessageSearch(messageQuery);

  useLayoutEffect(() => {
    if (!selectedHit && results.current) results.current.scrollTop = savedScroll.current;
  }, [selectedHit]);

  useEffect(() => {
    if (!open || scoped) return;
    let live = true;
    setHistoryError(false);
    setHistoryLoading(true);
    Promise.allSettled(hostProviders(info).map(p => backend.nativeSessions(p).then(l => l.map(n => ({ ...n, provider: p })))))
      .then(rs => {
        if (!live) return;
        setHistory(rs.flatMap(r => (r.status === 'fulfilled' ? r.value : [])));
        setHistoryError(rs.some(r => r.status === 'rejected'));
        setHistoryLoading(false);
      });
    return () => { live = false; };
  }, [backend, info, open, scoped]);

  const ordered = useMemo(() => conversationOrder(sessions), [sessions]);
  const folders = useMemo(() => [...new Set([...sessions, ...history].map(item => item.cwd).filter(Boolean))].sort(), [sessions, history]);
  const app = useApp.getState;
  const run = (fn: () => void) => { onOpenChange(false); fn(); };
  // Opening another surface replaces this one, so those actions skip the close.

  const actions = [
    { id: 'new', icon: Plus, label: t('New session', '新建会话'), shortcut: 'Ctrl N', run: () => app().open({ type: 'new-session' }) },
    { id: 'settings', icon: Settings, label: t('Settings', '设置'), shortcut: 'Ctrl ,', run: () => app().open({ type: 'settings' }) },
    { id: 'status', icon: Activity, label: t('Host status', '宿主状态'), run: () => app().open({ type: 'status' }) },
    {
      id: 'theme', icon: SunMoon, label: dark ? t('Switch to light theme', '切换到浅色主题') : t('Switch to dark theme', '切换到深色主题'),
      run: () => run(() => setPrefs({ theme: dark ? 'light' : 'dark' })),
    },
    ...(!mobile ? [{
      id: 'sidebar', icon: PanelLeft, label: sidebarCollapsed ? t('Show sidebar', '显示侧栏') : t('Hide sidebar', '隐藏侧栏'), shortcut: 'Ctrl B',
      run: () => run(() => setPrefs({ sidebarCollapsed: !sidebarCollapsed })),
    }] : []),
  ];

  const matchText = (key: string, keywords: string[]) => !q || defaultFilter(key, q, keywords) > 0;
  const matchSession = (item: NativeSession) => {
    const source = item.provider ?? 'claude';
    return (!provider || provider === source) && (!cwd || cwd === item.cwd) &&
      (!since || Date.parse(item.lastConversationAt || item.updatedAt) >= Date.parse(since)) &&
      matchText(item.id, [item.title, item.cwd, providerName(source)]);
  };
  const shownSessions = sessionsShown ? ordered.filter(matchSession).slice(0, q || scope === 'sessions' ? undefined : 8) : [];
  const shownHistory = sessionsShown ? history.filter(matchSession).slice(0, q || scope === 'sessions' ? undefined : 5) : [];
  const shownActions = actionsShown ? actions.filter(action => matchText(action.id, [action.label])) : [];
  const scopes: Scope[] = canSearchMessages ? ['all', 'sessions', 'messages', 'actions'] : ['all', 'sessions', 'actions'];
  const empty = !shownSessions.length && !shownHistory.length && !shownActions.length && !messages.hits.length &&
    !messages.loading && !messages.error && (!sessionsShown || !historyLoading);
  const searchTitle = scoped ? t('Search this conversation', '搜索此会话') : t('Search', '搜索');
  const globalSearchHint = canSearchMessages
    ? t('Search sessions, messages or actions…', '搜索会话、消息或操作…')
    : t('Search sessions, history or actions…', '搜索会话、终端历史或操作…');

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title={searchTitle}
      description={scoped ? t('Search saved messages in this conversation', '搜索此会话中已保存的消息') : globalSearchHint}
      className={cn(
        'max-w-xl overflow-hidden rounded-2xl! border-popover-border bg-popover p-0 shadow-xl',
        mobile ? 'top-[max(env(safe-area-inset-top),12px)] max-w-[calc(100%-1.5rem)] translate-y-0' : 'top-[18vh] translate-y-0',
      )}
    >
      {selectedHit ? <MessageContext hit={selectedHit} onBack={() => setSelectedHit(undefined)} onClose={() => onOpenChange(false)} /> : (
      <Command className="rounded-2xl bg-popover p-0.5 text-foreground" loop shouldFilter={false}>
        <div className="flex items-center border-b border-border [&>[data-slot=command-input-wrapper]]:min-w-0 [&>[data-slot=command-input-wrapper]]:flex-1 [&>[data-slot=command-input-wrapper]]:border-0">
          <CommandInput
            value={query}
            onValueChange={setQuery}
            aria-label={searchTitle}
            placeholder={scoped ? t('Search messages…', '搜索消息…') : globalSearchHint}
            className={cn('text-ui-base', mobile && 'text-mobile-input-safe')}
            inputGroupClassName={mobile ? 'h-11' : 'h-10'}
            autoFocus
          />
          {mobile && (
            <Button type="button" variant="ghost" size="icon-md" className="mr-1 size-10 shrink-0"
              aria-label={t('Close', '关闭')} onClick={() => onOpenChange(false)} onKeyDown={event => event.stopPropagation()}>
              <X />
            </Button>
          )}
        </div>
        <div className="border-b border-border/60 px-2 py-1.5" onKeyDown={event => event.stopPropagation()}>
          {!scoped && (
            <div role="group" aria-label={t('Search scope', '搜索范围')} className="flex gap-0.5">
              {scopes.map(item => (
                <button key={item} type="button" aria-pressed={scope === item} onClick={() => setScope(item)}
                  className="min-h-8 rounded-md px-2.5 text-ui-sm font-medium text-foreground-subtle hover:bg-menu-hover aria-pressed:bg-selected aria-pressed:text-foreground max-md:min-h-9">
                  {scopeLabels[item]()}
                </button>
              ))}
            </div>
          )}
          {scope !== 'actions' && (
            <div className={cn('grid gap-1', scoped ? 'grid-cols-[minmax(0,10rem)]' : 'mt-1 grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_minmax(0,1fr)]')}>
              {!scoped && <>
                <select aria-label={t('Provider', '供应方')} className={filterClass} value={provider} onChange={event => setProvider(event.target.value as Provider | '')}>
                  <option value="">{t('All providers', '所有供应方')}</option>
                  {hostProviders(info).map(item => <option key={item} value={item}>{providerName(item)}</option>)}
                </select>
                <select aria-label={t('Project', '项目')} className={filterClass} value={cwd} onChange={event => setCwd(event.target.value)}>
                  <option value="">{t('All projects', '所有项目')}</option>
                  {folders.map(folder => <option key={folder} title={folder} value={folder}>{folderName(folder)} · {shortPath(folder)}</option>)}
                </select>
              </>}
              <select aria-label={t('Date range', '时间范围')} className={filterClass} value={days} onChange={event => setDays(event.target.value)}>
                <option value="">{t('Any time', '不限时间')}</option>
                <option value="7">{t('Past 7 days', '最近 7 天')}</option>
                <option value="30">{t('Past 30 days', '最近 30 天')}</option>
                <option value="90">{t('Past 90 days', '最近 90 天')}</option>
              </select>
            </div>
          )}
        </div>
        <CommandList ref={results} className={cn('py-0.5', mobile ? 'max-h-[46dvh]' : 'max-h-[min(460px,52vh)]')}>
          {empty && <p className="py-8 text-center text-ui-sm text-foreground-subtle" role="status">{!q && (scoped || scope === 'messages') ? t('Enter a search term', '输入搜索关键词') : t('Nothing found', '没有找到结果')}</p>}
          {shownSessions.length > 0 && (
            <CommandGroup heading={t('Sessions', '会话')}>
              {shownSessions.map(s => (
                <CommandItem
                  key={s.id}
                  value={`session:${s.id}`}
                  className={itemClass}
                  onSelect={() => run(() => select({ kind: 'session', id: s.id }))}
                >
                  <span className="flex size-4 shrink-0 items-center justify-center"><span className={cn('size-[7px] rounded-full', toneDot[statusTone(s)])} /></span>
                  <span className="min-w-0 flex-1 truncate">{s.title}</span>
                  <span className="max-w-[45%] shrink-0 truncate text-ui-sm text-foreground-subtlest max-sm:hidden">{shortPath(s.cwd)} · {sessionMeta(s)}</span>
                  <span className="shrink-0 text-ui-xs text-foreground-subtlest sm:hidden">{conversationTime(s, Date.now(), lang).label}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          )}

          {shownHistory.length > 0 && (
            <CommandGroup heading={t('Terminal history', '终端历史')}>
              {shownHistory.map(n => (
                <CommandItem
                  key={`${n.provider}:${n.id}`}
                  value={`native:${n.provider}:${n.id}`}
                  className={itemClass}
                  onSelect={() => run(() => select({ kind: 'native', id: n.id, provider: n.provider }))}
                >
                  <History className="size-4 text-foreground-subtlest" />
                  <span className="min-w-0 flex-1 truncate">{n.title}</span>
                  <span className="max-w-[45%] shrink-0 truncate text-ui-sm text-foreground-subtlest max-sm:hidden">{shortPath(n.cwd)} · {providerName(n.provider)}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          )}

          {sessionsShown && q && historyLoading && <p role="status" className="px-3 py-1.5 text-ui-xs text-foreground-subtle">{t('Loading terminal history…', '正在加载终端历史…')}</p>}
          {sessionsShown && q && historyError && <p role="status" className="px-3 py-1.5 text-ui-xs text-warning">{t('Some terminal history could not be loaded.', '部分终端历史无法加载。')}</p>}

          {messages.hits.length > 0 && (
            <CommandGroup heading={t('Messages', '消息')}>
              {messages.hits.map(hit => (
                <CommandItem key={`${hit.native ?? 'saved'}:${hit.sessionId}:${hit.locator}`}
                  value={`message:${hit.native ?? 'saved'}:${hit.sessionId}:${hit.locator}`}
                  className={cn(itemClass, 'items-start py-2')} onSelect={() => {
                    savedScroll.current = results.current?.scrollTop ?? 0;
                    setSelectedHit(hit);
                  }}>
                  <MessageSquare className="mt-0.5 size-4 text-foreground-subtlest" />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-baseline gap-2">
                      <span className="min-w-0 flex-1 truncate font-medium">{hit.title || folderName(hit.cwd) || hit.sessionId}</span>
                      <time className="shrink-0 text-ui-xs text-foreground-subtle" dateTime={hit.createdAt}>{messageTime(hit.createdAt)}</time>
                    </div>
                    <div className="truncate text-ui-xs text-foreground-subtle">{messageRole(hit.kind)} · {providerName(hit.provider)}
                      {hit.native && ` · ${t('Terminal history', '终端历史')}`}
                      {hit.cwd && ` · ${shortPath(hit.cwd)}`}
                    </div>
                    <div className="mt-0.5 line-clamp-3 break-words text-ui-sm"><Snippet hit={hit} /></div>
                  </div>
                </CommandItem>
              ))}
              {messages.next && (
                <CommandItem value="more-messages" className={itemClass} disabled={messages.loading} onSelect={() => void messages.loadMore()}>
                  {t('More messages', '更多消息')}
                </CommandItem>
              )}
            </CommandGroup>
          )}
          {messages.loading && <p role="status" className="px-3 py-2 text-ui-sm text-foreground-subtle">{t('Searching messages…', '正在搜索消息…')}</p>}
          {messages.partial && <p role="status" className="px-3 py-2 text-ui-sm text-warning">{t('Some native records could not be searched. Try a provider or project filter.', '部分原生记录未能搜索。可按供应方或项目缩小范围后重试。')}</p>}
          {messages.error && (
            <CommandGroup heading={t('Messages', '消息')}>
              <p role="alert" className="break-words px-2.5 py-1 text-ui-sm text-destructive">{messages.error}</p>
              {!messages.next && <CommandItem value="retry-messages" className={itemClass} onSelect={messages.retry}>{t('Try again', '重试')}</CommandItem>}
            </CommandGroup>
          )}

          {shownActions.length > 0 && <CommandGroup heading={t('Actions', '操作')}>
            {shownActions.map(a => (
              <CommandItem key={a.id} value={`action:${a.id}`} className={itemClass} onSelect={a.run}>
                <a.icon className="size-4 text-foreground-subtle" />
                <span className="flex-1 truncate">{a.label}</span>
                {a.shortcut && !mobile && (
                  <CommandShortcut className="inline-flex h-5 min-w-7 items-center justify-center rounded-sm bg-surface px-1 font-sans tracking-normal text-foreground-subtle">{a.shortcut}</CommandShortcut>
                )}
              </CommandItem>
            ))}
          </CommandGroup>}
        </CommandList>
        {!mobile && (
          <div className="flex items-center gap-3 border-t border-border/60 px-3 py-1.5 text-ui-xs text-foreground-subtlest">
            <MonitorCog className="size-3.5" />
            <span className="flex-1 truncate">{info?.name || t('This host', '本宿主')}</span>
            <span>↑↓ {t('to move', '选择')}</span>
            <span>↵ {t('to open', '打开')}</span>
            <span>Esc {t('to close', '关闭')}</span>
          </div>
        )}
      </Command>
      )}
    </CommandDialog>
  );
}

function Snippet({ hit }: { hit: MessageSearchHit }) {
  const { snippet, matchStart, matchEnd } = hit;
  if (matchStart < 0 || matchEnd <= matchStart || matchEnd > snippet.length) return snippet;
  return <>{snippet.slice(0, matchStart)}<mark className="rounded-sm bg-warning/25 px-px text-current">{snippet.slice(matchStart, matchEnd)}</mark>{snippet.slice(matchEnd)}</>;
}

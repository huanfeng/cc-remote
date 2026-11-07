// Ctrl+K: jump to a session or a terminal-history entry, or run an action.
// Derived from ZCode (Apache-2.0) quickpick/QuickPickCommandDialog.tsx and
// quickpick/quickPickStyles.ts; modified by cc-remote.
import { useEffect, useMemo, useState } from 'react';
import { Activity, History, MonitorCog, PanelLeft, Plus, Settings, SunMoon } from 'lucide-react';
import { Command, CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList, CommandShortcut } from '@/components/ui/command';
import { cn } from '@/components/lib/utils';
import { useBackend } from '@/backend/backend';
import type { NativeSession, Provider } from '@/backend/types';
import { select, useApp } from '@/store/app';
import { usePrefs } from '@/lib/prefs';
import { useResolvedDark } from '@/lib/theme';
import { t, lang } from '@/lib/i18n';
import { conversationOrder, conversationTime } from '@/domain/conversationTime';
import { hostProviders, providerName } from '@/domain/providers';
import { sessionMeta, shortPath, statusTone, toneDot } from '@/features/session/status';

const itemClass = 'min-h-9 items-center rounded-xl px-2.5 text-ui-base max-md:min-h-11';

export function CommandPalette({ open, onOpenChange, mobile }: { open: boolean; onOpenChange: (o: boolean) => void; mobile: boolean }) {
  const backend = useBackend();
  const sessions = useApp(s => s.sessions);
  const info = useApp(s => s.info);
  const setPrefs = usePrefs(s => s.set);
  const sidebarCollapsed = usePrefs(s => s.sidebarCollapsed);
  const dark = useResolvedDark();
  const [history, setHistory] = useState<(NativeSession & { provider: Provider })[]>([]);
  const [query, setQuery] = useState('');

  useEffect(() => {
    let live = true;
    Promise.allSettled(hostProviders(info).map(p => backend.nativeSessions(p).then(l => l.map(n => ({ ...n, provider: p })))))
      .then(rs => { if (live) setHistory(rs.flatMap(r => (r.status === 'fulfilled' ? r.value : []))); });
    return () => { live = false; };
  }, [backend, info]);

  const ordered = useMemo(() => conversationOrder(sessions), [sessions]);
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

  const q = query.trim();
  const shownHistory = q ? history : history.slice(0, 5);

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Search', '搜索')}
      description={t('Search sessions, terminal history and actions', '搜索会话、终端历史和操作')}
      className={cn(
        'max-w-xl overflow-hidden rounded-2xl! border-popover-border bg-popover p-0 shadow-xl',
        mobile ? 'top-[max(env(safe-area-inset-top),12px)] max-w-[calc(100%-1.5rem)] translate-y-0' : 'top-[18vh] translate-y-0',
      )}
    >
      <Command className="rounded-2xl bg-popover p-0.5 text-foreground" loop>
        <CommandInput
          value={query}
          onValueChange={setQuery}
          placeholder={t('Search sessions, history or actions…', '搜索会话、终端历史或操作…')}
          className={cn('text-ui-base', mobile && 'text-mobile-input-safe')}
          inputGroupClassName={mobile ? 'h-11' : 'h-10'}
          autoFocus
        />
        <CommandList className={cn('py-0.5', mobile ? 'max-h-[60dvh]' : 'max-h-[min(460px,60vh)]')}>
          <CommandEmpty className="py-8 text-ui-sm text-foreground-subtle">{t('Nothing found', '没有找到结果')}</CommandEmpty>

          {ordered.length > 0 && (
            <CommandGroup heading={t('Sessions', '会话')}>
              {(q ? ordered : ordered.slice(0, 8)).map(s => (
                <CommandItem
                  key={s.id}
                  value={`session:${s.id}`}
                  keywords={[s.title, s.cwd, providerName(s.provider ?? 'claude')]}
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
                  keywords={[n.title, n.cwd, providerName(n.provider)]}
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

          <CommandGroup heading={t('Actions', '操作')}>
            {actions.map(a => (
              <CommandItem key={a.id} value={`action:${a.id}`} keywords={[a.label]} className={itemClass} onSelect={a.run}>
                <a.icon className="size-4 text-foreground-subtle" />
                <span className="flex-1 truncate">{a.label}</span>
                {a.shortcut && !mobile && (
                  <CommandShortcut className="inline-flex h-5 min-w-7 items-center justify-center rounded-sm bg-surface px-1 font-sans tracking-normal text-foreground-subtle">{a.shortcut}</CommandShortcut>
                )}
              </CommandItem>
            ))}
          </CommandGroup>
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
    </CommandDialog>
  );
}

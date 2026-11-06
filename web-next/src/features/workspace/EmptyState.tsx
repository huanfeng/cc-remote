// Shown when no session is selected (desktop): start something, or pick up
// where you left off. Codex-style: one calm centred column.
import { useMemo } from 'react';
import { ArrowUpRight, FolderGit2, Plus, SquareTerminal } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Kbd } from '@/components/ui/kbd';
import { select, useApp } from '@/store/app';
import { t, lang } from '@/lib/i18n';
import { conversationTime } from '@/domain/conversationTime';
import { recentProjects } from '@/domain/recentProjects';
import { sessionMeta, shortPath, statusTone, toneDot } from '@/features/session/status';
import { cn } from '@/components/lib/utils';

export function EmptyState() {
  const sessions = useApp(s => s.sessions);
  const info = useApp(s => s.info);
  const open = useApp(s => s.open);
  const attention = sessions.filter(s => s.status === 'waiting' || s.status === 'running');
  const projects = useMemo(() => recentProjects(sessions.map(s => ({ cwd: s.cwd, updatedAt: s.lastConversationAt ?? s.updatedAt })), 4), [sessions]);

  return (
    <div className="flex h-full min-h-0 flex-col" data-tauri-drag-region>
      <div className="h-12 shrink-0" data-tauri-drag-region />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-2xl flex-col gap-8 px-6 pb-16 pt-[12vh]">
          <div className="flex flex-col gap-2">
            <h1 className="text-ui-display font-semibold tracking-tight">{t('What should we work on?', '今天做点什么？')}</h1>
            <p className="text-ui-base text-foreground-subtle">
              {info?.name ? t(`Connected to ${info.name}.`, `已连接到 ${info.name}。`) : ''} {t('Start a session, or continue one below.', '新建一个会话，或从下面继续。')}
            </p>
          </div>

          <div className="flex flex-wrap gap-2">
            <Button size="lg" onClick={() => open({ type: 'new-session' })}>
              <Plus className="size-4" /> {t('New session', '新建会话')} <Kbd className="ml-1 border-primary-foreground/30 text-primary-foreground/70">Ctrl N</Kbd>
            </Button>
            <Button size="lg" variant="outline" onClick={() => open({ type: 'palette' })}>
              {t('Search sessions', '搜索会话')} <Kbd className="ml-1">Ctrl K</Kbd>
            </Button>
          </div>

          {attention.length > 0 && (
            <Section title={t('Needs attention', '需要关注')}>
              {attention.map(s => (
                <button key={s.id} type="button" onClick={() => select({ kind: 'session', id: s.id })}
                  className="group flex items-center gap-3 rounded-xl border border-border/70 bg-card px-3.5 py-3 text-left hover:bg-card-selected">
                  <span className={cn('size-2 shrink-0 rounded-full', toneDot[statusTone(s)])} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-ui-base font-medium">{s.title}</span>
                    <span className="block truncate text-ui-sm text-foreground-subtle">{s.live?.activity || sessionMeta(s)}</span>
                  </span>
                  <span className="shrink-0 text-ui-xs text-foreground-subtlest">{conversationTime(s, Date.now(), lang).label}</span>
                  <ArrowUpRight className="size-4 shrink-0 text-foreground-subtlest opacity-0 group-hover:opacity-100" />
                </button>
              ))}
            </Section>
          )}

          {projects.length > 0 && (
            <Section title={t('Recent projects', '最近的项目')}>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {projects.map(p => (
                  <button key={p.path} type="button" onClick={() => open({ type: 'new-session' })}
                    className="flex items-center gap-3 rounded-xl border border-border/70 px-3.5 py-3 text-left hover:bg-surface-hover" title={p.path}>
                    <FolderGit2 className="size-4 shrink-0 text-foreground-subtle" />
                    <span className="min-w-0">
                      <span className="block truncate text-ui-base">{p.name}</span>
                      <span className="block truncate text-ui-xs text-foreground-subtlest">{shortPath(p.path)}</span>
                    </span>
                  </button>
                ))}
              </div>
            </Section>
          )}

          <p className="flex items-center gap-1.5 text-ui-sm text-foreground-subtlest">
            <SquareTerminal className="size-3.5" />
            {t('Terminal sessions also open in a local terminal with', '终端会话也能在本地终端中打开：')} <code className="rounded bg-surface px-1 font-mono">ccr</code>
          </p>
        </div>
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-ui-sm font-medium text-foreground-subtle">{title}</h2>
      <div className="flex flex-col gap-2">{children}</div>
    </section>
  );
}

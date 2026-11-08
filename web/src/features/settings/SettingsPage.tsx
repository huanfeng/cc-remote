// Settings in four sections (five in the desktop app: the service on this computer). Desktop: a large dialog with a left nav.
// Phones: a full-screen page listing the sections, each opening as its own
// page (back returns to the list). Leaving a section with unsaved edits, or
// closing, asks first.
import { useCallback, useMemo, useRef, useState } from 'react';
import { Dialog as DialogPrimitive } from 'radix-ui';
import { ChevronRight, Globe2, HardDrive, KeyRound, Monitor, SlidersHorizontal, X, type LucideIcon } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { cn } from '@/components/lib/utils';
import type { SettingsSection } from '@/store/app';
import { t } from '@/lib/i18n';
import { SurfaceShell } from '@/features/dialogs/SurfaceShell';
import { ConfirmDialog } from '@/features/dialogs/common';
import { DirtyContext } from './parts';
import { DefaultsSection } from './DefaultsSection';
import { OAuthSection } from './OAuthSection';
import { HostSection } from './HostSection';
import { BrowserSection } from './BrowserSection';
import { ComputerSection } from './ComputerSection';
import { DESKTOP } from '@/platform/desktop';

const SECTIONS: { key: SettingsSection; icon: LucideIcon; title: () => string; hint: () => string }[] = [
  { key: 'defaults', icon: SlidersHorizontal, title: () => t('Session defaults', '会话默认'), hint: () => t('Supplier, model, effort, permissions', '供应商、模型、推理与权限') },
  { key: 'oauth', icon: KeyRound, title: () => t('OAuth tokens', 'OAuth 令牌'), hint: () => t('Official logins for sessions', '会话使用的官方登录') },
  { key: 'host', icon: Monitor, title: () => t('This host', '本宿主'), hint: () => t('Versions, name, colour and icon', '版本、名称、颜色与图标') },
  { key: 'browser', icon: Globe2, title: () => (DESKTOP ? t('This app', '本应用') : t('This browser', '本浏览器')), hint: () => t('Language, theme, send key, notifications', '语言、主题、发送键、通知') },
  ...(DESKTOP ? [{ key: 'computer' as const, icon: HardDrive, title: () => t('This computer', '本机服务'), hint: () => t('The cc-remote service here', '本机的 cc-remote 服务') }] : []),
];

function Body({ section, mobile }: { section: SettingsSection; mobile: boolean }) {
  switch (section) {
    case 'defaults': return <DefaultsSection mobile={mobile} />;
    case 'oauth': return <OAuthSection mobile={mobile} />;
    case 'host': return <HostSection mobile={mobile} />;
    case 'browser': return <BrowserSection mobile={mobile} />;
    case 'computer': return <ComputerSection mobile={mobile} />;
  }
}

export function SettingsPage({ open, onOpenChange, mobile, section: initial }: { open: boolean; onOpenChange: (o: boolean) => void; mobile: boolean; section?: SettingsSection }) {
  const sections = SECTIONS;
  const [section, setSection] = useState<SettingsSection | null>(initial ?? (mobile ? null : 'defaults'));
  const dirtyKeys = useRef(new Set<string>());
  const [, force] = useState(0);
  const [pending, setPending] = useState<{ run: () => void } | null>(null);

  const report = useCallback((key: string, dirty: boolean) => {
    const had = dirtyKeys.current.has(key);
    if (dirty === had) return;
    if (dirty) dirtyKeys.current.add(key); else dirtyKeys.current.delete(key);
    force(n => n + 1);
  }, []);
  const guarded = useCallback((run: () => void) => {
    if (dirtyKeys.current.size) setPending({ run });
    else run();
  }, []);
  const ctx = useMemo(() => ({ report, guard: guarded }), [report, guarded]);
  const go = (next: SettingsSection | null) => { if (next !== section) guarded(() => setSection(next)); };
  const request = (o: boolean) => { if (o) onOpenChange(true); else guarded(() => onOpenChange(false)); };

  const guard = (
    <ConfirmDialog
      open={!!pending}
      onOpenChange={o => { if (!o) setPending(null); }}
      title={t('Discard unsaved changes?', '放弃未保存的修改？')}
      description={t('Changes in this section have not been saved.', '此分区的修改尚未保存。')}
      confirmLabel={t('Discard', '放弃')}
      destructive
      onConfirm={() => { const p = pending; setPending(null); dirtyKeys.current.clear(); p?.run(); }}
    />
  );

  if (mobile) {
    const current = sections.find(s => s.key === section);
    return (
      <DirtyContext.Provider value={ctx}>
        <SurfaceShell
          open={open}
          onOpenChange={request}
          mobile
          title={current ? current.title() : t('Settings', '设置')}
          onBack={current ? () => go(null) : undefined}
        >
          {current ? (
            <div key={current.key} className="animate-in fade-in-0 slide-in-from-right-4 duration-200"><Body section={current.key} mobile /></div>
          ) : (
            <nav aria-label={t('Settings sections', '设置分区')} className="overflow-hidden rounded-xl border border-border/70 bg-card">
              {sections.map(s => (
                <button key={s.key} type="button" onClick={() => go(s.key)} className="flex min-h-14 w-full items-center gap-3 border-t border-border/60 px-4 py-3 text-left first:border-t-0 active:bg-surface-hover">
                  <span className="flex size-8 items-center justify-center rounded-lg bg-surface text-foreground-subtle"><s.icon className="size-4" /></span>
                  <span className="min-w-0 flex-1">
                    <span className="block text-ui-base font-medium">{s.title()}</span>
                    <span className="block truncate text-ui-sm text-foreground-subtle">{s.hint()}</span>
                  </span>
                  <ChevronRight className="size-4 text-foreground-subtlest" />
                </button>
              ))}
            </nav>
          )}
        </SurfaceShell>
        {guard}
      </DirtyContext.Provider>
    );
  }

  const active = section ?? 'defaults';
  return (
    <DirtyContext.Provider value={ctx}>
      <DialogPrimitive.Root open={open} onOpenChange={request}>
        <DialogPrimitive.Portal>
          <DialogPrimitive.Overlay className="fixed inset-0 isolate z-50 bg-black/60 duration-100 supports-backdrop-filter:backdrop-blur-xs data-open:animate-in data-open:fade-in-0 data-closed:animate-out data-closed:fade-out-0" />
          <DialogPrimitive.Content
            aria-describedby={undefined}
            className="fixed left-1/2 top-1/2 z-50 flex h-[min(720px,88vh)] w-[min(960px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 overflow-hidden rounded-2xl border border-popover-border bg-popover text-foreground shadow-2xl outline-none duration-150 data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0 data-closed:zoom-out-95 [app-region:no-drag]"
          >
            <nav aria-label={t('Settings sections', '设置分区')} className="flex w-60 shrink-0 flex-col gap-0.5 border-r border-border/60 bg-sidebar p-2">
              <DialogPrimitive.Title className="px-2.5 pb-3 pt-2.5 text-ui-lg font-semibold">{t('Settings', '设置')}</DialogPrimitive.Title>
              {sections.map(s => (
                <button
                  key={s.key}
                  type="button"
                  aria-current={active === s.key ? 'page' : undefined}
                  onClick={() => go(s.key)}
                  className={cn('flex h-8 items-center gap-2.5 rounded-lg px-2.5 text-left text-ui-base transition-colors', active === s.key ? 'bg-selected font-medium text-foreground' : 'text-foreground-subtle hover:bg-surface-hover hover:text-foreground')}
                >
                  <s.icon className="size-4 shrink-0" />
                  <span className="truncate">{s.title()}</span>
                  {dirtyKeys.current.size > 0 && active === s.key && <span className="ml-auto size-1.5 rounded-full bg-warning" aria-label={t('Unsaved', '未保存')} />}
                </button>
              ))}
              <div className="mt-auto px-2.5 pb-1 text-ui-xs text-foreground-subtlest">Ctrl ,</div>
            </nav>
            <div className="relative min-w-0 flex-1 overflow-y-auto">
              <DialogPrimitive.Close asChild>
                <Button variant="ghost" size="icon-sm" className="absolute right-4 top-4 z-10" aria-label={t('Close', '关闭')}><X className="size-4" /></Button>
              </DialogPrimitive.Close>
              <div key={active} className="mx-auto max-w-2xl px-8 pb-10 pt-7 animate-in fade-in-0 duration-150">
                <Body section={active} mobile={false} />
              </div>
            </div>
          </DialogPrimitive.Content>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
      {guard}
    </DirtyContext.Provider>
  );
}

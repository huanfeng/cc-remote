// The host identity at the top of the sidebar. With several saved hosts (the
// standalone page, the desktop app, or the mock's demo list) it opens the host
// list: switch (a plain click switches this tab; Ctrl/⌘/middle click or a
// long press on the link opens the host in a tab of its own), add, edit or
// forget hosts. On a host-served page it only shows which host this is.
import { useEffect, useState, type MouseEvent, type ReactNode } from 'react';
import { Check, ChevronsUpDown, Eye, EyeOff, LogOut, Pencil, Plus, X } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Button } from '@/components/ui/button';
import { cn } from '@/components/lib/utils';
import { useApp } from '@/store/app';
import {
  STANDALONE, apiBase, currentHost, forgetAllHosts, hostLink, onLan, pingAll, removeHost, shownAddress, switchHost, useHosts,
  type HostEntry, type HostSummary,
} from '@/lib/hosts';
import { useIdentity } from '@/lib/identity';
import { useIsMobile } from '@/lib/hooks';
import { t } from '@/lib/i18n';
import { hostProviders, providerName } from '@/domain/providers';
import { HostGlyph } from '@/features/settings/parts';
import { ConfirmDialog } from '@/features/dialogs/common';
import { HostFormDialog } from '@/features/standalone/HostForm';
import { useHiddenAddresses } from '@/features/standalone/parts';

function PingState({ p }: { p: HostSummary['ping'] }) {
  if (!p) return <span className="text-ui-xs text-foreground-subtlest">…</span>;
  if (!p.online) return <span className="text-ui-xs text-foreground-subtlest">{t('Offline', '离线')}</span>;
  if (p.waiting) return <span className="text-ui-xs font-medium text-warning">{t(`${p.waiting} waiting`, `${p.waiting} 待操作`)}</span>;
  if (p.running) return <span className="text-ui-xs text-icon-blue">{t(`${p.running} running`, `${p.running} 执行中`)}</span>;
  return <span className="size-1.5 rounded-full bg-success" aria-label={t('Idle', '空闲')} />;
}

export function HostSwitcher() {
  const info = useApp(s => s.info);
  const id = useIdentity();
  const hosts = useHosts(s => s.hosts);
  const providers = hostProviders(info).filter(p => (p === 'codex' ? info?.codexAvailable : info?.claudeAvailable));
  const meta = [providers.map(providerName).join(' · '), onLan ? t('LAN', '内网') : '', info?.version ? `v${info.version}` : ''].filter(Boolean).join(' · ');

  const identity = (
    <>
      <HostGlyph icon={id.icon || 'code'} color={id.color} className="size-6 rounded-md [&_svg]:size-3.5 [&_span]:text-ui-xs" />
      <span className="min-w-0">
        <span className="block truncate text-ui-base font-medium leading-tight">{id.name}</span>
        <span className="block truncate text-ui-xs leading-tight text-foreground-subtlest" title={onLan ? t('On the home network', '走内网') : undefined}>{meta}</span>
      </span>
    </>
  );

  if (!STANDALONE && hosts.length < 2) return <div className="flex h-8 min-w-0 items-center gap-2 px-1.5">{identity}</div>;
  return <HostMenu identity={identity} />;
}

function HostMenu({ identity }: { identity: ReactNode }) {
  const hosts = useHosts(s => s.hosts);
  const currentId = useHosts(s => s.currentId);
  const setHosts = useHosts(s => s.set);
  const mobile = useIsMobile();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<{ editing?: HostEntry } | null>(null);
  const [removing, setRemoving] = useState<HostSummary>();
  const [clearing, setClearing] = useState(false);
  const [hidden, setHidden] = useHiddenAddresses();
  const demo = !STANDALONE;

  useEffect(() => {
    if (!open || demo) return;
    const controller = new AbortController();
    pingAll(controller.signal);
    return () => controller.abort();
  }, [open, demo]);

  // A plain click switches this tab; a modified one opens a tab of its own (the link's default).
  const pick = (e: MouseEvent, host: HostSummary) => {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    setOpen(false);
    if (host.id === currentId) return;
    if (demo) setHosts({ currentId: host.id });
    else switchHost(host.id);
  };

  const address = (h: HostSummary) => {
    if (demo || !h.entry) return shownAddress(h.url, hidden);
    if (h.id === currentHost?.id && onLan) return `${shownAddress(apiBase, hidden)} · ${t('Home network', '内网')}`;
    return shownAddress(h.entry.url, hidden) + (h.entry.via ? ` · ${t('Address URL', '文本地址')}` : '');
  };
  const details = (h: HostSummary) => (hidden || !h.entry ? undefined
    : [h.entry.entry ? `${h.entry.entry} → ${h.entry.url}` : '', h.entry.lan ? t(`Home network: ${h.entry.lan}`, `内网：${h.entry.lan}`) : ''].filter(Boolean).join('\n') || undefined);

  const row = 'flex w-full items-center gap-2 rounded-lg px-2 text-left text-ui-base hover:bg-surface-hover focus-visible:bg-surface-hover outline-none';
  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button
            type="button"
            aria-label={t(`Switch host (${hosts.length})`, `切换宿主（共 ${hosts.length} 个）`)}
            className="flex h-8 min-w-0 items-center gap-2 rounded-lg px-1.5 text-left hover:bg-surface-hover data-[state=open]:bg-surface-hover"
          >
            {identity}
            <ChevronsUpDown className="size-3.5 shrink-0 text-foreground-subtlest" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-80 max-w-[calc(100vw-2rem)] gap-0 p-1.5">
          <div className="flex items-center gap-2 px-2 pb-1 pt-0.5">
            <span className="flex-1 text-ui-xs font-medium text-foreground-subtlest">{t('Hosts', '宿主')}</span>
            {!demo && (
              <Button variant="ghost" size="icon-sm" className="max-md:size-9" aria-pressed={hidden}
                aria-label={hidden ? t('Show addresses', '显示地址') : t('Hide addresses', '隐藏地址')}
                title={t('On this device, before showing or sharing the screen', '本设备，展示或分享屏幕前用')} onClick={() => setHidden(!hidden)}>
                {hidden ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
              </Button>
            )}
          </div>
          <div role="list" className="flex max-h-[min(60vh,420px)] flex-col gap-0.5 overflow-y-auto">
            {hosts.map(h => (
              <div role="listitem" key={h.id} className={cn('group flex items-center gap-0.5 rounded-lg', h.id === currentId && 'bg-surface')}>
                {/* The mock's demo hosts have no page to open: a link only for saved ones. */}
                <a href={demo ? '#' : hostLink(h.id)} onClick={e => (demo ? (e.preventDefault(), pick(e, h)) : pick(e, h))}
                  aria-current={h.id === currentId || undefined} className={cn(row, 'min-w-0 flex-1 py-1.5 max-md:py-2.5')}>
                  <HostGlyph icon={h.icon || 'code'} color={h.color} className="size-6 rounded-md [&_svg]:size-3.5 [&_span]:text-ui-xs" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate">{h.name}</span>
                    <span className="block truncate text-ui-xs text-foreground-subtlest" title={details(h)}>{address(h)}</span>
                  </span>
                  <PingState p={h.ping} />
                  {h.id === currentId && <Check className="size-3.5 shrink-0" aria-label={t('Current', '当前')} />}
                </a>
                {!demo && h.entry && (
                  <>
                    <Button variant="ghost" size="icon-sm" className="shrink-0 max-md:size-9"
                      aria-label={t(`Edit ${h.name}`, `编辑 ${h.name}`)} title={t('Edit name, colour, icon, address or token', '编辑名称、颜色、图标、地址或令牌')}
                      onClick={() => { setOpen(false); setForm({ editing: h.entry }); }}>
                      <Pencil className="size-3.5" />
                    </Button>
                    <Button variant="ghost" size="icon-sm" className="shrink-0 max-md:size-9"
                      aria-label={t(`Remove ${h.name}`, `移除 ${h.name}`)} title={t('Remove from this page', '从此页面移除')}
                      onClick={() => { setOpen(false); setRemoving(h); }}>
                      <X className="size-3.5" />
                    </Button>
                  </>
                )}
              </div>
            ))}
          </div>
          <div className="mt-1 flex flex-col gap-0.5 border-t border-border/60 pt-1">
            <button type="button" disabled={demo} className={cn(row, 'h-8 disabled:opacity-50 max-md:h-10')} onClick={() => { setOpen(false); setForm({}); }}>
              <Plus className="size-3.5" />{t('Add host…', '添加宿主…')}
            </button>
            {!demo && (
              <button type="button" className={cn(row, 'h-8 text-destructive max-md:h-10')} onClick={() => { setOpen(false); setClearing(true); }}>
                <LogOut className="size-3.5" />{t('Sign out of all hosts and clear…', '退出并清除所有宿主…')}
              </button>
            )}
          </div>
        </PopoverContent>
      </Popover>

      <HostFormDialog open={!!form} onOpenChange={v => { if (!v) setForm(null); }} editing={form?.editing} mobile={mobile} />
      <ConfirmDialog
        open={!!removing}
        onOpenChange={v => { if (!v) setRemoving(undefined); }}
        title={t(`Remove ${removing?.name ?? ''}?`, `移除 ${removing?.name ?? ''}？`)}
        description={t('Its token and drafts saved on this page are deleted.', '此页面保存的令牌和草稿会一并删除。')}
        confirmLabel={t('Remove', '移除')}
        destructive
        onConfirm={() => { if (removing) removeHost(removing.id); setRemoving(undefined); }}
      />
      <ConfirmDialog
        open={clearing}
        onOpenChange={setClearing}
        title={t(`Sign out of all ${hosts.length} hosts?`, `退出全部 ${hosts.length} 个宿主？`)}
        description={t('Every host, token, draft and tab saved on this page is deleted; adding a host again needs its token.', '此页面保存的所有宿主、令牌、草稿和标签页记录都会删除，无法恢复；再次添加宿主需要重新输入令牌。')}
        confirmLabel={t('Sign out and clear', '退出并清除')}
        destructive
        onConfirm={forgetAllHosts}
      />
    </>
  );
}

// The host identity at the top of the sidebar. With several saved hosts
// (standalone page, desktop app, or the mock's demo list) it is a switcher;
// on a host-served page it only shows which host this is.
import { Check, ChevronsUpDown, Laptop, Plus } from 'lucide-react';
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useApp } from '@/store/app';
import { useHosts, type HostSummary } from '@/lib/hosts';
import { t } from '@/lib/i18n';
import { hostProviders, providerName } from '@/domain/providers';

function pingLabel(p: HostSummary['ping']): string {
  if (!p) return t('Checking…', '检查中…');
  if (!p.online) return t('Offline', '离线');
  const parts = [
    p.running ? t(`${p.running} running`, `${p.running} 运行`) : '',
    p.waiting ? t(`${p.waiting} waiting`, `${p.waiting} 等待`) : '',
  ].filter(Boolean);
  return parts.join(' · ') || t('Idle', '空闲');
}

export function HostSwitcher() {
  const info = useApp(s => s.info);
  const hosts = useHosts(s => s.hosts);
  const currentId = useHosts(s => s.currentId);
  const setHosts = useHosts(s => s.set);
  const name = info?.name || t('This host', '本宿主');
  const color = info?.color || 'var(--color-brand)';
  const providers = hostProviders(info).filter(p => (p === 'codex' ? info?.codexAvailable : info?.claudeAvailable));

  const identity = (
    <>
      <span className="flex size-6 shrink-0 items-center justify-center rounded-md text-foreground-inverse" style={{ background: color }}>
        <Laptop className="size-3.5" />
      </span>
      <span className="min-w-0">
        <span className="block truncate text-ui-base font-medium leading-tight">{name}</span>
        <span className="block truncate text-ui-xs leading-tight text-foreground-subtlest">
          {providers.map(providerName).join(' · ')}{info?.version ? ` · v${info.version}` : ''}
        </span>
      </span>
    </>
  );

  if (hosts.length < 2) {
    return <div className="flex h-8 min-w-0 items-center gap-2 px-1.5">{identity}</div>;
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={t('Switch host', '切换宿主')}
          className="flex h-8 min-w-0 items-center gap-2 rounded-lg px-1.5 text-left hover:bg-surface-hover data-[state=open]:bg-surface-hover"
        >
          {identity}
          <ChevronsUpDown className="size-3.5 shrink-0 text-foreground-subtlest" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-72">
        <DropdownMenuLabel className="text-ui-xs text-foreground-subtlest">{t('Hosts', '宿主')}</DropdownMenuLabel>
        {hosts.map(h => (
          <DropdownMenuItem key={h.id} className="gap-2.5" onSelect={() => setHosts({ currentId: h.id })}>
            <span className="size-2.5 shrink-0 rounded-full" style={{ background: h.color }} />
            <span className="min-w-0 flex-1">
              <span className="block truncate">{h.name}</span>
              <span className="block truncate text-ui-xs text-foreground-subtlest">{pingLabel(h.ping)}</span>
            </span>
            {h.id === currentId && <Check className="size-3.5" aria-label={t('Current', '当前')} />}
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled><Plus className="size-3.5" /> {t('Add a host…', '添加宿主…')}</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

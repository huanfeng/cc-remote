// Host status: the service, the CLIs, listeners, session counts, totals,
// subscription usage with its history, the last 14 days and the log tail.
// Refreshes every 5 s while open.
import { useEffect, useState, type ReactNode } from 'react';
import { AlertTriangle, ChevronRight, RefreshCw, ScrollText } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { cn } from '@/components/lib/utils';
import { useBackend } from '@/backend/backend';
import type { HostStatus, Provider } from '@/backend/types';
import { useApp } from '@/store/app';
import { t } from '@/lib/i18n';
import { hostProviders, providerName } from '@/domain/providers';
import { SurfaceShell, Section } from '@/features/dialogs/SurfaceShell';
import { CopyButton, ErrorText, errorMessage } from '@/features/dialogs/common';
import { compactNumber, costText, durationText } from './usageMath';
import { Legend, UsageDetails, useUsage } from './UsageDetails';
import { DailyChart } from './charts';

const REFRESH = 5000;

export function StatusPanel({ open, onOpenChange, mobile }: { open: boolean; onOpenChange: (o: boolean) => void; mobile: boolean }) {
  const backend = useBackend();
  const [status, setStatus] = useState<HostStatus>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let live = true;
    const load = () => {
      setLoading(true);
      backend.status()
        .then(s => { if (live) { setStatus(s); setError(''); } }, e => { if (live) setError(errorMessage(e)); })
        .finally(() => { if (live) setLoading(false); });
    };
    load();
    const timer = window.setInterval(() => { if (!document.hidden) load(); }, REFRESH);
    return () => { live = false; window.clearInterval(timer); };
  }, [backend, tick]);

  const actions = (
    <span className="flex items-center gap-1.5 text-ui-xs text-foreground-subtlest">
      {!mobile && <span>{t('Refreshes every 5 s', '每 5 秒刷新')}</span>}
      <Button variant="ghost" size={mobile ? 'icon-lg' : 'icon-sm'} aria-label={t('Refresh', '刷新')} onClick={() => setTick(n => n + 1)}>
        {loading ? <Spinner className="size-3.5" /> : <RefreshCw className="size-3.5" />}
      </Button>
    </span>
  );

  return (
    <SurfaceShell
      open={open}
      onOpenChange={onOpenChange}
      mobile={mobile}
      title={t('Host status', '宿主状态')}
      description={status ? t(`cc-remote ${status.version} · ${status.os}/${status.arch}`, `cc-remote ${status.version} · ${status.os}/${status.arch}`) : undefined}
      actions={actions}
      className="sm:max-w-[860px]"
      bodyClassName="pt-1"
    >
      {!status ? (
        error ? <ErrorText>{error}</ErrorText> : <div className="flex h-60 items-center justify-center"><Spinner className="size-5 text-foreground-subtle" /></div>
      ) : (
        <div className="flex flex-col gap-6">
          {error && <ErrorText>{error}</ErrorText>}
          <HostCards status={status} />
          <Counts status={status} />
          <UsageSection />
          {status.daily && status.daily.length > 0 && (
            <Section
              title={t('Last 14 days (UTC)', '近 14 天（UTC）')}
              actions={<span className="flex gap-3 text-ui-xs text-foreground-subtle"><Legend className="bg-usage-chart-1" label={t('Turns', '轮次')} /><Legend className="bg-usage-chart-2" label={t('Cost', '费用')} /></span>}
            >
              <div className="rounded-xl border border-border/70 bg-card p-3"><DailyChart daily={status.daily} /></div>
            </Section>
          )}
          <LogTail lines={status.logTail} />
        </div>
      )}
    </SurfaceShell>
  );
}

function Card({ title, children, className }: { title: string; children: ReactNode; className?: string }) {
  return (
    <div className={cn('flex flex-col rounded-xl border border-border/70 bg-card', className)}>
      <div className="px-3.5 pb-1 pt-3 text-ui-sm font-medium text-foreground-subtle">{title}</div>
      <dl className="flex flex-col px-3.5 pb-2">{children}</dl>
    </div>
  );
}

function KV({ k, v, mono, copy }: { k: string; v: ReactNode; mono?: boolean; copy?: string }) {
  return (
    <div className="flex min-h-7 items-center gap-3 border-t border-border/50 py-1 first:border-t-0">
      <dt className="w-24 shrink-0 text-ui-sm text-foreground-subtle">{k}</dt>
      <dd className={cn('min-w-0 flex-1 truncate text-ui-sm text-foreground', mono && 'font-mono')} title={typeof v === 'string' ? v : undefined}>{v}</dd>
      {copy && <CopyButton text={copy} className="-my-1 size-5" />}
    </div>
  );
}

function HostCards({ status: s }: { status: HostStatus }) {
  const info = useApp(st => st.info);
  const service = s.service.managed
    ? t(`Starts at boot · ${s.service.manager}`, `开机自动运行 · ${s.service.manager}`)
    : t('Foreground / started manually', '前台 / 手动启动');
  const codex = info?.codexAvailable === undefined ? undefined : info.codexAvailable ? info.codexVersion || t('Available', '可用') : t('Unavailable', '不可用');
  return (
    <div className="grid gap-3 md:grid-cols-2">
      <Card title={t('Service', '服务')}>
        <KV k={t('Version', '版本')} v={s.version} />
        <KV k="PID" v={<span className="tabular-nums">{s.pid}</span>} />
        <KV k={t('Uptime', '运行时长')} v={durationText(s.uptimeSeconds)} />
        <KV k={t('System', '系统')} v={`${s.os}/${s.arch} · ${s.goVersion}`} />
        <KV k={t('Service', '托管')} v={service} />
        <KV k={t('Data', '数据目录')} v={s.dataDir} mono copy={s.dataDir} />
      </Card>
      <Card title={t('Providers', '提供方')}>
        <KV k="Claude" v={s.claude.error ? <span className="text-destructive">{s.claude.error}</span> : s.claude.version || t('Not reported', '未报告')} />
        <KV k={t('Executable', '可执行文件')} v={s.claude.path || s.claude.executable} mono copy={s.claude.path || undefined} />
        {codex !== undefined && <KV k="Codex" v={codex} />}
        <div className="mt-1 border-t border-border/50 pt-2">
          <div className="pb-1 text-ui-sm text-foreground-subtle">{t('Listening on', '监听地址')}</div>
          <ul className="flex flex-col gap-1">
            {s.listeners.map(l => (
              <li key={l.address} className="flex items-center gap-2">
                <code className="min-w-0 flex-1 truncate font-mono text-ui-sm">{l.address}</code>
                {l.loopback ? (
                  <span className="rounded-md bg-surface px-1.5 text-ui-xs text-foreground-subtle">{t('Local only', '仅本机')}</span>
                ) : l.insecure ? (
                  <span className="inline-flex items-center gap-1 rounded-md bg-warning-surface px-1.5 text-ui-xs text-warning-surface-foreground" title={t('Anyone on the network can read the traffic, including the token.', '同一网络中的任何人都能看到流量（包括令牌）。')}>
                    <AlertTriangle className="size-3" /> {t('LAN · plain HTTP', '局域网 · 明文 HTTP')}
                  </span>
                ) : (
                  <span className="rounded-md bg-success-surface px-1.5 text-ui-xs text-success-surface-foreground">{t('LAN', '局域网')}</span>
                )}
              </li>
            ))}
          </ul>
        </div>
      </Card>
    </div>
  );
}

function Stat({ label, value, tone }: { label: string; value: ReactNode; tone?: 'running' | 'waiting' }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5 rounded-xl border border-border/70 bg-card px-3 py-2.5">
      <span className="truncate text-ui-xs text-foreground-subtle">{label}</span>
      <span className={cn('text-ui-xl font-semibold tabular-nums tracking-tight', tone === 'running' && 'text-icon-blue', tone === 'waiting' && 'text-warning')}>{value}</span>
    </div>
  );
}

function Counts({ status: s }: { status: HostStatus }) {
  const totals = s.totals;
  return (
    <>
      <Section title={t('Sessions', '会话')}>
        <div className="grid grid-cols-3 gap-2 sm:grid-cols-6">
          <Stat label={t('All', '全部')} value={s.sessions.total} />
          <Stat label={t('Managed', '托管')} value={s.sessions.managed} />
          <Stat label={t('Terminal', '终端')} value={s.sessions.terminal} />
          <Stat label={t('Running', '运行中')} value={s.sessions.running} tone={s.sessions.running ? 'running' : undefined} />
          <Stat label={t('Waiting', '等待中')} value={s.sessions.waiting} tone={s.sessions.waiting ? 'waiting' : undefined} />
          <Stat label={t('Pending approvals', '待审批')} value={s.pendingRequests} tone={s.pendingRequests ? 'waiting' : undefined} />
        </div>
      </Section>
      {totals && (
        <Section title={t('Totals', '累计')} description={t('Recorded by CC Remote from managed sessions; cost is an API-price estimate.', '由 CC Remote 从托管会话记录；费用按 API 价格估算。')}>
          <div className="grid grid-cols-3 gap-2 sm:grid-cols-6">
            <Stat label={t('Turns', '轮次')} value={compactNumber(totals.turns)} />
            <Stat label={t('Cost', '费用')} value={costText(totals.costUSD)} />
            <Stat label="Tokens" value={compactNumber(totals.tokens)} />
            <Stat label={t('Run time', '执行耗时')} value={durationText(totals.durationMs / 1000)} />
            <Stat label={t('Tool calls', '工具调用')} value={compactNumber(totals.toolCalls)} />
            <Stat label={t('Errors', '错误')} value={compactNumber(totals.errors)} />
          </div>
        </Section>
      )}
    </>
  );
}

function UsageSection() {
  const info = useApp(s => s.info);
  const providers = hostProviders(info);
  const [provider, setProvider] = useState<Provider>('claude');
  const usage = useUsage(provider);
  return (
    <Section
      title={t('Subscription usage', '订阅用量')}
      actions={providers.length > 1 && (
        <div role="tablist" aria-label={t('Provider', '提供方')} className="flex rounded-lg bg-surface p-0.5">
          {providers.map(p => (
            <button
              key={p}
              type="button"
              role="tab"
              aria-selected={provider === p}
              onClick={() => setProvider(p)}
              className={cn('h-6 rounded-md px-2.5 text-ui-xs transition-colors max-md:h-9 max-md:px-3', provider === p ? 'bg-card font-medium text-foreground shadow-sm' : 'text-foreground-subtle hover:text-foreground')}
            >
              {providerName(p)}
            </button>
          ))}
        </div>
      )}
    >
      <div className="rounded-xl border border-border/70 bg-card p-3.5">
        <UsageDetails usage={usage} provider={provider} history />
      </div>
    </Section>
  );
}

function LogTail({ lines }: { lines?: string[] }) {
  const [open, setOpen] = useState(false);
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger asChild>
        <button type="button" className="flex items-center gap-1.5 text-ui-sm font-medium text-foreground-subtle hover:text-foreground max-md:min-h-9">
          <ChevronRight className={cn('size-3.5 transition-transform', open && 'rotate-90')} />
          <ScrollText className="size-3.5" />
          {t('Log', '日志')}
          {lines?.length ? <span className="font-normal text-foreground-subtlest">{t(`last ${lines.length} lines`, `最近 ${lines.length} 行`)}</span> : null}
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        {lines?.length ? (
          <pre className="mt-2 max-h-72 overflow-auto rounded-xl border border-border/70 bg-surface px-3 py-2 font-mono text-[12px] leading-5 text-foreground-subtle">
            {lines.map((l, i) => <div key={i} className={cn(/\bWARN\b/.test(l) && 'text-warning', /\bERROR\b/.test(l) && 'text-destructive')}>{l}</div>)}
          </pre>
        ) : (
          <p className="mt-2 text-ui-sm text-foreground-subtlest">{t('No log entries', '暂无日志')}</p>
        )}
      </CollapsibleContent>
    </Collapsible>
  );
}

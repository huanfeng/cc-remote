import { useEffect, useRef, useState, type ReactNode } from 'react';
import { RefreshCw } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import { BackendError } from '@/backend/errors';
import type { MonitorAccount, MonitorHistory, UsageMonitor as Monitor } from '@/backend/types';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { Section } from '@/features/dialogs/SurfaceShell';
import { ErrorText, errorMessage } from '@/features/dialogs/common';
import { t, locale } from '@/lib/i18n';
import { cn } from '@/components/lib/utils';
import { providerName } from '@/domain/providers';
import { useNow } from '@/lib/hooks';
import { UsageBar } from './UsageDetails';
import { fetchedText, resetsIn, staleText, unavailableText, validWindow } from './usageMath';
import { historyWindows, monitorAccountLabel, monitorAccountGroups, monitorAccountState, monitorRuns, windowKey } from './monitorMath';

export function UsageMonitor({ fallback }: { fallback: ReactNode }) {
  const backend = useBackend();
  const [monitor, setMonitor] = useState<Monitor>();
  const [unsupported, setUnsupported] = useState(!backend.watchUsageMonitor);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [legacy, setLegacy] = useState(false);
  const [selected, setSelected] = useState(() => new URLSearchParams(location.hash.split('?')[1]).get('account') || '');
  const [hours, setHours] = useState(24);
  const [history, setHistory] = useState<MonitorHistory>();
  const [historyError, setHistoryError] = useState('');
  const [historyTick, setHistoryTick] = useState(0);
  const section = useRef<HTMLDivElement>(null);
  const historySection = useRef<HTMLDivElement>(null);
  const groups = monitorAccountGroups(monitor?.accounts ?? []);
  const account = monitor?.accounts.find(a => a.id === selected) ?? groups.current[0] ?? groups.archived[0];

  useEffect(() => backend.watchUsageMonitor?.(v => { setMonitor(v); setError(''); }, e => {
    if (e instanceof BackendError && e.status === 404) setUnsupported(true); else setError(errorMessage(e));
  }), [backend]);
  useEffect(() => {
    const update = () => {
      const id = new URLSearchParams(location.hash.split('?')[1]).get('account');
      if (id) { setSelected(id); requestAnimationFrame(() => historySection.current?.scrollIntoView({ block: 'nearest' })); }
    };
    window.addEventListener('hashchange', update);
    return () => window.removeEventListener('hashchange', update);
  }, []);
  useEffect(() => {
    if (location.hash.startsWith('#/usage-history')) {
      const target = new URLSearchParams(location.hash.split('?')[1]).get('account') ? historySection : section;
      target.current?.scrollIntoView({ block: 'start' });
    }
  }, [!!monitor]);
  useEffect(() => {
    if (!account || !backend.monitorHistory) return;
    let live = true;
    setHistory(undefined); setHistoryError('');
    backend.monitorHistory(account.id, hours).then(v => { if (live) setHistory(v); }, e => { if (live) setHistoryError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend, account?.id, account?.fetchedAt, account?.stale, account?.error, hours, historyTick]);

  const act = async (interval?: 10 | 30) => {
    if (busy) return;
    setBusy(true);
    try {
      const v = interval ? await backend.saveUsageMonitor?.(interval) : await backend.refreshUsageMonitor?.();
      if (v) { setMonitor(v); setError(''); setHistoryTick(n => n + 1); }
    } catch (e) { setError(errorMessage(e)); } finally { setBusy(false); }
  };
  if (unsupported) return fallback;
  const selectClass = 'h-8 max-md:h-9 rounded-md border border-border bg-card px-2 text-ui-sm text-foreground';
  return <div ref={section} id="subscription-monitor">
    <Section title={t('Subscription monitor', '订阅用量监控')}
      description={t('The host records quota snapshots in the background. History is retained for 30 days.', '后台自动记录订阅额度快照，历史保留 30 天。')}
      actions={<Button variant="ghost" size="icon-sm" disabled={busy} aria-label={t('Refresh usage', '刷新用量')} onClick={() => void act()}>{busy ? <Spinner className="size-4" /> : <RefreshCw className="size-4" />}</Button>}>
      {error && <ErrorText>{error}</ErrorText>}
      {!monitor ? (!error && <Spinner className="size-4" />) : <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="text-ui-sm text-foreground-subtle">{t(`${groups.current.length} current · ${groups.archived.length} historical records`, `${groups.current.length} 个当前账号 · ${groups.archived.length} 条历史记录`)}</span>
          <label className="text-ui-xs text-foreground-subtle">{t('Sampling', '采样间隔')}{' '}
            <select className={selectClass} value={monitor.intervalMinutes} disabled={busy} onChange={e => void act(Number(e.target.value) as 10 | 30)}>
              <option value={10}>{t('10 minutes', '10 分钟')}</option><option value={30}>{t('30 minutes', '30 分钟')}</option>
            </select>
          </label>
        </div>
        {!account ? <p className="text-ui-sm text-foreground-subtle">{t('No readings yet. Sign in on the host or add an OAuth profile in Settings, then refresh.', '尚无采样。请在宿主登录官方账号，或在设置中添加 OAuth 授权后刷新。')}</p> : <>
          <div className="grid items-start gap-2 sm:grid-cols-2" aria-label={t('All account statuses', '全部账号状态')}>
            {groups.current.map(a => <AccountCard key={a.id} name={monitorAccountLabel(a, monitor.accounts)} account={a} selected={account.id === a.id} onHistory={() => {
              setSelected(a.id);
              historySection.current?.scrollIntoView({ block: 'nearest' });
            }} />)}
          </div>
          {!groups.current.length && <p className="text-ui-sm text-foreground-subtle">{t('No account is currently being sampled. Previous login records are retained below.', '当前没有正在采集的账号，下方保留以往登录的历史记录。')}</p>}
          {!!groups.archived.length && <details className="rounded-xl border border-border/70 p-3">
            <summary className="cursor-pointer text-ui-sm font-medium">{t(`Historical login records (${groups.archived.length})`, `历史登录记录（${groups.archived.length}）`)}</summary>
            <p className="my-2 text-ui-xs text-foreground-subtle">{t('Account changes or an unconfirmed identity can leave separate records. These are not additional current logins.', '切换账号或身份未确认时可能保留独立记录，这些记录不代表当前存在多个登录。')}</p>
            <div className="grid items-start gap-2 sm:grid-cols-2">{groups.archived.map(a => <AccountCard key={a.id} name={monitorAccountLabel(a, monitor.accounts)} account={a} selected={account.id === a.id} onHistory={() => {
              setSelected(a.id);
              historySection.current?.scrollIntoView({ block: 'nearest' });
            }} />)}</div>
          </details>}
          <div ref={historySection} className="flex scroll-mt-3 flex-col gap-3 rounded-xl border border-border/70 bg-card p-3.5">
            <div className="flex flex-wrap items-center gap-2">
              <h4 className="mr-auto text-ui-sm font-medium">{t('Quota history', '额度历史')}</h4>
              <select aria-label={t('History account', '历史账号')} className={cn(selectClass, 'min-w-0 max-w-full')} value={account.id} onChange={e => setSelected(e.target.value)}>
                {monitor.accounts.map(a => <option key={a.id} value={a.id}>{monitorAccountLabel(a, monitor.accounts)}{a.active ? '' : t(' (archived)', '（已停止采集）')}</option>)}
              </select>
              <select aria-label={t('History period', '历史范围')} className={selectClass} value={hours} onChange={e => setHours(Number(e.target.value))}>
                <option value={24}>{t('24 hours', '24 小时')}</option><option value={168}>{t('7 days', '7 天')}</option><option value={720}>{t('30 days', '30 天')}</option>
              </select>
            </div>
            {historyError ? <ErrorText>{historyError}</ErrorText> : history ? <MonitorChart history={history} interval={monitor.intervalMinutes} /> : <Spinner className="size-4" />}
          </div>
        </>}
      </div>}
    </Section>
    <Button variant="ghost" size="sm" className="mt-2" onClick={() => setLegacy(v => !v)}>{legacy ? t('Hide previous records', '收起旧版记录') : t('View previous records', '查看旧版记录')}</Button>
    {legacy && fallback}
  </div>;
}

function AccountCard({ account: a, name, selected, onHistory }: { account: MonitorAccount; name: string; selected: boolean; onHistory(): void }) {
  useNow(30_000);
  const state = monitorAccountState(a);
  const dim = !a.active || !a.available || !!a.stale || !!a.error;
  const note = a.error ? staleText({ available: a.available, error: a.error, profile: a.profile }, a.provider)
    : a.reason && a.reason !== 'identity-unavailable' ? unavailableText({ available: false, reason: a.reason === 'api-key' || a.reason === 'third-party' ? a.reason : 'no-login' }, providerName(a.provider)) : '';
  const labels = {
    ready: t('Up to date', '正常'), error: t('Update failed', '更新失败'), stale: t('Out of date', '数据已过期'),
    stopped: t('Collection stopped', '已停止采集'), pending: t('No reading yet', '尚未采样'), unavailable: t('Unavailable', '不可用'),
  };
  return <article aria-label={name} className="flex min-w-0 flex-col gap-2.5 rounded-xl border border-border/70 bg-card p-3">
    <div className="flex min-w-0 items-start gap-2">
      <div className="min-w-0 flex-1">
        <h4 className="break-words text-ui-base font-medium" title={a.profile}>{name}</h4>
        <div className="mt-0.5 flex flex-wrap gap-x-2 text-ui-xs text-foreground-subtlest">
          {a.plan && <span>{a.plan}</span>}
          <span title={a.fetchedAt ? new Date(a.fetchedAt).toLocaleString(locale) : undefined}>
            {fetchedText(a.fetchedAt)}
          </span>
        </div>
      </div>
      <Button variant="ghost" size="sm" className="-mr-1 -mt-1 shrink-0" aria-label={t(`View history for ${name}`, `查看 ${name} 的历史`)} aria-pressed={selected} onClick={onHistory}>
        {t('History', '历史')}
      </Button>
    </div>
    <div className="flex flex-wrap items-center gap-2 text-ui-xs">
      <span className={cn('rounded-md px-1.5 py-0.5 font-medium', state === 'ready' ? 'bg-success-surface text-success-surface-foreground' : state === 'error' || state === 'stale' ? 'bg-warning-surface text-warning-surface-foreground' : 'bg-surface text-foreground-subtle')}>{labels[state]}</span>
      {a.provider === 'codex' && !a.identityKnown && <span className="text-foreground-subtle">{t('Current quota only', '仅显示当前额度')}</span>}
      {!a.active && <span className="text-foreground-subtle">{t('History retained', '历史保留')}</span>}
    </div>
    {note && <p className={cn('break-words text-ui-xs', a.error ? 'text-warning' : 'text-foreground-subtle')}>{note}</p>}
    {a.windows.length ? <div className="grid grid-cols-2 gap-2.5">{a.windows.map(w => <div key={windowKey(w)} className="flex min-w-0 flex-col gap-1">
      <div className="flex flex-wrap items-baseline justify-between gap-x-1 text-ui-sm">
        <span className="break-words text-foreground-subtle">{w.name}</span>
        <span className="font-medium tabular-nums">{validWindow(w) ? t(`${w.percent.toFixed(1)}% used`, `已用 ${w.percent.toFixed(1)}%`) : '—'}</span>
      </div>
      <UsageBar window={w} dim={dim} />
      <span className="text-ui-xs text-foreground-subtlest" title={w.resetsAt ? new Date(w.resetsAt).toLocaleString(locale) : undefined}>{resetsIn(w.resetsAt) || t('Reset time unknown', '重置时间未知')}</span>
    </div>)}</div> : <p className="text-ui-sm text-foreground-subtle">{t('No quota reported', '尚无额度数据')}</p>}
  </article>;
}

const colors = ['stroke-usage-chart-1', 'stroke-usage-chart-3', 'stroke-usage-chart-2', 'stroke-foreground-subtle'];
function MonitorChart({ history, interval }: { history: MonitorHistory; interval: number }) {
  const windows = historyWindows(history);
  const end = Date.now(), start = end - history.hours * 3600_000;
  const box = { width: 640, height: 190, padLeft: 34, padRight: 8, padTop: 10, padBottom: 26 };
  if (!windows.length) return <p className="py-8 text-center text-ui-sm text-foreground-subtle">{t('No successful readings in this period.', '这段时间暂无成功采样。')}</p>;
  return <div>
    <svg viewBox="0 0 640 190" role="img" aria-label={t('Subscription quota history', '订阅额度历史曲线')} className="w-full">
      {[0,50,100].map(v => {const y = 10 + (1 - v/100) * 154;return <g key={v}>
        <line x1={34} x2={632} y1={y} y2={y} className="stroke-border" strokeDasharray="3 3" />
        <text x={29} y={y+4} textAnchor="end" className="fill-foreground-subtlest text-[10px]">{v}%</text>
      </g>;})}
      {windows.map((w,i) => <g key={windowKey(w)} className={colors[i % colors.length]}>
        {monitorRuns(history,windowKey(w),start,end,box,interval).map((run,j) => run.length===1
          ? <circle key={j} cx={run[0][0]} cy={run[0][1]} r={2} fill="none" strokeWidth={2} />
          : <polyline key={j} points={run.map(p => p.join(',')).join(' ')} fill="none" strokeWidth={1.75} />)}
      </g>)}
      {[start,start+(end-start)/2,end].map((at,i) => <text key={i} x={34+i*299} y={186} textAnchor={i===0?'start':i===2?'end':'middle'} className="fill-foreground-subtlest text-[10px]">{new Date(at).toLocaleString(locale,{month:'numeric',day:'numeric',hour:'2-digit',minute:'2-digit'})}</text>)}
    </svg>
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-ui-xs text-foreground-subtle">{windows.map((w,i) => <span key={windowKey(w)} className="inline-flex items-center gap-1"><svg width={16} height={8}><line x1={0} x2={16} y1={4} y2={4} strokeWidth={2} className={colors[i%colors.length]} /></svg>{w.name}</span>)}</div>
    <p className="mt-2 text-ui-xs text-foreground-subtlest">{t('Gaps indicate missed or failed readings. Percentages describe subscription quota, not token counts or charges.', '缺口表示离线或采样失败。百分比表示订阅额度，不代表精确 token 数或费用。')}</p>
  </div>;
}

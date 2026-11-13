import { useEffect, useRef, useState, type ReactNode } from 'react';
import { RefreshCw } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import { BackendError } from '@/backend/errors';
import type { MonitorHistory, UsageMonitor as Monitor } from '@/backend/types';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { Section } from '@/features/dialogs/SurfaceShell';
import { ErrorText, errorMessage } from '@/features/dialogs/common';
import { t, locale } from '@/lib/i18n';
import { cn } from '@/components/lib/utils';
import { UsageBar } from './UsageDetails';
import { historyWindows, monitorRuns, windowKey } from './monitorMath';

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
  const account = monitor?.accounts.find(a => a.id === selected) ?? monitor?.accounts[0];

  useEffect(() => backend.watchUsageMonitor?.(v => { setMonitor(v); setError(''); }, e => {
    if (e instanceof BackendError && e.status === 404) setUnsupported(true); else setError(errorMessage(e));
  }), [backend]);
  useEffect(() => {
    const update = () => { const id = new URLSearchParams(location.hash.split('?')[1]).get('account'); if (id) setSelected(id); };
    window.addEventListener('hashchange', update);
    return () => window.removeEventListener('hashchange', update);
  }, []);
  useEffect(() => {
    if (location.hash.startsWith('#/usage-history')) section.current?.scrollIntoView({ block: 'start' });
  }, [!!monitor]);
  useEffect(() => {
    if (!account || !backend.monitorHistory) return;
    let live = true;
    setHistory(undefined); setHistoryError('');
    backend.monitorHistory(account.id, hours).then(v => { if (live) setHistory(v); }, e => { if (live) setHistoryError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend, account?.id, account?.fetchedAt, account?.stale, account?.error, hours, historyTick]);

  const act = async (interval?: 10 | 30) => {
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
      {!monitor ? (!error && <Spinner className="size-4" />) : <div className="flex flex-col gap-3 rounded-xl border border-border/70 bg-card p-3.5">
        <div className="flex flex-wrap items-center gap-2">
          <label className="text-ui-xs text-foreground-subtle">{t('Sampling', '采样间隔')}{' '}
            <select className={selectClass} value={monitor.intervalMinutes} disabled={busy} onChange={e => void act(Number(e.target.value) as 10 | 30)}>
              <option value={10}>{t('10 minutes', '10 分钟')}</option><option value={30}>{t('30 minutes', '30 分钟')}</option>
            </select>
          </label>
          {account && <select aria-label={t('Account', '账号')} className={cn(selectClass, 'min-w-0 max-w-full flex-1')} value={account.id} onChange={e => setSelected(e.target.value)}>
            {monitor.accounts.map(a => <option key={a.id} value={a.id}>{a.provider} · {a.name}{a.active ? '' : t(' (archived)', '（已停止采集）')}</option>)}
          </select>}
        </div>
        {!account ? <p className="text-ui-sm text-foreground-subtle">{t('No readings yet. Sign in on the host or add an OAuth profile in Settings, then refresh.', '尚无采样。请在宿主登录官方账号，或在设置中添加 OAuth 授权后刷新。')}</p> : <>
          <div className="text-ui-xs text-foreground-subtle">
            {account.plan && <span>{account.plan} · </span>}
            {t('Updated: ', '更新：')}{account.fetchedAt ? new Date(account.fetchedAt).toLocaleString(locale) : '—'}
            {!account.active && <span> · {t('Collection stopped; history retained', '已停止采集，历史保留')}</span>}
          </div>
          {!account.identityKnown && <p className="text-ui-xs text-foreground-subtle">{account.provider === 'codex'
            ? t('This Codex CLI cannot confirm account identity. Only the current quota is shown; update the CLI to enable safe account history.', '此版本 Codex 无法确认账号身份，仅显示当前额度；更新 CLI 后可按账号记录历史。')
            : t('Account identity is unavailable for this credential. It is recorded separately to avoid merging different accounts.', '此授权无法读取账号身份，暂时独立记录，避免合并不同账号。')}</p>}
          {(account.stale || account.error) && <p role="status" className="text-ui-xs text-warning">{t('Out of date: ', '数据已过期：')}{account.error || t('waiting for an update', '等待更新')}</p>}
          <div className="grid gap-3 sm:grid-cols-2">{account.windows.map(w => <div key={windowKey(w)} className="flex flex-col gap-1">
            <div className="flex justify-between text-ui-sm"><span>{w.name}</span><span className="tabular-nums">{w.percent.toFixed(1)}%</span></div>
            <UsageBar window={w} dim={account.stale} />
            <span className="text-ui-xs text-foreground-subtlest">{w.resetsAt ? t('Resets: ', '重置：') + new Date(w.resetsAt).toLocaleString(locale) : t('Reset time unknown', '重置时间未知')}</span>
          </div>)}</div>
          <div className="flex items-center justify-between gap-2 border-t border-border/60 pt-3">
            <span className="text-ui-sm">{t('Quota history', '额度历史')}</span>
            <select aria-label={t('History period', '历史范围')} className={selectClass} value={hours} onChange={e => setHours(Number(e.target.value))}>
              <option value={24}>{t('24 hours', '24 小时')}</option><option value={168}>{t('7 days', '7 天')}</option><option value={720}>{t('30 days', '30 天')}</option>
            </select>
          </div>
          {historyError ? <ErrorText>{historyError}</ErrorText> : history ? <MonitorChart history={history} interval={monitor.intervalMinutes} /> : <Spinner className="size-4" />}
        </>}
      </div>}
    </Section>
    <Button variant="ghost" size="sm" className="mt-2" onClick={() => setLegacy(v => !v)}>{legacy ? t('Hide previous records', '收起旧版记录') : t('View previous records', '查看旧版记录')}</Button>
    {legacy && fallback}
  </div>;
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

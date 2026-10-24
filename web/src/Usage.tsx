import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { Api } from './ClaudeSettings';
import { locale, t } from './i18n';
import './usage.css';

// Claude subscription usage (5-hour and weekly limits), read by the host with
// Claude Code's own login. Only an official subscription has it.

type Window = { percent: number; resetsAt?: string; severity?: string };
export type UsageReply = {
  available: boolean; reason?: 'third-party' | 'api-key' | 'no-login'; provider?: string; plan?: string;
  usage?: { fiveHour?: Window; weekly?: Window; models?: (Window & { name: string })[] };
  fetchedAt?: string; stale?: boolean; error?: string;
};
type Point = { at: string; fiveHour?: number; weekly?: number };

const POLL = 60_000;

/** Polls /api/usage while the page is visible; undefined until the first answer. */
export function useUsage(api: Api) {
  const [result, setResult] = useState<{ api: Api; usage: UsageReply }>();
  const source = useRef(api);
  source.current = api;
  useEffect(() => {
    let active = true;
    let request = 0;
    const load = () => {
      if (document.hidden) return;
      const id = ++request;
      const current = () => active && source.current === api && id === request;
      api<UsageReply>('/api/usage').then(usage => {
        if (current()) setResult({ api, usage });
      }).catch(() => {
        if (current()) setResult(previous => ({ api, usage: {
          ...(previous?.api === api ? previous.usage : { available: true }),
          stale: true, error: 'request-failed',
        } }));
      });
    };
    load();
    const timer = window.setInterval(load, POLL);
    document.addEventListener('visibilitychange', load);
    return () => { active = false; clearInterval(timer); document.removeEventListener('visibilitychange', load); };
  }, [api]);
  return result?.api === api ? result.usage : undefined;
}

const level = (w: Window) => w.severity && w.severity !== 'normal' ? (w.severity === 'warning' ? 'warn' : 'high') : w.percent >= 90 ? 'high' : w.percent >= 75 ? 'warn' : 'ok';
const percent = (w: Window) => `${Math.round(w.percent)}%`;

function until(iso?: string) {
  const at = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(at)) return '';
  const m = Math.max(0, Math.round((at - Date.now()) / 60000)), d = Math.floor(m / 1440), h = Math.floor(m % 1440 / 60);
  return d ? t(`${d}d ${h}h`, `${d} 天 ${h} 小时`) : h ? t(`${h}h ${m % 60}m`, `${h} 小时 ${m % 60} 分`) : t(`${m}m`, `${m} 分钟`);
}

export function ago(iso?: string) {
  const at = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(at)) return '';
  const s = Math.max(0, (Date.now() - at) / 1000);
  return s < 60 ? t('just now', '刚刚') : s < 3600 ? t(`${Math.floor(s / 60)} min ago`, `${Math.floor(s / 60)} 分钟前`) : s < 86400 ? t(`${Math.floor(s / 3600)} h ago`, `${Math.floor(s / 3600)} 小时前`) : new Date(at).toLocaleString(locale);
}

const staleText = (u: UsageReply) => u.error === 'expired' ? t('The Claude Code login expired; it is renewed the next time Claude Code runs', 'Claude Code 的登录令牌已过期，下次 Claude Code 运行时会自动续期')
  : u.error === 'rate-limited' ? t('Too many requests; trying again later', '请求过于频繁，稍后重试') : u.error ? t(`Could not update: ${u.error}`, `更新失败：${u.error}`) : '';

export const unavailableText = (u: UsageReply) => u.reason === 'third-party' ? t(`Claude Code uses ${u.provider || 'another provider'}; subscription usage applies to an official Claude login only.`, `Claude Code 当前使用 ${u.provider || '第三方服务'}，订阅用量只适用于官方 Claude 账号登录。`)
  : u.reason === 'api-key' ? t('Claude Code uses an API key, billed per token; there are no subscription limits.', 'Claude Code 当前使用 API Key，按 token 计费，没有订阅额度。')
  : t('Claude Code is not signed in with a Claude subscription on this host.', '此宿主上的 Claude Code 没有用 Claude 订阅账号登录。');

/** The sidebar line: 5-hour and weekly usage; opens the details. */
export function UsageBar({ api, open }: { api: Api; open: () => void }) {
  const usage = useUsage(api);
  const u = usage?.usage;
  const valid = (w?: Window): w is Window => !!w && Number.isFinite(w.percent) && w.percent >= 0 && w.percent <= 100;
  const fresh = !!usage?.available && !usage.stale && !usage.error;
  const state = !usage ? t('Loading usage', '正在加载用量')
    : usage.error ? staleText(usage)
    : usage.stale ? t('Usage is out of date', '用量已过期')
    : !usage.available ? unavailableText(usage) : t('Usage unknown', '用量未知');
  const item = (label: string, w?: Window) => {
    const known = fresh && valid(w);
    return <span className="usage-item">
      <svg className="usage-ring" viewBox="0 0 32 32" aria-hidden="true" focusable="false">
        <circle className="usage-ring-track" cx="16" cy="16" r="13" />
        {known && <circle className={`usage-ring-fill ${level(w)}`} cx="16" cy="16" r="13" pathLength="100" strokeDasharray={`${w.percent} 100`} transform="rotate(-90 16 16)" />}
      </svg>
      <span className="usage-label">{label}</span><span className="usage-value">{known ? percent(w) : '—'}</span>
    </span>;
  };
  const summary = (label: string, w?: Window) => {
    const value = valid(w) ? t(`${percent(w)} used${fresh ? '' : ' (cached)'}`, `已使用 ${percent(w)}${fresh ? '' : '（缓存）'}`) : t('unknown', '未知');
    const reset = w?.resetsAt && Number.isFinite(Date.parse(w.resetsAt))
      ? t(`resets in ${until(w.resetsAt)} (${new Date(w.resetsAt).toLocaleString(locale)})`, `${until(w.resetsAt)}后重置（${new Date(w.resetsAt).toLocaleString(locale)}）`) : t('reset time unknown', '重置时间未知');
    return `${label}: ${value}, ${reset}`;
  };
  const title = [t('Claude subscription usage', 'Claude 订阅用量'),
    summary(t('5-hour limit', '5 小时额度'), u?.fiveHour), summary(t('Weekly limit', '每周额度'), u?.weekly),
    usage?.fetchedAt && Number.isFinite(Date.parse(usage.fetchedAt)) ? t(`Updated ${ago(usage.fetchedAt)} (${new Date(usage.fetchedAt).toLocaleString(locale)})`, `更新于${ago(usage.fetchedAt)}（${new Date(usage.fetchedAt).toLocaleString(locale)}）`) : t('Update time unknown', '更新时间未知'),
    (!fresh || !valid(u?.fiveHour) || !valid(u?.weekly)) && state,
    t('Open service status details', '打开服务状态详情'),
  ].filter(Boolean).join('\n');
  return <button type="button" className="usage-bar" aria-label={title} title={title} onClick={open}>
    {item('5h', u?.fiveHour)}{item(t('Week', '周'), u?.weekly)}
  </button>;
}

function HistoryChart({ api }: { api: Api }) {
  const [hours, setHours] = useState(24);
  const [points, setPoints] = useState<Point[]>();
  const box = useRef<HTMLElement>(null);
  const [w, setW] = useState(560);
  useLayoutEffect(() => {
    const element = box.current; if (!element) return;
    const observer = new ResizeObserver(() => setW(Math.max(280, Math.round(element.clientWidth - 28))));
    observer.observe(element); return () => observer.disconnect();
  }, []);
  useEffect(() => {
    let active = true;
    api<{ points: Point[] }>(`/api/usage/history?hours=${hours}`).then(x => { if (active) setPoints(x.points); }).catch(() => { if (active) setPoints([]); });
    return () => { active = false; };
  }, [api, hours]);
  const h = 150, pad = 26, end = Date.now(), start = end - hours * 3600_000;
  const x = (at: string) => pad + (Date.parse(at) - start) / (end - start) * (w - pad * 2);
  const y = (v: number) => h - pad - v / 100 * (h - pad * 2);
  // A gap longer than three samples (the host was off) breaks the line.
  const lines = (key: 'fiveHour' | 'weekly') => {
    const runs: string[][] = [];
    let prev = 0;
    for (const p of points || []) {
      const v = p[key];
      if (v === undefined) continue;
      const at = Date.parse(p.at);
      if (!runs.length || at - prev > 30 * 60_000) runs.push([]);
      runs[runs.length - 1].push(`${x(p.at).toFixed(1)},${y(v).toFixed(1)}`);
      prev = at;
    }
    return runs.map((run, i) => run.length === 1 ? <circle key={i} cx={run[0].split(',')[0]} cy={run[0].split(',')[1]} r="2" className={`usage-dot ${key}`} /> : <polyline key={i} points={run.join(' ')} className={`usage-line ${key}`} />);
  };
  const ticks = hours <= 24 ? [0, 6, 12, 18, 24].map(n => end - n * 3600_000) : Array.from({ length: Math.round(hours / 24) + 1 }, (_, n) => end - n * 86400_000);
  const label = (at: number) => hours <= 24 ? new Date(at).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' }) : new Date(at).toLocaleDateString(locale, { month: 'numeric', day: 'numeric' });
  return <figure className="status-chart usage-chart" ref={box}>
    <figcaption><span><i className="swatch usage-5h" />{t('5-hour', '5 小时')}</span><span><i className="swatch usage-week" />{t('Weekly', '每周')}</span>
      <span className="usage-range" role="group" aria-label={t('Time range', '时间范围')}>{[24, 168, 720].map(n => <button key={n} type="button" className={hours === n ? 'active' : ''} onClick={() => setHours(n)}>{n === 24 ? t('24 h', '24 小时') : n === 168 ? t('7 days', '7 天') : t('30 days', '30 天')}</button>)}</span></figcaption>
    {points && !points.length ? <p className="usage-empty">{t('No readings in this period yet. The host records usage every 10 minutes while the login is valid.', '这段时间还没有记录。登录有效时宿主每 10 分钟记录一次用量。')}</p> :
      <svg viewBox={`0 0 ${w} ${h}`} role="img" aria-label={t('Usage over time', '用量变化')}>
        {[0, 50, 100].map(v => <g key={v}><line x1={pad} x2={w - pad} y1={y(v)} y2={y(v)} className={v ? 'grid' : 'axis'} /><text x={pad - 4} y={y(v) + 3} textAnchor="end" className="scale">{v}%</text></g>)}
        {ticks.filter((_, i) => w > 420 || i % 2 === 0).map(at => <text key={at} x={pad + (at - start) / (end - start) * (w - pad * 2)} y={h - 6} textAnchor="middle">{label(at)}</text>)}
        {lines('weekly')}{lines('fiveHour')}
      </svg>}
  </figure>;
}

/** The status panel section: every limit with its reset, the update time and the history. */
export function UsageDetails({ api }: { api: Api }) {
  const usage = useUsage(api);
  if (!usage) return null;
  if (!usage.available) return <div className="status-block usage-details"><h3>{t('Subscription usage', '订阅用量')}</h3><p className="usage-note">{unavailableText(usage)}</p></div>;
  const u = usage.usage;
  const row = (label: string, w?: Window) => w && <div className="usage-row" key={label}>
    <span className="usage-row-label">{label}</span>
    <span className="usage-track wide"><span className={`usage-fill ${level(w)}`} style={{ width: `${w.percent}%` }} /></span>
    <strong>{percent(w)}</strong>
    <small title={w.resetsAt ? new Date(w.resetsAt).toLocaleString(locale) : undefined}>{w.resetsAt ? t(`resets in ${until(w.resetsAt)}`, `${until(w.resetsAt)}后重置`) : ''}</small>
  </div>;
  return <div className="status-block usage-details">
    <h3>{t('Subscription usage', '订阅用量')}{usage.plan && <span className="usage-plan">{usage.plan}</span>}
      <span className="usage-updated" title={usage.fetchedAt ? new Date(usage.fetchedAt).toLocaleString(locale) : undefined}>{usage.fetchedAt ? t(`Updated ${ago(usage.fetchedAt)}`, `更新于${ago(usage.fetchedAt)}`) : t('Not read yet', '尚未读取')}</span></h3>
    {usage.stale && <p className="usage-note warn">{staleText(usage)}</p>}
    {u && <div className="usage-rows">{row(t('5-hour limit', '5 小时额度'), u.fiveHour)}{row(t('Weekly limit', '每周额度'), u.weekly)}{u.models?.map(m => row(t(`Weekly · ${m.name}`, `每周 · ${m.name}`), m))}</div>}
    <HistoryChart api={api} />
  </div>;
}

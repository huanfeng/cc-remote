import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { Api } from './ClaudeSettings';
import './status.css';
import { CloseIcon } from './icons';
import { locale, t } from './i18n';
import { UsageDetails } from './Usage';

export type Stats = { sessionId: string; turns: number; results: number; costUSD: number; costKnown: boolean; inputTokens: number; outputTokens: number; cacheReadTokens: number; cacheCreationTokens: number; durationMs: number; toolCalls: number; errors: number; lastActivityAt: string };
type SessionRow = Stats & { id: string; title: string; mode: string; status: string; online: boolean; cwd: string };
type Daily = { date: string; turns: number; costUSD: number; tokens: number };
export type HostStatus = {
  version: string; pid: number; startedAt: string; uptimeSeconds: number; listen: string; listeners: { address: string; loopback: boolean; insecure?: boolean }[];
  dataDir: string; claudeConfigDir: string; logFile: string; goVersion: string; os: string; arch: string;
  claude: { executable: string; path: string; version: string; error?: string }; service: { managed: boolean; manager: string };
  sessions: Record<'total' | 'managed' | 'terminal' | 'running' | 'waiting' | 'idle', number>;
  pendingRequests: number; activeRuns: number; storage: { databaseBytes: number; uploadsBytes: number; logBytes: number };
  totals: Stats; daily: Daily[]; sessionStats: SessionRow[]; logTail: string[];
};
type SortKey = 'title' | 'mode' | 'status' | 'turns' | 'tokens' | 'costUSD' | 'durationMs' | 'lastActivityAt';

const statusText: Record<string, string> = { running: t('Running', '执行中'), waiting: t('Waiting for answer', '等待回答'), idle: t('Ready', '就绪'), completed: t('Completed', '已完成'), interrupted: t('Stopped', '已停止'), failed: t('Failed', '执行失败'), offline: t('Offline', '离线') };
const managerText: Record<string, string> = { schtasks: t('Windows Task Scheduler', 'Windows 计划任务'), systemd: t('systemd user service', 'systemd 用户服务') };
export const tokens = (x: Stats) => x.inputTokens + x.outputTokens + x.cacheReadTokens + x.cacheCreationTokens;
export const number = (n: number) => n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1e4 ? `${(n / 1e3).toFixed(1)}k` : n.toLocaleString(locale);
export const money = (x: Pick<Stats, 'costUSD' | 'costKnown'>) => x.costKnown ? `$${x.costUSD.toFixed(x.costUSD >= 10 ? 2 : 4)}` : '—';
const bytes = (n: number) => n >= 1 << 30 ? `${(n / (1 << 30)).toFixed(1)} GiB` : n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MiB` : n >= 1024 ? `${(n / 1024).toFixed(1)} KiB` : `${n} B`;
export function duration(seconds: number) {
  const d = Math.floor(seconds / 86400), h = Math.floor(seconds % 86400 / 3600), m = Math.floor(seconds % 3600 / 60);
  const s = Math.floor(seconds % 60);
  return d ? t(`${d}d ${h}h`, `${d} 天 ${h} 小时`) : h ? t(`${h}h ${m}m`, `${h} 小时 ${m} 分`) : m ? t(`${m}m ${s}s`, `${m} 分 ${s} 秒`) : t(`${Math.floor(seconds)}s`, `${Math.floor(seconds)} 秒`);
}
function ago(iso: string) {
  const at = Date.parse(iso); if (!at) return '—';
  const s = Math.max(0, (Date.now() - at) / 1000);
  return s < 60 ? t('just now', '刚刚') : s < 3600 ? t(`${Math.floor(s / 60)} min ago`, `${Math.floor(s / 60)} 分钟前`) : s < 86400 ? t(`${Math.floor(s / 3600)} h ago`, `${Math.floor(s / 3600)} 小时前`) : new Date(at).toLocaleDateString(locale);
}

function Chart({ daily }: { daily: Daily[] }) {
  const box = useRef<HTMLElement>(null);
  const [w, setW] = useState(560);
  useLayoutEffect(() => {
    const element = box.current; if (!element) return;
    const observer = new ResizeObserver(() => setW(Math.max(280, Math.round(element.clientWidth - 28))));
    observer.observe(element); return () => observer.disconnect();
  }, []);
  const h = 150, pad = 22, bw = (w - pad * 2) / Math.max(daily.length, 1);
  const maxTurns = Math.max(1, ...daily.map(x => x.turns)), maxCost = Math.max(0, ...daily.map(x => x.costUSD));
  const y = (v: number, max: number) => h - pad - (max ? v / max : 0) * (h - pad * 2);
  const line = daily.map((x, i) => `${pad + bw * i + bw / 2},${y(x.costUSD, maxCost)}`).join(' ');
  return <figure className="status-chart" ref={box}>
    <figcaption><span><i className="swatch turns" />{t('Turns', '轮次')}</span>{maxCost > 0 && <span><i className="swatch cost" />{t('Cost', '费用')}</span>}<small>{t('Last 14 days (UTC)', '近 14 天（UTC）')}</small></figcaption>
    <svg viewBox={`0 0 ${w} ${h}`} role="img" aria-label={t('Turns and cost over the last 14 days', '近 14 天轮次与费用')}>
      <line x1={pad} x2={w - pad} y1={h - pad} y2={h - pad} className="axis" />
      {daily.map((x, i) => <g key={x.date}>
        <title>{t(`${x.date}: ${x.turns} turns · $${x.costUSD.toFixed(4)} · ${number(x.tokens)} tokens`, `${x.date}：${x.turns} 轮 · $${x.costUSD.toFixed(4)} · ${number(x.tokens)} tokens`)}</title>
        <rect x={pad + bw * i + bw * .18} width={bw * .64} y={y(x.turns, maxTurns)} height={Math.max(0, h - pad - y(x.turns, maxTurns))} rx="3" className="bar" />
        {(w > 520 || i % 2 === (daily.length - 1) % 2) && <text x={pad + bw * i + bw / 2} y={h - 6} textAnchor="middle">{x.date.slice(5)}</text>}
      </g>)}
      {maxCost > 0 && <><polyline points={line} className="cost-line" />{daily.map((x, i) => <circle key={x.date} cx={pad + bw * i + bw / 2} cy={y(x.costUSD, maxCost)} r="2.5" className="cost-dot" />)}</>}
      <text x={pad} y={12} className="scale">{t(`${maxTurns} turns`, `${maxTurns} 轮`)}</text>
      {maxCost > 0 && <text x={w - pad} y={12} textAnchor="end" className="scale">${maxCost.toFixed(2)}</text>}
    </svg>
  </figure>;
}

export function StatusPanel({ api, close, select, providerStatus }: { api: Api; close: () => void; select: (id: string) => void; providerStatus?: { codexAvailable?: boolean; codexVersion?: string } }) {
  const [data, setData] = useState<HostStatus>();
  const [error, setError] = useState('');
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'lastActivityAt', desc: true });
  const [logOpen, setLogOpen] = useState(false);
  const log = useRef<HTMLPreElement>(null);
  const closeRef = useRef(close); closeRef.current = close;
  useEffect(() => {
    const controller = new AbortController();
    let inFlight = false;
    // Skip a tick while the previous request is still running so slow hosts never pile up requests.
    const load = async () => {
      if (document.hidden || inFlight) return;
      inFlight = true;
      try { const next = await api<HostStatus>('/api/status', { signal: controller.signal }); if (!controller.signal.aborted) { setData(next); setError(''); } } catch (err) { if (!controller.signal.aborted) setError((err as Error).message); } finally { inFlight = false; }
    };
    void load(); const timer = window.setInterval(load, 5000);
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') closeRef.current(); };
    document.addEventListener('keydown', key);
    return () => { controller.abort(); clearInterval(timer); document.removeEventListener('keydown', key); };
  }, [api]);
  useLayoutEffect(() => { if (log.current) log.current.scrollTop = log.current.scrollHeight; }, [data?.logTail, logOpen]);
  const value = (x: SessionRow) => sort.key === 'tokens' ? tokens(x) : x[sort.key];
  const rows = [...data?.sessionStats || []].sort((a, b) => { const x = value(a), y = value(b); const c = typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y), locale); return sort.desc ? -c : c; });
  const header = (key: SortKey, label: string) => <th aria-sort={sort.key === key ? sort.desc ? 'descending' : 'ascending' : undefined}><button onClick={() => setSort(old => ({ key, desc: old.key === key ? !old.desc : key !== 'title' && key !== 'mode' && key !== 'status' }))}>{label}{sort.key === key ? sort.desc ? ' ↓' : ' ↑' : ''}</button></th>;
  const totals = data?.totals;
  const L = { title: t('Title', '标题'), mode: t('Mode', '模式'), status: t('Status', '状态'), turns: t('Turns', '轮次'), cost: t('Cost', '费用'), time: t('Run time', '耗时'), last: t('Last activity', '最后活动') };
  return <div className="modal-backdrop" onClick={event => { if (event.target === event.currentTarget) close(); }}>
    <section className="modal status-panel" role="dialog" aria-modal="true" aria-label={t('Host status', '服务状态')}>
      <div className="modal-heading"><h2>{t('Host status', '服务状态')}</h2><span className="status-refresh">{data ? t('Refreshes every 5 s', '每 5 秒刷新') : t('Loading…', '读取中…')}</span><button type="button" className="icon" aria-label={t('Close host status', '关闭服务状态')} onClick={close}><CloseIcon size={18} /></button></div>
      {error && <div className="error" role="alert">{error}</div>}
      {data && totals && <>
        {data.listeners.some(x => x.insecure) && <div className="status-insecure" role="alert">{t(`⚠ The host listens over plain HTTP on non-local addresses (${data.listeners.filter(x => x.insecure).map(x => x.address).join(', ')}); the access token and session content travel the network unencrypted. Use it only on a trusted LAN, or access it through an HTTPS reverse proxy / VPN.`, `⚠ 宿主在非本机地址（${data.listeners.filter(x => x.insecure).map(x => x.address).join('、')}）以明文 HTTP 监听，访问令牌和会话内容会在网络中明文传输。仅在可信局域网使用，或通过 HTTPS 反向代理 / VPN 访问。`)}</div>}
        <div className="status-cards">
          <div className="status-card"><small>{L.status}</small><strong><span className="dot idle" />{t('Running', '运行中')}</strong><span>PID {data.pid}</span></div>
          <div className="status-card"><small>{t('Version', '版本')}</small><strong>{data.version || 'dev'}</strong><span>{data.goVersion} · {data.os}/{data.arch}</span></div>
          <div className="status-card"><small>{t('Uptime', '运行时长')}</small><strong>{duration(data.uptimeSeconds)}</strong><span title={data.startedAt}>{data.startedAt ? new Date(data.startedAt).toLocaleString(locale) : '—'}</span></div>
          <div className="status-card"><small>{t('Service', '服务托管')}</small><strong>{data.service.managed ? managerText[data.service.manager] || data.service.manager : t('Foreground / started manually', '前台 / 手动启动')}</strong><span>{data.service.managed ? t('Starts at boot', '开机自动运行') : t('Not registered as a system service', '未注册为系统服务')}</span></div>
          <div className="status-card"><small>Claude Code</small><strong className={data.claude.error ? 'warn' : ''}>{data.claude.version || t('Unavailable', '不可用')}</strong><span title={data.claude.path || data.claude.error}>{data.claude.path || data.claude.error}</span></div>
          {providerStatus?.codexAvailable !== undefined && <div className="status-card"><small>Codex</small><strong className={providerStatus.codexAvailable ? '' : 'warn'}>{providerStatus.codexVersion || (providerStatus.codexAvailable ? t('Available', '可用') : t('Unavailable', '不可用'))}</strong></div>}
          <div className="status-card"><small>{t('Listening on', '监听地址')}</small>{(data.listeners.length ? data.listeners : [{ address: data.listen || '—', loopback: true }]).map(x => <span key={x.address} className="status-listener"><code>{x.address}</code>{!x.loopback && <em>{x.insecure ? t('Reachable from LAN · plain HTTP', '局域网可访问 · 明文 HTTP') : t('Reachable from LAN', '局域网可访问')}</em>}</span>)}</div>
        </div>
        <div className="status-grid">
          <div className="status-block"><h3>{t('Sessions', '会话')}</h3><dl>
            <div><dt>{t('All', '全部')}</dt><dd>{data.sessions.total}</dd></div><div><dt>{t('Managed', '托管')}</dt><dd>{data.sessions.managed}</dd></div><div><dt>{t('Terminal', '终端')}</dt><dd>{data.sessions.terminal}</dd></div>
            <div><dt>{t('Running', '执行中')}</dt><dd>{data.sessions.running}<small>{t(` · processes ${data.activeRuns}`, ` · 进程 ${data.activeRuns}`)}</small></dd></div><div><dt>{t('Waiting for answer', '等待回答')}</dt><dd>{data.sessions.waiting}</dd></div><div><dt>{t('Pending approvals', '待审批')}</dt><dd className={data.pendingRequests ? 'warn' : ''}>{data.pendingRequests}</dd></div>
          </dl></div>
          <div className="status-block"><h3>{t('Totals', '累计')}</h3><dl>
            <div><dt>{L.turns}</dt><dd>{number(totals.turns)}</dd></div><div><dt>{L.cost}</dt><dd>{money(totals)}</dd></div><div><dt>Tokens</dt><dd title={t(`Input ${totals.inputTokens} · output ${totals.outputTokens} · cache read ${totals.cacheReadTokens} · cache write ${totals.cacheCreationTokens}`, `输入 ${totals.inputTokens} · 输出 ${totals.outputTokens} · 缓存读 ${totals.cacheReadTokens} · 缓存写 ${totals.cacheCreationTokens}`)}>{totals.costKnown ? number(tokens(totals)) : '—'}</dd></div>
            <div><dt>{t('Run time', '执行耗时')}</dt><dd>{duration(totals.durationMs / 1000)}</dd></div><div><dt>{t('Tool calls', '工具调用')}</dt><dd>{number(totals.toolCalls)}</dd></div><div><dt>{t('Errors', '错误')}</dt><dd>{totals.errors}</dd></div>
          </dl></div>
          <div className="status-block"><h3>{t('Storage', '存储')}</h3><dl>
            <div><dt>{t('Database', '数据库')}</dt><dd>{bytes(data.storage.databaseBytes)}</dd></div><div><dt>{t('Uploads', '上传附件')}</dt><dd>{bytes(data.storage.uploadsBytes)}</dd></div><div><dt>{t('Logs', '日志')}</dt><dd>{bytes(data.storage.logBytes)}</dd></div>
          </dl><small className="status-path" title={data.dataDir}>{data.dataDir}</small></div>
        </div>
        <UsageDetails api={api} />
        <Chart daily={data.daily} />
        <div className="status-table-wrap"><table className="status-table">
          <thead><tr>{header('title', L.title)}{header('mode', L.mode)}{header('status', L.status)}{header('turns', L.turns)}{header('tokens', 'Tokens')}{header('costUSD', L.cost)}{header('durationMs', L.time)}{header('lastActivityAt', L.last)}</tr></thead>
          <tbody>{rows.length === 0 ? <tr><td colSpan={8} className="muted">{t('No sessions yet', '暂无会话')}</td></tr> : rows.map(x => <tr key={x.id} tabIndex={0} title={x.cwd} onClick={() => select(x.id)} onKeyDown={event => { if (event.key === 'Enter') select(x.id); }}>
            <td data-label={L.title}><strong>{x.title || t('Untitled session', '未命名会话')}</strong></td>
            <td data-label={L.mode}>{x.mode === 'terminal' ? t('Terminal', '终端') : t('Managed', '托管')}</td>
            <td data-label={L.status}><span className={`dot ${x.status}`} />{statusText[x.status] || x.status}</td>
            <td data-label={L.turns}>{x.turns}</td>
            <td data-label="Tokens">{x.costKnown ? number(tokens(x)) : '—'}</td>
            <td data-label={L.cost}>{money(x)}</td>
            <td data-label={L.time}>{x.durationMs ? duration(x.durationMs / 1000) : '—'}</td>
            <td data-label={L.last} title={x.lastActivityAt}>{ago(x.lastActivityAt)}</td>
          </tr>)}</tbody>
        </table></div>
        <small className="status-note">{t('Cost and tokens come from the Claude result events of managed sessions; terminal sessions have no usage data yet and show "—".', '费用与 tokens 来自托管会话的 Claude 结果事件；终端会话暂无用量数据，显示为「—」。')}</small>
        <details className="status-log" open={logOpen} onToggle={event => setLogOpen(event.currentTarget.open)}>
          <summary>{t('Log', '日志')} <small>{data.logFile || t('No log file configured', '未配置日志文件')}{t(` · last ${data.logTail.length} lines`, ` · 最近 ${data.logTail.length} 行`)}</small></summary>
          <pre ref={log}>{data.logTail.length ? data.logTail.join('\n') : t('No log entries', '暂无日志')}</pre>
        </details>
      </>}
    </section>
  </div>;
}

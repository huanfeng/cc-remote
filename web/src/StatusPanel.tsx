import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { Api } from './ClaudeSettings';
import './status.css';
import { CloseIcon } from './icons';

export type Stats = { sessionId: string; turns: number; results: number; costUSD: number; costKnown: boolean; inputTokens: number; outputTokens: number; cacheReadTokens: number; cacheCreationTokens: number; durationMs: number; toolCalls: number; errors: number; lastActivityAt: string };
type SessionRow = Stats & { id: string; title: string; mode: string; status: string; online: boolean; cwd: string };
type Daily = { date: string; turns: number; costUSD: number; tokens: number };
export type HostStatus = {
  version: string; pid: number; startedAt: string; uptimeSeconds: number; listen: string; listeners: { address: string; loopback: boolean; insecure?: boolean }[];
  dataDir: string; claudeConfigDir: string; logFile: string; goVersion: string; os: string; arch: string;
  claude: { executable: string; path: string; version: string; error?: string }; service: { managed: boolean; manager: string };
  sessions: Record<'total' | 'managed' | 'channel' | 'running' | 'waiting' | 'idle' | 'online', number>;
  pendingRequests: number; activeRuns: number; storage: { databaseBytes: number; uploadsBytes: number; logBytes: number };
  totals: Stats; daily: Daily[]; sessionStats: SessionRow[]; logTail: string[];
};
type SortKey = 'title' | 'mode' | 'status' | 'turns' | 'tokens' | 'costUSD' | 'durationMs' | 'lastActivityAt';

const statusText: Record<string, string> = { running: '执行中', waiting: '等待回答', idle: '就绪', completed: '已完成', interrupted: '已停止', failed: '执行失败', offline: '离线' };
const managerText: Record<string, string> = { schtasks: 'Windows 计划任务', systemd: 'systemd 用户服务' };
export const tokens = (x: Stats) => x.inputTokens + x.outputTokens + x.cacheReadTokens + x.cacheCreationTokens;
export const number = (n: number) => n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1e4 ? `${(n / 1e3).toFixed(1)}k` : n.toLocaleString('zh-CN');
export const money = (x: Pick<Stats, 'costUSD' | 'costKnown'>) => x.costKnown ? `$${x.costUSD.toFixed(x.costUSD >= 10 ? 2 : 4)}` : '—';
const bytes = (n: number) => n >= 1 << 30 ? `${(n / (1 << 30)).toFixed(1)} GiB` : n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MiB` : n >= 1024 ? `${(n / 1024).toFixed(1)} KiB` : `${n} B`;
export function duration(seconds: number) {
  const d = Math.floor(seconds / 86400), h = Math.floor(seconds % 86400 / 3600), m = Math.floor(seconds % 3600 / 60);
  return d ? `${d} 天 ${h} 小时` : h ? `${h} 小时 ${m} 分` : m ? `${m} 分 ${Math.floor(seconds % 60)} 秒` : `${Math.floor(seconds)} 秒`;
}
function ago(iso: string) {
  const t = Date.parse(iso); if (!t) return '—';
  const s = Math.max(0, (Date.now() - t) / 1000);
  return s < 60 ? '刚刚' : s < 3600 ? `${Math.floor(s / 60)} 分钟前` : s < 86400 ? `${Math.floor(s / 3600)} 小时前` : new Date(t).toLocaleDateString('zh-CN');
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
    <figcaption><span><i className="swatch turns" />轮次</span>{maxCost > 0 && <span><i className="swatch cost" />费用</span>}<small>近 14 天（UTC）</small></figcaption>
    <svg viewBox={`0 0 ${w} ${h}`} role="img" aria-label="近 14 天轮次与费用">
      <line x1={pad} x2={w - pad} y1={h - pad} y2={h - pad} className="axis" />
      {daily.map((x, i) => <g key={x.date}>
        <title>{`${x.date}：${x.turns} 轮 · $${x.costUSD.toFixed(4)} · ${number(x.tokens)} tokens`}</title>
        <rect x={pad + bw * i + bw * .18} width={bw * .64} y={y(x.turns, maxTurns)} height={Math.max(0, h - pad - y(x.turns, maxTurns))} rx="3" className="bar" />
        {(w > 520 || i % 2 === (daily.length - 1) % 2) && <text x={pad + bw * i + bw / 2} y={h - 6} textAnchor="middle">{x.date.slice(5)}</text>}
      </g>)}
      {maxCost > 0 && <><polyline points={line} className="cost-line" />{daily.map((x, i) => <circle key={x.date} cx={pad + bw * i + bw / 2} cy={y(x.costUSD, maxCost)} r="2.5" className="cost-dot" />)}</>}
      <text x={pad} y={12} className="scale">{maxTurns} 轮</text>
      {maxCost > 0 && <text x={w - pad} y={12} textAnchor="end" className="scale">${maxCost.toFixed(2)}</text>}
    </svg>
  </figure>;
}

export function StatusPanel({ api, close, select }: { api: Api; close: () => void; select: (id: string) => void }) {
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
  const rows = [...data?.sessionStats || []].sort((a, b) => { const x = value(a), y = value(b); const c = typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y), 'zh-CN'); return sort.desc ? -c : c; });
  const header = (key: SortKey, label: string) => <th aria-sort={sort.key === key ? sort.desc ? 'descending' : 'ascending' : undefined}><button onClick={() => setSort(old => ({ key, desc: old.key === key ? !old.desc : key !== 'title' && key !== 'mode' && key !== 'status' }))}>{label}{sort.key === key ? sort.desc ? ' ↓' : ' ↑' : ''}</button></th>;
  const t = data?.totals;
  return <div className="modal-backdrop" onClick={event => { if (event.target === event.currentTarget) close(); }}>
    <section className="modal status-panel" role="dialog" aria-modal="true" aria-label="服务状态">
      <div className="modal-heading"><h2>服务状态</h2><span className="status-refresh">{data ? '每 5 秒刷新' : '读取中…'}</span><button type="button" className="icon" aria-label="关闭服务状态" onClick={close}><CloseIcon size={18} /></button></div>
      {error && <div className="error" role="alert">{error}</div>}
      {data && t && <>
        {data.listeners.some(x => x.insecure) && <div className="status-insecure" role="alert">⚠ 宿主在非本机地址（{data.listeners.filter(x => x.insecure).map(x => x.address).join('、')}）以明文 HTTP 监听，访问令牌和会话内容会在网络中明文传输。仅在可信局域网使用，或通过 HTTPS 反向代理 / VPN 访问。</div>}
        <div className="status-cards">
          <div className="status-card"><small>状态</small><strong><span className="dot idle" />运行中</strong><span>PID {data.pid}</span></div>
          <div className="status-card"><small>版本</small><strong>{data.version || 'dev'}</strong><span>{data.goVersion} · {data.os}/{data.arch}</span></div>
          <div className="status-card"><small>运行时长</small><strong>{duration(data.uptimeSeconds)}</strong><span title={data.startedAt}>{data.startedAt ? new Date(data.startedAt).toLocaleString('zh-CN') : '—'}</span></div>
          <div className="status-card"><small>服务托管</small><strong>{data.service.managed ? managerText[data.service.manager] || data.service.manager : '前台 / 手动启动'}</strong><span>{data.service.managed ? '开机自动运行' : '未注册为系统服务'}</span></div>
          <div className="status-card"><small>Claude Code</small><strong className={data.claude.error ? 'warn' : ''}>{data.claude.version || '不可用'}</strong><span title={data.claude.path || data.claude.error}>{data.claude.path || data.claude.error}</span></div>
          <div className="status-card"><small>监听地址</small>{(data.listeners.length ? data.listeners : [{ address: data.listen || '—', loopback: true }]).map(x => <span key={x.address} className="status-listener"><code>{x.address}</code>{!x.loopback && <em>{x.insecure ? '局域网可访问 · 明文 HTTP' : '局域网可访问'}</em>}</span>)}</div>
        </div>
        <div className="status-grid">
          <div className="status-block"><h3>会话</h3><dl>
            <div><dt>全部</dt><dd>{data.sessions.total}</dd></div><div><dt>托管</dt><dd>{data.sessions.managed}</dd></div><div><dt>共享终端</dt><dd>{data.sessions.channel}<small> · 在线 {data.sessions.online}</small></dd></div>
            <div><dt>执行中</dt><dd>{data.sessions.running}<small> · 进程 {data.activeRuns}</small></dd></div><div><dt>等待回答</dt><dd>{data.sessions.waiting}</dd></div><div><dt>待审批</dt><dd className={data.pendingRequests ? 'warn' : ''}>{data.pendingRequests}</dd></div>
          </dl></div>
          <div className="status-block"><h3>累计</h3><dl>
            <div><dt>轮次</dt><dd>{number(t.turns)}</dd></div><div><dt>费用</dt><dd>{money(t)}</dd></div><div><dt>Tokens</dt><dd title={`输入 ${t.inputTokens} · 输出 ${t.outputTokens} · 缓存读 ${t.cacheReadTokens} · 缓存写 ${t.cacheCreationTokens}`}>{t.costKnown ? number(tokens(t)) : '—'}</dd></div>
            <div><dt>执行耗时</dt><dd>{duration(t.durationMs / 1000)}</dd></div><div><dt>工具调用</dt><dd>{number(t.toolCalls)}</dd></div><div><dt>错误</dt><dd>{t.errors}</dd></div>
          </dl></div>
          <div className="status-block"><h3>存储</h3><dl>
            <div><dt>数据库</dt><dd>{bytes(data.storage.databaseBytes)}</dd></div><div><dt>上传附件</dt><dd>{bytes(data.storage.uploadsBytes)}</dd></div><div><dt>日志</dt><dd>{bytes(data.storage.logBytes)}</dd></div>
          </dl><small className="status-path" title={data.dataDir}>{data.dataDir}</small></div>
        </div>
        <Chart daily={data.daily} />
        <div className="status-table-wrap"><table className="status-table">
          <thead><tr>{header('title', '标题')}{header('mode', '模式')}{header('status', '状态')}{header('turns', '轮次')}{header('tokens', 'Tokens')}{header('costUSD', '费用')}{header('durationMs', '耗时')}{header('lastActivityAt', '最后活动')}</tr></thead>
          <tbody>{rows.length === 0 ? <tr><td colSpan={8} className="muted">暂无会话</td></tr> : rows.map(x => <tr key={x.id} tabIndex={0} title={x.cwd} onClick={() => select(x.id)} onKeyDown={event => { if (event.key === 'Enter') select(x.id); }}>
            <td data-label="标题"><strong>{x.title || '未命名会话'}</strong></td>
            <td data-label="模式">{x.mode === 'channel' ? '共享' : x.mode === 'terminal' ? '终端' : '托管'}</td>
            <td data-label="状态"><span className={`dot ${x.status}`} />{statusText[x.status] || x.status}</td>
            <td data-label="轮次">{x.turns}</td>
            <td data-label="Tokens">{x.costKnown ? number(tokens(x)) : '—'}</td>
            <td data-label="费用">{money(x)}</td>
            <td data-label="耗时">{x.durationMs ? duration(x.durationMs / 1000) : '—'}</td>
            <td data-label="最后活动" title={x.lastActivityAt}>{ago(x.lastActivityAt)}</td>
          </tr>)}</tbody>
        </table></div>
        <small className="status-note">费用与 tokens 来自托管会话的 Claude 结果事件；共享终端会话暂无用量数据，显示为「—」。</small>
        <details className="status-log" open={logOpen} onToggle={event => setLogOpen(event.currentTarget.open)}>
          <summary>日志 <small>{data.logFile || '未配置日志文件'} · 最近 {data.logTail.length} 行</small></summary>
          <pre ref={log}>{data.logTail.length ? data.logTail.join('\n') : '暂无日志'}</pre>
        </details>
      </>}
    </section>
  </div>;
}

import { useEffect, useRef, useState } from 'react';
import { CloseIcon } from './icons';
import { ModelPicker, confirmMode, dangerousMode, effortOptions, permissionOptions, type Api } from './ModelPicker';
import { Select } from './Select';
import { duration, money, number, tokens, type Stats } from './StatusPanel';

export type SessionInfo = { id: string; title: string; cwd: string; status?: string; mode?: string; online?: boolean; claudeSessionId?: string; createdAt?: string; permissionMode?: string; model?: string; effort?: string; live?: LiveStatus };
// What Claude Code reports to a terminal session's status line.
export type LiveStatus = { model?: string; modelId?: string; effort?: string; costUSD: number; durationMs: number; contextTokens: number; contextSize: number; contextPercent: number; activity?: string };
const compact = (n: number) => n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1000 ? `${Math.round(n / 1000)}k` : String(n);

export type Defaults = { permissionMode: string; model: string; effort: string };

const CHANNEL_NOTE = '共享终端：模型、推理强度与权限模式由运行中的终端进程持有，网页无法切换。请在终端执行 /model、/effort，或按 Shift+Tab 切换权限模式。';
const permTone = (mode: string) => mode === 'bypassPermissions' ? ' danger' : dangerousMode(mode) ? ' warn' : '';

/** Closes a dialog on Escape; the latest callback is used without re-subscribing. */
function useEscape(close: () => void) {
  const ref = useRef(close); ref.current = close;
  useEffect(() => {
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') ref.current(); };
    document.addEventListener('keydown', key);
    return () => document.removeEventListener('keydown', key);
  }, []);
}

/** Read-only effective settings in the top bar: override, or 默认(<inherited>). */
export function SessionChips({ session, defaults, open }: { session: SessionInfo; defaults?: Defaults; open: () => void }) {
  if (session.mode === 'channel') return <button type="button" className="session-chips" title="会话设置" onClick={open}><span className="chip inherited">设置由终端持有</span></button>;
  const chip = (label: string, value: string | undefined, inherited: string | undefined, tone = '') => {
    const shown = value || `默认(${inherited || 'Claude 配置'})`;
    return <span className={`chip${value ? '' : ' inherited'}${tone}`} title={`${label}：${shown}`}>{shown}</span>;
  };
  const live = session.mode === 'terminal' ? session.live : undefined;
  if (live?.model) {
    const effort = live.effort || session.effort || defaults?.effort;
    const pct = Math.round(live.contextPercent);
    return <button type="button" className="session-chips" title="会话设置" aria-label="打开会话设置" onClick={open}>
      <span className="chip" title={`模型：${live.model}${live.modelId ? `（${live.modelId}）` : ''}`}>{live.model}</span>
      {effort && <span className="chip" title={`推理强度：${effort}`}>{effort}</span>}
      {live.contextSize > 0 && <span className={`chip${pct >= 80 ? ' danger' : pct >= 60 ? ' warn' : ''}`} title={`上下文：${live.contextTokens.toLocaleString()} / ${live.contextSize.toLocaleString()} tokens`}>上下文 {pct}% · {compact(live.contextTokens)}/{compact(live.contextSize)}</span>}
      {live.durationMs > 0 && <span className="chip inherited" title="会话时长">{duration(live.durationMs / 1000)}</span>}
      {live.costUSD > 0 && <span className="chip inherited" title="本次运行的费用（按 API 价格估算）">${live.costUSD.toFixed(2)}</span>}
    </button>;
  }
  const perm = session.permissionMode || defaults?.permissionMode || '';
  return <button type="button" className="session-chips" title="会话设置（模型 · 推理强度 · 权限模式）" aria-label="打开会话设置" onClick={open}>
    {chip('模型', session.model, defaults?.model)}{chip('推理强度', session.effort, defaults?.effort)}{chip('权限模式', session.permissionMode, defaults?.permissionMode, permTone(perm))}
  </button>;
}

export function SessionSettings<T extends SessionInfo>({ api, session, defaults, close, onUpdated, onDelete }: { api: Api; session: T; defaults?: Defaults; close: () => void; onUpdated: (session: T) => void; onDelete: () => void }) {
  const channel = session.mode === 'channel';
  const saved = { permissionMode: session.permissionMode || '', model: session.model || '', effort: session.effort || '' };
  const [draft, setDraft] = useState(saved);
  const [stats, setStats] = useState<Stats>();
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  useEscape(close);
  useEffect(() => {
    let active = true;
    api<Stats>(`/api/sessions/${encodeURIComponent(session.id)}/stats`).then(data => { if (active) setStats(data); }).catch(() => {});
    return () => { active = false; };
  }, [api, session.id]);
  const changed = draft.permissionMode !== saved.permissionMode || draft.model.trim() !== saved.model || draft.effort !== saved.effort;
  const effective = draft.permissionMode || defaults?.permissionMode || '';
  const inherit = (value?: string) => `跟随默认（${value || 'Claude 配置'}）`;
  async function save() {
    if (!confirmMode(draft.permissionMode, saved.permissionMode)) return;
    setSaving(true); setError('');
    try {
      onUpdated(await api<T>(`/api/sessions/${encodeURIComponent(session.id)}`, { method: 'PATCH', body: JSON.stringify({ ...draft, model: draft.model.trim(), confirmDangerous: dangerousMode(draft.permissionMode) }) }));
      close();
    } catch (err) { setError((err as Error).message); } finally { setSaving(false); }
  }
  const created = session.createdAt ? new Date(session.createdAt).toLocaleString('zh-CN') : '—';
  return <div className="modal-backdrop" onClick={event => { if (event.target === event.currentTarget) close(); }}>
    <form className="modal session-settings" role="dialog" aria-modal="true" aria-label="会话设置" onSubmit={event => { event.preventDefault(); if (!channel && changed) void save(); }}>
      <div className="modal-heading"><h2>会话设置</h2><button type="button" className="icon" aria-label="关闭会话设置" onClick={close}><CloseIcon size={18} /></button></div>
      <p className="session-settings-title" title={session.title}>{session.title || '未命名会话'}</p>
      <h3>启动参数</h3>
      {channel ? <p className="channel-note">{CHANNEL_NOTE}</p> : <p>仅作用于此会话，下一轮执行生效；留空跟随「设置」中的 CC Remote 默认值。</p>}
      <fieldset disabled={channel || saving}>
        <label>模型<ModelPicker api={api} sessionId={session.id} label="会话模型" inheritLabel={inherit(defaults?.model)} value={draft.model} onChange={model => setDraft(old => ({ ...old, model }))} /></label>
        <div className="new-session-overrides">
          <label>推理强度<Select label="会话推理强度" disabled={channel || saving} value={draft.effort} options={effortOptions(inherit(defaults?.effort))} onChange={effort => setDraft(old => ({ ...old, effort }))} /></label>
          <label>权限模式<Select label="会话权限模式" disabled={channel || saving} value={draft.permissionMode} options={permissionOptions(inherit(defaults?.permissionMode), draft.permissionMode)} onChange={permissionMode => setDraft(old => ({ ...old, permissionMode }))} /></label>
        </div>
        {!channel && <p className="effective-line">生效权限模式：<span className={`chip${permTone(effective)}`}>{effective || 'Claude 配置'}</span>{effective === 'bypassPermissions' && <small className="danger-note">跳过所有审批，仅在可信项目中使用。</small>}</p>}
      </fieldset>
      <h3>会话信息</h3>
      <dl className="session-info">
        <div><dt>模式</dt><dd>{channel ? `共享终端 · ${session.online ? '在线' : '离线'}` : '托管'}</dd></div>
        <div><dt>创建时间</dt><dd>{created}</dd></div>
        <div className="wide"><dt>项目目录</dt><dd><code>{session.cwd}</code></dd></div>
        <div className="wide"><dt>会话 ID</dt><dd><code>{session.id}</code></dd></div>
        <div className="wide"><dt>Claude 会话 ID</dt><dd><code>{session.claudeSessionId || '—'}</code></dd></div>
        <div><dt>轮次</dt><dd>{stats ? number(stats.turns) : '…'}</dd></div>
        <div><dt>Tokens</dt><dd>{stats ? stats.costKnown ? number(tokens(stats)) : '—' : '…'}</dd></div>
        <div><dt>费用</dt><dd>{stats ? money(stats) : '…'}</dd></div>
        <div><dt>执行耗时</dt><dd>{stats ? stats.durationMs ? duration(stats.durationMs / 1000) : '—' : '…'}</dd></div>
      </dl>
      <section className="danger-zone">
        <div><strong>删除会话</strong><small>只删除 CC Remote 中的记录和上传附件；Claude Code 原生历史保留。</small></div>
        <button type="button" className="danger-button" onClick={onDelete}>删除…</button>
      </section>
      {error && <div className="error" role="alert">{error}</div>}
      <div className="modal-actions">
        {!channel && <button type="button" className="secondary reset" disabled={saving || (!draft.permissionMode && !draft.model.trim() && !draft.effort)} onClick={() => setDraft({ permissionMode: '', model: '', effort: '' })}>全部恢复默认</button>}
        <button type="button" className="secondary" onClick={close}>取消</button>
        {!channel && <button className="primary" disabled={saving || !changed}>{saving ? '保存中…' : '保存'}</button>}
      </div>
    </form>
  </div>;
}

export function DeleteSessionDialog({ session, close, confirm }: { session: SessionInfo; close: () => void; confirm: (stop: boolean) => Promise<void> }) {
  const running = session.mode !== 'channel' && ['running', 'waiting'].includes(session.status || '');
  const online = session.mode === 'channel' && !!session.online;
  const [stop, setStop] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEscape(() => { if (!busy) close(); });
  async function submit() {
    setBusy(true); setError('');
    try { await confirm(running && stop); } catch (err) { setError((err as Error).message); setBusy(false); }
  }
  return <div className="modal-backdrop confirm-backdrop" onClick={event => { if (event.target === event.currentTarget && !busy) close(); }}>
    <div className="modal confirm-modal" role="alertdialog" aria-modal="true" aria-labelledby="delete-title" aria-describedby="delete-detail">
      <div className="modal-heading"><h2 id="delete-title">删除会话？</h2><button type="button" className="icon" aria-label="取消删除" disabled={busy} onClick={close}><CloseIcon size={18} /></button></div>
      <p className="confirm-target" title={session.cwd}><strong>{session.title || '未命名会话'}</strong><small>{session.cwd}</small></p>
      <div id="delete-detail">
        <p>将永久删除 CC Remote 中此会话的：</p>
        <ul><li>网页消息与工具事件记录</li><li>审批 / 提问记录</li><li>共享终端同步记录</li><li>上传到宿主的图片和文件</li></ul>
        <p>Claude Code 原生会话历史（~/.claude 下的 JSONL）<strong>不会删除</strong>，仍可在「终端历史」中查看或恢复。此操作无法撤销。</p>
      </div>
      {online && <div className="error" role="alert">共享终端在线，请先退出终端中的 Claude Code 再删除。</div>}
      {running && <label className="checkbox"><input type="checkbox" checked={stop} onChange={event => setStop(event.target.checked)} />此会话正在执行，先停止当前执行再删除</label>}
      {error && <div className="error" role="alert">{error}</div>}
      <div className="modal-actions"><button type="button" className="secondary" disabled={busy} onClick={close}>取消</button><button type="button" className="danger-button solid" disabled={busy || online || (running && !stop)} onClick={() => void submit()}>{busy ? '删除中…' : running ? '停止并删除' : '删除会话'}</button></div>
    </div>
  </div>;
}

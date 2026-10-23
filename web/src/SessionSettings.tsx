import { useEffect, useRef, useState, type ReactNode } from 'react';
import { ClockIcon, CloseIcon, GaugeIcon, ShieldIcon, SparkIcon } from './icons';
import { ModelPicker, confirmMode, dangerousMode, effortOptions, permissionOptions, type Api } from './ModelPicker';
import { Select } from './Select';
import { duration, money, number, tokens, type Stats } from './StatusPanel';
import { locale, t } from './i18n';

export type SessionInfo = { id: string; title: string; cwd: string; status?: string; mode?: string; online?: boolean; claudeSessionId?: string; createdAt?: string; permissionMode?: string; model?: string; effort?: string; live?: LiveStatus };
// What Claude Code reports to a terminal session's status line.
export type LiveStatus = { model?: string; modelId?: string; effort?: string; costUSD: number; durationMs: number; contextTokens: number; contextSize: number; contextPercent: number; activity?: string; suggestion?: string };
const compact = (n: number) => n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1000 ? `${Math.round(n / 1000)}k` : String(n);

export type Defaults = { permissionMode: string; model: string; effort: string };

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

/** How full the context window is, as a ring that fills clockwise from the top. */
export function ContextRing({ percent, size = 12 }: { percent: number; size?: number }) {
  const r = 5, around = 2 * Math.PI * r;
  const filled = Math.max(0, Math.min(100, percent)) / 100 * around;
  return <svg className="context-ring" viewBox="0 0 14 14" width={size} height={size} aria-hidden="true" focusable="false">
    <circle cx="7" cy="7" r={r} fill="none" stroke="currentColor" strokeOpacity=".22" strokeWidth="2.2" />
    {filled > 0 && <circle cx="7" cy="7" r={r} fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeDasharray={`${filled} ${around}`} transform="rotate(-90 7 7)" />}
  </svg>;
}

/** Effective settings in the top bar: override, or Default(<inherited>). Any chip opens
 * the session status (open), which has the full values, the switches and the settings. */
export function SessionChips({ session, defaults, open }: { session: SessionInfo; defaults?: Defaults; open: (anchor: DOMRect) => void }) {
  const show = (event: { currentTarget: Element }) => open(event.currentTarget.getBoundingClientRect());
  const button = (className: string, title: string, text: ReactNode, icon?: ReactNode) => <button type="button" className={className} title={title} aria-haspopup="dialog" onClick={show}>{icon}<span>{text}</span></button>;
  const chip = (label: string, value: string | undefined, inherited: string | undefined, icon: ReactNode, tone = '') => {
    const shown = value || t(`Default(${inherited || 'Claude settings'})`, `默认(${inherited || 'Claude 配置'})`);
    return button(`chip${value ? '' : ' inherited'}${tone}`, t(`${label}: ${shown}`, `${label}：${shown}`), shown, icon);
  };
  const live = session.mode === 'terminal' ? session.live : undefined;
  if (live?.model) {
    const effort = live.effort || session.effort || defaults?.effort;
    const pct = Math.round(live.contextPercent);
    return <div className="session-chips">
      {button('chip', t(`Model: ${live.model}${live.modelId ? ` (${live.modelId})` : ''}`, `模型：${live.model}${live.modelId ? `（${live.modelId}）` : ''}`), live.model, <SparkIcon size={11} />)}
      {effort && button('chip', t(`Effort: ${effort}`, `推理强度：${effort}`), effort, <GaugeIcon size={11} />)}
      {live.contextSize > 0 && button(`chip context-chip${pct >= 80 ? ' danger' : pct >= 60 ? ' warn' : ''}`, t(`Context: ${pct}% · ${live.contextTokens.toLocaleString(locale)} / ${live.contextSize.toLocaleString(locale)} tokens`, `上下文：${pct}% · ${live.contextTokens.toLocaleString(locale)} / ${live.contextSize.toLocaleString(locale)} tokens`), `${pct}%`, <ContextRing percent={pct} />)}
      {live.durationMs > 0 && button('chip inherited', t('Session duration', '会话时长'), duration(live.durationMs / 1000), <ClockIcon size={11} />)}
      {live.costUSD > 0 && button('chip inherited', t('Cost of this run (estimated at API prices)', '本次运行的费用（按 API 价格估算）'), `$${live.costUSD.toFixed(2)}`)}
    </div>;
  }
  const perm = session.permissionMode || defaults?.permissionMode || '';
  return <div className="session-chips">
    {chip(t('Model', '模型'), session.model, defaults?.model, <SparkIcon size={11} />)}{chip(t('Effort', '推理强度'), session.effort, defaults?.effort, <GaugeIcon size={11} />)}{chip(t('Permission mode', '权限模式'), session.permissionMode, defaults?.permissionMode, <ShieldIcon size={11} />, permTone(perm))}
  </div>;
}

export function SessionSettings<T extends SessionInfo>({ api, session, defaults, close, onUpdated, onDelete }: { api: Api; session: T; defaults?: Defaults; close: () => void; onUpdated: (session: T) => void; onDelete: () => void }) {
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
  const inherit = (value?: string) => t(`Use default (${value || 'Claude settings'})`, `跟随默认（${value || 'Claude 配置'}）`);
  async function save() {
    if (!confirmMode(draft.permissionMode, saved.permissionMode)) return;
    setSaving(true); setError('');
    try {
      onUpdated(await api<T>(`/api/sessions/${encodeURIComponent(session.id)}`, { method: 'PATCH', body: JSON.stringify({ ...draft, model: draft.model.trim(), confirmDangerous: dangerousMode(draft.permissionMode) }) }));
      close();
    } catch (err) { setError((err as Error).message); } finally { setSaving(false); }
  }
  const created = session.createdAt ? new Date(session.createdAt).toLocaleString(locale) : '—';
  const mode = session.mode === 'terminal' ? t('Terminal', '终端') : t('Managed', '托管');
  return <div className="modal-backdrop" onClick={event => { if (event.target === event.currentTarget) close(); }}>
    <form className="modal session-settings" role="dialog" aria-modal="true" aria-label={t('Session settings', '会话设置')} onSubmit={event => { event.preventDefault(); if (changed) void save(); }}>
      <div className="modal-heading"><h2>{t('Session settings', '会话设置')}</h2><button type="button" className="icon" aria-label={t('Close session settings', '关闭会话设置')} onClick={close}><CloseIcon size={18} /></button></div>
      <p className="session-settings-title" title={session.title}>{session.title || t('Untitled session', '未命名会话')}</p>
      <h3>{t('Launch settings', '启动参数')}</h3>
      <p>{t('Applies to this session only, from its next turn; leave empty to use the CC Remote defaults in Settings.', '仅作用于此会话，下一轮执行生效；留空跟随「设置」中的 CC Remote 默认值。')}</p>
      <fieldset disabled={saving}>
        <label>{t('Model', '模型')}<ModelPicker api={api} sessionId={session.id} label={t('Session model', '会话模型')} inheritLabel={inherit(defaults?.model)} value={draft.model} onChange={model => setDraft(old => ({ ...old, model }))} /></label>
        <div className="new-session-overrides">
          <label>{t('Effort', '推理强度')}<Select label={t('Session effort', '会话推理强度')} disabled={saving} value={draft.effort} options={effortOptions(inherit(defaults?.effort))} onChange={effort => setDraft(old => ({ ...old, effort }))} /></label>
          <label>{t('Permission mode', '权限模式')}<Select label={t('Session permission mode', '会话权限模式')} disabled={saving} value={draft.permissionMode} options={permissionOptions(inherit(defaults?.permissionMode), draft.permissionMode)} onChange={permissionMode => setDraft(old => ({ ...old, permissionMode }))} /></label>
        </div>
        {<p className="effective-line">{t('Effective permission mode: ', '生效权限模式：')}<span className={`chip${permTone(effective)}`}>{effective || t('Claude settings', 'Claude 配置')}</span>{effective === 'bypassPermissions' && <small className="danger-note">{t('Skips all approvals; use it only in trusted projects.', '跳过所有审批，仅在可信项目中使用。')}</small>}</p>}
      </fieldset>
      <h3>{t('Session info', '会话信息')}</h3>
      <dl className="session-info">
        <div><dt>{t('Mode', '模式')}</dt><dd>{mode}</dd></div>
        <div><dt>{t('Created', '创建时间')}</dt><dd>{created}</dd></div>
        <div className="wide"><dt>{t('Project directory', '项目目录')}</dt><dd><code>{session.cwd}</code></dd></div>
        <div className="wide"><dt>{t('Session ID', '会话 ID')}</dt><dd><code>{session.id}</code></dd></div>
        <div className="wide"><dt>{t('Claude session ID', 'Claude 会话 ID')}</dt><dd><code>{session.claudeSessionId || '—'}</code></dd></div>
        <div><dt>{t('Turns', '轮次')}</dt><dd>{stats ? number(stats.turns) : '…'}</dd></div>
        <div><dt>Tokens</dt><dd>{stats ? stats.costKnown ? number(tokens(stats)) : '—' : '…'}</dd></div>
        <div><dt>{t('Cost', '费用')}</dt><dd>{stats ? money(stats) : '…'}</dd></div>
        <div><dt>{t('Run time', '执行耗时')}</dt><dd>{stats ? stats.durationMs ? duration(stats.durationMs / 1000) : '—' : '…'}</dd></div>
      </dl>
      <section className="danger-zone">
        <div><strong>{t('Delete session', '删除会话')}</strong><small>{t('Deletes only the CC Remote records and uploaded attachments; Claude Code\'s native history is kept.', '只删除 CC Remote 中的记录和上传附件；Claude Code 原生历史保留。')}</small></div>
        <button type="button" className="danger-button" onClick={onDelete}>{t('Delete…', '删除…')}</button>
      </section>
      {error && <div className="error" role="alert">{error}</div>}
      <div className="modal-actions">
        {<button type="button" className="secondary reset" disabled={saving || (!draft.permissionMode && !draft.model.trim() && !draft.effort)} onClick={() => setDraft({ permissionMode: '', model: '', effort: '' })}>{t('Reset all to defaults', '全部恢复默认')}</button>}
        <button type="button" className="secondary" onClick={close}>{t('Cancel', '取消')}</button>
        {<button className="primary" disabled={saving || !changed}>{saving ? t('Saving…', '保存中…') : t('Save', '保存')}</button>}
      </div>
    </form>
  </div>;
}

export function DeleteSessionDialog({ session, close, confirm }: { session: SessionInfo; close: () => void; confirm: (stop: boolean) => Promise<void> }) {
  const running = ['running', 'waiting'].includes(session.status || '');
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
      <div className="modal-heading"><h2 id="delete-title">{t('Delete session?', '删除会话？')}</h2><button type="button" className="icon" aria-label={t('Cancel deletion', '取消删除')} disabled={busy} onClick={close}><CloseIcon size={18} /></button></div>
      <p className="confirm-target" title={session.cwd}><strong>{session.title || t('Untitled session', '未命名会话')}</strong><small>{session.cwd}</small></p>
      <div id="delete-detail">
        <p>{t('This permanently deletes, for this session in CC Remote:', '将永久删除 CC Remote 中此会话的：')}</p>
        <ul><li>{t('Web messages and tool event records', '网页消息与工具事件记录')}</li><li>{t('Approval / question records', '审批 / 提问记录')}</li><li>{t('Terminal transcript sync records', '终端记录同步标记')}</li><li>{t('Images and files uploaded to the host', '上传到宿主的图片和文件')}</li></ul>
        <p>{t('Claude Code\'s native session history (the JSONL under ~/.claude) ', 'Claude Code 原生会话历史（~/.claude 下的 JSONL）')}<strong>{t('is not deleted', '不会删除')}</strong>{t(' and can still be viewed or resumed under "Terminal history". This cannot be undone.', '，仍可在「终端历史」中查看或恢复。此操作无法撤销。')}</p>
      </div>
      {running && <label className="checkbox"><input type="checkbox" checked={stop} onChange={event => setStop(event.target.checked)} />{t('This session is running; stop the current run before deleting', '此会话正在执行，先停止当前执行再删除')}</label>}
      {error && <div className="error" role="alert">{error}</div>}
      <div className="modal-actions"><button type="button" className="secondary" disabled={busy} onClick={close}>{t('Cancel', '取消')}</button><button type="button" className="danger-button solid" disabled={busy || (running && !stop)} onClick={() => void submit()}>{busy ? t('Deleting…', '删除中…') : running ? t('Stop and delete', '停止并删除') : t('Delete session', '删除会话')}</button></div>
    </div>
  </div>;
}

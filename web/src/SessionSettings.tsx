import { providerName, providerOf, type Provider } from './providers';
import { readCodexUsage, type CodexEvent } from './codexStatus';
import { CodexSessionUsage } from './CodexStatusEvent';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { ClockIcon, CloseIcon, GaugeIcon, ShieldIcon, SparkIcon } from './icons';
import { ModelPicker, confirmMode, dangerousMode, effortOptions, permissionOptions, type Api } from './ModelPicker';
import { Select } from './Select';
import { SessionProfile } from './SupplierProfile';
import { duration, money, number, type Stats } from './StatusPanel';
import { locale, t } from './i18n';
import { copyText } from './clipboard';
import './session-details.css';

export type SessionInfo = { id: string; provider?: Provider; profileId?: string; profileName?: string; title: string; cwd: string; status?: string; mode?: string; online?: boolean; claudeSessionId?: string; createdAt?: string; lastConversationAt?: string; conversationTimeEstimated?: boolean; queued?: { text: string; payload?: any }[]; activeTasks?: number; readOnly?: boolean; permissionMode?: string; model?: string; effort?: string; live?: LiveStatus };
// What Claude Code reports to a terminal session's status line.
export type LiveStatus = { model?: string; modelId?: string; effort?: string; costUSD: number; durationMs: number; contextTokens: number; contextSize: number; contextPercent: number; activity?: string; suggestion?: string; permissionMode?: string };

export type Defaults = { provider?: Provider; permissionMode: string; model: string; effort: string };

const permTone = (mode: string) => ['bypassPermissions', 'danger-full-access'].includes(mode) ? ' danger' : dangerousMode(mode) ? ' warn' : '';

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

/** Reported terminal values or configured defaults; each chip opens the quick summary. */
export function SessionChips({ session, defaults, open }: { session: SessionInfo; defaults?: Defaults; open: (anchor: DOMRect) => void }) {
  const show = (event: { currentTarget: Element }) => open(event.currentTarget.getBoundingClientRect());
  const button = (className: string, title: string, text: ReactNode, icon?: ReactNode) => <button type="button" className={className} title={title} aria-haspopup="dialog" onClick={show}>{icon}<span>{text}</span></button>;
  const chip = (label: string, value: string | undefined, inherited: string | undefined, icon: ReactNode, tone = '') => {
    const shown = value || t(`Default(${inherited || `${providerName(providerOf(session))} settings`})`, `默认(${inherited || `${providerName(providerOf(session))} 配置`})`);
    return button(`chip${value ? '' : ' inherited'}${tone}`, t(`Configured ${label}: ${shown}`, `配置${label}：${shown}`), shown, icon);
  };
  const live = session.mode === 'terminal' ? session.live : undefined;
  if (live?.model) {
    const effort = live.effort;
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

export function SessionSettings<T extends SessionInfo>({ api, session, defaults, events, close, onUpdated, onDelete, onImport }: { api: Api; session: T; defaults?: Defaults; events?: readonly CodexEvent[]; close: () => void; onUpdated: (session: T) => void; onDelete: () => void; onImport?: () => void }) {
  const provider = providerOf(session);
  const nativeHistory = !!session.readOnly || session.mode === 'native' || session.mode === 'history';
  const saved = { permissionMode: session.permissionMode || '', model: session.model || '', effort: session.effort || '' };
  const [draft, setDraft] = useState(saved);
  const [editing, setEditing] = useState(false);
  const [stats, setStats] = useState<Partial<Stats> & { tokensKnown?: boolean }>();
  const currentStats = nativeHistory ? undefined : stats;
  const [statsError, setStatsError] = useState(false);
  const [statsReadAt, setStatsReadAt] = useState<string>();
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [saving, setSaving] = useState(false);
  const [profileBusy, setProfileBusy] = useState(false);
  const panel = useRef<HTMLDivElement>(null);
  const changed = editing && (draft.permissionMode !== saved.permissionMode || draft.model.trim() !== saved.model || draft.effort !== saved.effort);
  const mayLeave = () => !saving && !profileBusy && (!changed || window.confirm(t('Discard unsaved session changes?', '放弃未保存的会话修改？')));
  const requestClose = () => { if (mayLeave()) close(); };
  useEscape(requestClose);
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    panel.current?.focus();
    return () => previous?.focus();
  }, []);
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => { if (changed || saving || profileBusy) { event.preventDefault(); event.returnValue = ''; } };
    window.addEventListener('beforeunload', beforeUnload);
    return () => window.removeEventListener('beforeunload', beforeUnload);
  }, [changed, saving, profileBusy]);
  useEffect(() => {
    let active = true;
    setStats(undefined); setStatsReadAt(undefined); setStatsError(false);
    if (nativeHistory) return;
    api<Partial<Stats> & { tokensKnown?: boolean }>(`/api/sessions/${encodeURIComponent(session.id)}/stats`).then(data => { if (active) { setStats(data); setStatsReadAt(new Date().toISOString()); } }).catch(() => { if (active) setStatsError(true); });
    return () => { active = false; };
  }, [api, session.id, nativeHistory]);
  const inherit = (value?: string) => t(`Use default (${value || `${providerName(provider)} native configuration`})`, `跟随默认（${value || `${providerName(provider)} 原生配置`}）`);
  async function save() {
    if (saving || !confirmMode(draft.permissionMode, saved.permissionMode, provider)) return;
    setSaving(true); setError('');
    try {
      onUpdated(await api<T>(`/api/sessions/${encodeURIComponent(session.id)}`, { method: 'PATCH', body: JSON.stringify({ ...draft, model: draft.model.trim(), confirmDangerous: dangerousMode(draft.permissionMode, provider) }) }));
      setEditing(false); setNotice(t('Saved. Configuration applies on the next send after current work and background tasks finish.', '已保存，配置在当前工作及后台任务结束后，下次发送时生效。'));
    } catch (err) { setError((err as Error).message); } finally { setSaving(false); }
  }
  const unknown = t('Not provided', '未提供');
  const fullTime = (value?: string) => value && Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString(locale, { dateStyle: 'full', timeStyle: 'long' }) : t('Time unknown', '时间未知');
  const row = (label: string, value: ReactNode) => <div><dt>{label}</dt><dd>{value}</dd></div>;
  const copy = (label: string, value?: string) => row(label, value ? <span className="session-copy"><code>{value}</code><button type="button" className="secondary" aria-label={t(`Copy ${label}`, `复制${label}`)} onClick={() => { void copyText(value).then(() => setNotice(t('Copied.', '已复制。'))).catch(() => setError(t('Copy failed. Select and copy the text manually.', '复制失败，请选择文字手动复制。'))); }}>{t('Copy', '复制')}</button></span> : unknown);
  const live = session.live;
  const reportedConfiguration = live?.model || live?.modelId || live?.effort || live?.permissionMode;
  const statusLabels: Record<string, string> = { running: t('Running', '执行中'), waiting: t('Waiting for answer', '等待回答'), idle: t('Ready', '就绪'), completed: t('Completed', '已完成'), interrupted: t('Stopped', '已停止'), stopped: t('Stopped', '已停止'), failed: t('Failed', '执行失败'), error: t('Failed', '执行失败'), offline: t('Offline', '离线') };
  const configuration = (key: keyof Defaults, label: string, reported?: string) => {
    if (key === 'provider') return null;
    const override = session[key], fallback = nativeHistory ? undefined : defaults?.[key];
    const source = override ? t('Session override', '本会话覆盖') : fallback ? t('CC Remote default', 'CC Remote 默认') : t(`Provider native configuration (${providerName(provider)} settings)`, `提供方原生配置（${providerName(provider)} 配置）`);
    if (nativeHistory) return <div key={key}><dt>{label}</dt><dd>{override || unknown}<small>{t('Native history metadata', '原生历史元数据')}</small></dd></div>;
    return <div className="session-configuration-row" key={key}><dt>{label}</dt><dd>{reportedConfiguration && <span>{t('Current · ', '当前 · ')}{reported || t('Not reported', '未报告')}{reported && <small>{t('CLI reported', 'CLI 实际报告')}</small>}</span>}<span>{t('Configured / next process · ', '配置 / 下次启动 · ')}{override || fallback || t('Inherit native configuration (value unknown)', '继承原生配置（值未知）')}<small>{source}</small></span></dd></div>;
  };
  const tokenFields = [currentStats?.inputTokens, currentStats?.outputTokens, currentStats?.cacheReadTokens, currentStats?.cacheCreationTokens];
  const reportedTokens = provider === 'codex' ? readCodexUsage(events || [])?.total?.totalTokens : undefined;
  const tokenTotal = reportedTokens ?? (tokenFields.every(value => typeof value === 'number' && Number.isFinite(value)) ? tokenFields.reduce<number>((sum, value) => sum + (value || 0), 0) : undefined);
  const tokensKnown = reportedTokens !== undefined || currentStats?.tokensKnown !== false && tokenTotal !== undefined && (currentStats?.tokensKnown === true || tokenTotal > 0);
  const metric = (value: number | undefined, format = number) => typeof value === 'number' && Number.isFinite(value) ? format(value) : unknown;
  return <div className="modal-backdrop session-details-backdrop" onClick={event => { if (event.target === event.currentTarget) requestClose(); }}>
    <div ref={panel} tabIndex={-1} className="modal session-settings session-details" role="dialog" aria-modal="true" aria-label={t('Session details', '会话详情')} onKeyDown={event => {
      if (event.key !== 'Tab') return;
      const items = Array.from(panel.current?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), [tabindex="0"], summary') || []).filter(item => item.getClientRects().length > 0);
      if (!items?.length) return;
      const first = items[0], last = items[items.length - 1];
      if (event.shiftKey && (document.activeElement === first || document.activeElement === panel.current)) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && (document.activeElement === last || document.activeElement === panel.current)) { event.preventDefault(); first.focus(); }
    }}>
      <div className="modal-heading"><h2>{t('Session details', '会话详情')}</h2><button type="button" className="icon" disabled={saving || profileBusy} aria-label={t('Close session details', '关闭会话详情')} onClick={requestClose}><CloseIcon size={18} /></button></div>
      <h3>{session.title || t('Untitled session', '未命名会话')}</h3>
      <dl className="session-detail-list">
        {row(t('Provider', '提供方'), providerName(provider))}
        {row(t('Mode', '模式'), nativeHistory ? t('Native history', '原生历史') : session.mode === 'terminal' ? t('Terminal', '终端') : t('Managed', '托管'))}
        {row(t('Status', '状态'), statusLabels[session.status || ''] || unknown)}
        {row(t('Last conversation', '最后对话时间'), <>{fullTime(session.lastConversationAt)}{session.conversationTimeEstimated && <small>{t('Estimated from history metadata', '根据历史元数据估算')}</small>}</>)}
        {row(t('Created', '创建时间'), fullTime(session.createdAt))}
        {copy(t('Project directory', '项目目录'), session.cwd)}
      </dl>
      <details><summary>{t('Session identifiers', '会话标识')}</summary><dl className="session-detail-list">{!nativeHistory && copy(t('Session ID', '会话 ID'), session.id)}{copy(provider === 'codex' ? t('Codex thread ID', 'Codex 线程 ID') : t('Claude session ID', 'Claude 会话 ID'), session.claudeSessionId || (nativeHistory ? session.id : undefined))}</dl></details>
      {!nativeHistory && <>
      <h3>{t('Current execution', '当前执行')}</h3>
      <dl className="session-detail-list">
        {row(t('Activity', '当前活动'), live?.activity || t('Not reported', '未报告'))}
        {live?.suggestion && row(t('Waiting / suggested action', '等待 / 建议操作'), live.suggestion)}
        {row(t('Queued messages', '排队消息'), metric(session.queued?.length ?? 0))}
        {row(t('Background tasks', '后台任务'), metric(session.activeTasks ?? 0))}
      </dl>
      </>}
      {!nativeHistory && !editing && <SessionProfile api={api} app={provider} session={session} onUpdated={onUpdated} onBusy={setProfileBusy} />}
      <h3>{nativeHistory ? t('Recorded configuration', '历史配置') : t('Configuration', '配置')}</h3>
      {!nativeHistory && !reportedConfiguration && <p>{t('Current configuration has not been reported by the CLI. Values below apply when the process can safely restart.', 'CLI 尚未报告当前配置。以下为进程可安全重启时应用的配置预期。')}</p>}
      <dl className="session-detail-list">{configuration('model', t('Model', '模型'), live?.model || live?.modelId)}{configuration('effort', t('Effort', '推理强度'), live?.effort)}{configuration('permissionMode', t('Permission / sandbox', '权限 / 沙箱'), live?.permissionMode)}</dl>
      {!nativeHistory && !editing && <button type="button" className="secondary" disabled={profileBusy} onClick={() => { setDraft(saved); setEditing(true); setNotice(''); }}>{t('Edit session overrides', '修改本会话覆盖')}</button>}
      {editing && <form onSubmit={event => { event.preventDefault(); if (changed) void save(); }}>
        <p>{t('Changes apply on the next send after current work and background tasks finish. Empty fields inherit defaults; current execution values remain as reported.', '修改在当前工作及后台任务结束后，下次发送时生效；留空继承默认值，当前执行值仍以实际报告为准。')}</p>
        <fieldset disabled={saving}>
          <div className="field">{t('Model', '模型')}<ModelPicker api={api} provider={provider} sessionId={session.id} label={t('Session model', '会话模型')} inheritLabel={inherit(defaults?.model)} value={draft.model} onChange={model => setDraft(old => ({ ...old, model }))} /></div>
          <div className="field">{t('Effort', '推理强度')}<Select label={t('Session effort', '会话推理强度')} value={draft.effort} options={effortOptions(inherit(defaults?.effort), provider)} onChange={effort => setDraft(old => ({ ...old, effort }))} /></div>
          <div className="field">{t('Permission mode', '权限模式')}<Select label={t('Session permission mode', '会话权限模式')} value={draft.permissionMode} options={permissionOptions(inherit(defaults?.permissionMode), draft.permissionMode, provider)} onChange={permissionMode => setDraft(old => ({ ...old, permissionMode }))} /></div>
          <div className="modal-actions"><button type="button" className="secondary" onClick={() => setDraft({ permissionMode: '', model: '', effort: '' })}>{t('Restore inheritance', '恢复继承')}</button><button type="button" className="secondary" onClick={() => { if (mayLeave()) { setDraft(saved); setEditing(false); } }}>{t('Cancel', '取消')}</button><button className="primary" disabled={!changed}>{saving ? t('Saving…', '保存中…') : t('Save', '保存')}</button></div>
        </fieldset>
      </form>}
      <h3>{t('Statistics', '统计')}</h3>
      <p>{t('CC Remote recorded events for this session; subscription quotas are separate. Cost, when reported, is an API-price estimate.', '此会话在 CC Remote 记录的事件；订阅额度另计。已报告费用按 API 价格估算。')}</p>
      {!nativeHistory && statsError && <p role="status">{t('Statistics unavailable.', '统计暂不可用。')}</p>}
      <dl className="session-detail-list">
        {row(t('Turns', '轮次'), metric(currentStats?.turns))}
        {row(t('Execution duration', '执行耗时'), metric(currentStats?.durationMs, value => duration(value / 1000)))}
        {row(t('Tokens', 'Token'), tokensKnown ? number(tokenTotal!) : unknown)}
        {row(t('Cost (estimated)', '费用（估算）'), currentStats?.costKnown === true && typeof currentStats.costUSD === 'number' && Number.isFinite(currentStats.costUSD) ? money({ costKnown: true, costUSD: currentStats.costUSD }) : unknown)}
        {row(t('Context', '上下文'), live && live.contextSize > 0 ? `${number(live.contextTokens)} / ${number(live.contextSize)}` : unknown)}
        {row(t('Last recorded event', '最后记录事件'), fullTime(currentStats?.lastActivityAt))}
        {row(t('Statistics retrieved', '统计读取时间'), fullTime(nativeHistory ? undefined : statsReadAt))}
      </dl>
      {provider === 'codex' && events && <CodexSessionUsage events={events} />}
      {nativeHistory && onImport && <button type="button" className="primary" onClick={onImport}>{t('Import session…', '导入会话…')}</button>}
      {!nativeHistory && <details><summary>{t('Management', '管理')}</summary><button type="button" className="danger-button" disabled={saving} onClick={() => { if (mayLeave()) onDelete(); }}>{t('Delete session…', '删除会话…')}</button></details>}
      {notice && <p role="status">{notice}</p>}{error && <div className="error" role="alert">{error}</div>}
    </div>
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
        <p>{t(`${providerName(providerOf(session))} native session history `, `${providerName(providerOf(session))} 原生会话历史 `)}<strong>{t('is not deleted', '不会删除')}</strong>{t(' and can still be viewed or resumed under "Terminal history". This cannot be undone.', '，仍可在「终端历史」中查看或恢复。此操作无法撤销。')}</p>
      </div>
      {running && <label className="checkbox"><input type="checkbox" checked={stop} onChange={event => setStop(event.target.checked)} />{t('This session is running; stop the current run before deleting', '此会话正在执行，先停止当前执行再删除')}</label>}
      {error && <div className="error" role="alert">{error}</div>}
      <div className="modal-actions"><button type="button" className="secondary" disabled={busy} onClick={close}>{t('Cancel', '取消')}</button><button type="button" className="danger-button solid" disabled={busy || (running && !stop)} onClick={() => void submit()}>{busy ? t('Deleting…', '删除中…') : running ? t('Stop and delete', '停止并删除') : t('Delete session', '删除会话')}</button></div>
    </div>
  </div>;
}

import { providerName, providerOf } from './providers';
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import type { Defaults, SessionInfo } from './SessionSettings';
import type { QuickKind } from './QuickSwitch';
import { GearIcon, TerminalIcon } from './icons';
import { t } from './i18n';
import { contextReading } from './contextReading';
import type { CodexEvent } from './codexStatus';
import { permissionOptions } from './ModelPicker';
import type { Api } from './ModelPicker';
import { SessionProfile } from './SupplierProfile';

/** Quick execution summary and switches; the details action opens the unified panel. */
export function SessionStatus<T extends SessionInfo>({ session, defaults, label, anchor, close, quick, settings, attach, events, api, onUpdated }: { session: T; defaults?: Defaults; label: string; anchor: DOMRect; close: () => void; quick: (kind: QuickKind, anchor: DOMRect) => void; settings: () => void; attach?: () => void; events?: readonly CodexEvent[]; api: Api; onUpdated: (session: T) => void }) {
  const box = useRef<HTMLDivElement>(null);
  const [showSupplier, setShowSupplier] = useState(false);
  const [supplierBusy, setSupplierBusy] = useState(false);
  const [place, setPlace] = useState({ left: anchor.left, top: anchor.bottom + 6 });
  useEffect(() => {
    const away = (event: MouseEvent | TouchEvent) => { if (!supplierBusy && !box.current?.contains(event.target as Node) && !(event.target as Element).closest?.('.mobile-badge,.desktop-badge,.select-menu')) close(); };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') close(); };
    document.addEventListener('mousedown', away); document.addEventListener('touchstart', away); document.addEventListener('keydown', key);
    return () => { document.removeEventListener('mousedown', away); document.removeEventListener('touchstart', away); document.removeEventListener('keydown', key); };
  }, [close, supplierBusy]);
  useLayoutEffect(() => {
    const position = () => {
      const rect = box.current?.getBoundingClientRect();
      if (rect) setPlace({ left: Math.max(8, Math.min(anchor.left, window.innerWidth - rect.width - 8)), top: Math.max(8, Math.min(anchor.bottom + 6, window.innerHeight - rect.height - 8)) });
    };
    position();
    window.addEventListener('resize', position);
    return () => window.removeEventListener('resize', position);
  }, [anchor, showSupplier]);
  const live = session.live;
  const inherited = (value: string | undefined, fallback: string | undefined) => value || t(`Default (${fallback || `${providerName(providerOf(session))} settings`})`, `默认（${fallback || `${providerName(providerOf(session))} 配置`}）`);
  const model = live?.model ? `${live.model}${live.modelId && live.modelId !== live.model ? ` · ${live.modelId}` : ''}` : inherited(session.model, defaults?.model);
  const effort = live?.effort || inherited(session.effort, defaults?.effort);
  const context = contextReading(providerOf(session), live, events);
  const permission = (value: string | undefined) => value ? permissionOptions('', value, providerOf(session)).find(option => option.value === value)?.label || value : inherited(value, defaults?.permissionMode);
  const switcher = (kind: QuickKind) => <button type="button" className="session-status-switch" onClick={event => { const rect = event.currentTarget.getBoundingClientRect(); close(); quick(kind, rect); }}>{t('Switch', '切换')}</button>;
  const row = (name: string, value: ReactNode, extra?: ReactNode) => <div className="session-status-row"><dt>{name}</dt><dd>{value}</dd>{extra}</div>;
  return <div ref={box} className="session-status popover-menu" role="dialog" aria-label={t('Session status', '会话状态')} style={{ left: place.left, top: place.top, maxHeight: `calc(100dvh - ${place.top + 8}px)` }}>
    <dl>
      {row(t('Provider', '提供方'), providerName(providerOf(session)))}
      {row(t('Session type', '会话类型'), session.mode === 'terminal' ? t('Terminal', '终端') : t('Managed', '托管'))}
      {row(t('Supplier', '供应商'), session.profileName || (session.profileId ? t('Configured supplier', '已配置供应商') : t('Host configuration', '宿主配置')), <button type="button" className="session-status-switch" aria-expanded={showSupplier} onClick={() => setShowSupplier(value => !value)}>{showSupplier ? t('Close', '收起') : t('Switch', '切换')}</button>)}
      {row(t('Status', '状态'), <><span className={`dot ${session.status || 'idle'}`} />{label}</>)}
      {live?.activity && row(t('Activity', '当前活动'), live.activity)}
      {row(live?.model ? t('Reported model', '已报告模型') : t('Configured model', '配置模型'), model, switcher('model'))}
      {row(live?.effort ? t('Reported effort', '已报告推理强度') : t('Configured effort', '配置推理强度'), effort, switcher('effort'))}
      {row(t('Permission mode', '权限模式'), permission(live?.permissionMode || session.permissionMode))}
      {(live?.model && session.model && session.model !== (live.modelId || live.model)) && row(t('Next model', '下一轮模型'), session.model)}
      {(live?.effort && session.effort && live.effort !== session.effort) && row(t('Next effort', '下一轮推理强度'), session.effort)}
      {(live?.permissionMode && session.permissionMode && live.permissionMode !== session.permissionMode) && row(t('Next permissions', '下一轮权限'), permission(session.permissionMode))}
      {row(t('Queued messages', '待发送消息'), session.queued?.length || 0)}
      {!!session.activeTasks && row(t('Background tasks', '后台任务'), session.activeTasks)}
      {context.percent !== undefined && row(t('Context', '上下文'), `${Math.round(context.percent)}% · ${context.tokens?.toLocaleString()} / ${context.capacity?.toLocaleString()}`)}
      {context.total?.totalTokens !== undefined && row(t('Session tokens', '会话累计 Token'), context.total.totalTokens.toLocaleString())}
    </dl>
    {showSupplier && <div className="session-status-supplier"><SessionProfile api={api} app={providerOf(session)} session={session} onUpdated={onUpdated} onBusy={setSupplierBusy} showEmpty /></div>}
    {attach && <button type="button" className="session-status-settings" onClick={() => { close(); attach(); }}><TerminalIcon size={14} />{t('Attach from a local terminal', '在本地终端接入')}</button>}
    <button type="button" className="session-status-settings" onClick={() => { close(); settings(); }}><GearIcon size={14} />{t('Session details', '会话详情')}</button>
  </div>;
}

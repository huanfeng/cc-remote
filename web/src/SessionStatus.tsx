import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import type { Defaults, SessionInfo } from './SessionSettings';
import type { QuickKind } from './QuickSwitch';
import { duration } from './StatusPanel';
import { GearIcon } from './icons';
import { locale, t } from './i18n';

/** The session's status under the top bar's chips: everything the chips cut short,
 * with the quick switches and the settings one tap away. */
export function SessionStatus({ session, defaults, label, anchor, close, quick, settings }: { session: SessionInfo; defaults?: Defaults; label: string; anchor: DOMRect; close: () => void; quick: (kind: QuickKind, anchor: DOMRect) => void; settings: () => void }) {
  const box = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState({ left: anchor.left, top: anchor.bottom + 6 });
  useEffect(() => {
    const away = (event: MouseEvent | TouchEvent) => { if (!box.current?.contains(event.target as Node) && !(event.target as Element).closest?.('.session-chips,.mobile-badge')) close(); };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') close(); };
    document.addEventListener('mousedown', away); document.addEventListener('touchstart', away); document.addEventListener('keydown', key);
    return () => { document.removeEventListener('mousedown', away); document.removeEventListener('touchstart', away); document.removeEventListener('keydown', key); };
  }, [close]);
  useLayoutEffect(() => {
    const rect = box.current?.getBoundingClientRect();
    if (rect) setPlace({ left: Math.max(8, Math.min(anchor.left, window.innerWidth - rect.width - 8)), top: anchor.bottom + 6 });
  }, [anchor]);
  const live = session.mode === 'terminal' ? session.live : undefined;
  const inherited = (value: string | undefined, fallback: string | undefined) => value || t(`Default (${fallback || 'Claude settings'})`, `默认（${fallback || 'Claude 配置'}）`);
  const model = live?.model ? `${live.model}${live.modelId && live.modelId !== live.model ? ` · ${live.modelId}` : ''}` : inherited(session.model, defaults?.model);
  const effort = live?.effort || inherited(session.effort, defaults?.effort);
  const perm = session.permissionMode || defaults?.permissionMode || '';
  const pct = live?.contextSize ? Math.min(100, Math.round(live.contextPercent)) : undefined;
  const switcher = (kind: QuickKind) => <button type="button" className="session-status-switch" onClick={event => { const rect = event.currentTarget.getBoundingClientRect(); close(); quick(kind, rect); }}>{t('Switch', '切换')}</button>;
  const row = (name: string, value: ReactNode, extra?: ReactNode) => <div className="session-status-row"><dt>{name}</dt><dd>{value}</dd>{extra}</div>;
  return <div ref={box} className="session-status popover-menu" role="dialog" aria-label={t('Session status', '会话状态')} style={{ left: place.left, top: place.top }}>
    <dl>
      {row(t('Status', '状态'), <><span className={`dot ${session.status || 'idle'}`} />{label}</>)}
      {live?.activity && row(t('Activity', '当前活动'), live.activity)}
      {row(t('Model', '模型'), model, switcher('model'))}
      {row(t('Effort', '推理强度'), effort, switcher('effort'))}
      {row(t('Permission mode', '权限模式'), <span className={perm === 'bypassPermissions' ? 'danger' : ''}>{inherited(session.permissionMode, defaults?.permissionMode)}</span>)}
      {live && pct !== undefined && row(t('Context', '上下文'), <span className="session-status-context"><span className="session-status-meter" aria-hidden="true"><i className={pct >= 80 ? 'danger' : pct >= 60 ? 'warn' : ''} style={{ width: `${pct}%` }} /></span>{`${pct}% · ${live.contextTokens.toLocaleString(locale)} / ${live.contextSize.toLocaleString(locale)}`}</span>)}
      {live && live.durationMs > 0 && row(t('Duration', '会话时长'), duration(live.durationMs / 1000))}
      {live && live.costUSD > 0 && row(t('Cost', '费用'), <span title={t('Estimated at API prices', '按 API 价格估算')}>${live.costUSD.toFixed(2)}</span>)}
      {row(t('Directory', '目录'), <code>{session.cwd}</code>)}
    </dl>
    <button type="button" className="session-status-settings" onClick={() => { close(); settings(); }}><GearIcon size={14} />{t('Session settings', '会话设置')}</button>
  </div>;
}

import { providerName, providerOf } from './providers';
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import type { Defaults, SessionInfo } from './SessionSettings';
import type { QuickKind } from './QuickSwitch';
import { GearIcon, TerminalIcon } from './icons';
import { t } from './i18n';

/** Quick execution summary and switches; the details action opens the unified panel. */
export function SessionStatus({ session, defaults, label, anchor, close, quick, settings, attach }: { session: SessionInfo; defaults?: Defaults; label: string; anchor: DOMRect; close: () => void; quick: (kind: QuickKind, anchor: DOMRect) => void; settings: () => void; attach?: () => void }) {
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
  const live = session.live;
  const inherited = (value: string | undefined, fallback: string | undefined) => value || t(`Default (${fallback || `${providerName(providerOf(session))} settings`})`, `默认（${fallback || `${providerName(providerOf(session))} 配置`}）`);
  const model = live?.model ? `${live.model}${live.modelId && live.modelId !== live.model ? ` · ${live.modelId}` : ''}` : inherited(session.model, defaults?.model);
  const effort = live?.effort || inherited(session.effort, defaults?.effort);
  const switcher = (kind: QuickKind) => <button type="button" className="session-status-switch" onClick={event => { const rect = event.currentTarget.getBoundingClientRect(); close(); quick(kind, rect); }}>{t('Switch', '切换')}</button>;
  const row = (name: string, value: ReactNode, extra?: ReactNode) => <div className="session-status-row"><dt>{name}</dt><dd>{value}</dd>{extra}</div>;
  return <div ref={box} className="session-status popover-menu" role="dialog" aria-label={t('Session status', '会话状态')} style={{ left: place.left, top: place.top }}>
    <dl>
      {row(t('Provider', '提供方'), providerName(providerOf(session)))}
      {row(t('Status', '状态'), <><span className={`dot ${session.status || 'idle'}`} />{label}</>)}
      {live?.activity && row(t('Activity', '当前活动'), live.activity)}
      {row(live?.model ? t('Reported model', '已报告模型') : t('Configured model', '配置模型'), model, switcher('model'))}
      {row(live?.effort ? t('Reported effort', '已报告推理强度') : t('Configured effort', '配置推理强度'), effort, switcher('effort'))}

    </dl>
    {attach && <button type="button" className="session-status-settings" onClick={() => { close(); attach(); }}><TerminalIcon size={14} />{t('Attach from a local terminal', '在本地终端接入')}</button>}
    <button type="button" className="session-status-settings" onClick={() => { close(); settings(); }}><GearIcon size={14} />{t('Session details', '会话详情')}</button>
  </div>;
}

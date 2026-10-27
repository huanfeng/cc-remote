import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { LiveStatus } from './SessionSettings';
import type { Provider } from './providers';
import type { CodexEvent } from './codexStatus';
import { contextReading } from './contextReading';
import { locale, t } from './i18n';
import './context-usage.css';

/** Session context and account subscription quota deliberately have separate controls. */
export function ContextUsage({ provider, live, events = [], sessionId }: { provider: Provider; live?: LiveStatus; events?: readonly CodexEvent[]; sessionId?: string }) {
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState({ left: 8, bottom: 8 });
  const trigger = useRef<HTMLButtonElement>(null), panel = useRef<HTMLDivElement>(null);
  const reading = contextReading(provider, live, events);
  const known = reading.percent !== undefined;
  const shown = known ? `${Math.round(reading.percent!)}%` : '—';
  const title = t(`Session context: ${known ? shown : 'unknown'}. Open token details.`, `会话上下文：${known ? shown : '未知'}。点击查看 Token 详情。`);
  useEffect(() => { setOpen(false); }, [sessionId, provider]);
  useEffect(() => {
    if (!open) return;
    const outside = (event: PointerEvent) => { if (!trigger.current?.contains(event.target as Node) && !panel.current?.contains(event.target as Node)) setOpen(false); };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.preventDefault(); setOpen(false); trigger.current?.focus(); } };
    document.addEventListener('pointerdown', outside); document.addEventListener('keydown', key);
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', key); };
  }, [open]);
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const rect = trigger.current?.getBoundingClientRect(), box = panel.current?.getBoundingClientRect();
      if (!rect || !box) return;
      setPosition({ left: Math.max(8, Math.min(rect.right - box.width, window.innerWidth - box.width - 8)), bottom: Math.max(8, window.innerHeight - rect.top + 6) });
    };
    place(); window.addEventListener('resize', place); window.addEventListener('scroll', place, true);
    return () => { window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true); };
  }, [open]);
  const n = (value?: number) => value === undefined ? t('Not reported', '未报告') : value.toLocaleString(locale);
  const row = (label: string, value: string) => <div><dt>{label}</dt><dd>{value}</dd></div>;
  return <>
    <button ref={trigger} type="button" className={`context-usage-button${known && reading.percent! >= 80 ? ' warn' : ''}`} aria-label={title} data-tip={title} aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(value => !value)}>
      <svg viewBox="0 0 32 32" width="30" height="30" aria-hidden="true"><circle cx="16" cy="16" r="12" className="context-usage-track" />
        {known && <circle cx="16" cy="16" r="12" className="context-usage-fill" pathLength="100" strokeDasharray={`${reading.percent} 100`} transform="rotate(-90 16 16)" />}
        <text x="16" y="19" textAnchor="middle">{shown}</text>
      </svg>
    </button>
    {open && createPortal(<div ref={panel} className="context-usage-panel popover-menu" role="dialog" aria-label={t('Session context details', '会话上下文详情')} style={position}>
      <strong>{t('Session context', '会话上下文')}</strong>
      <dl>{row(t('Context used', '上下文占用'), shown)}{row(t('Context tokens', '上下文 Token'), n(reading.tokens))}{row(t('Context capacity', '上下文容量'), n(reading.capacity))}
        {reading.total && <>{row(t('Session tokens', '会话累计 Token'), n(reading.total.totalTokens))}{row(t('Input tokens', '累计输入 Token'), n(reading.total.inputTokens))}{row(t('Output tokens', '累计输出 Token'), n(reading.total.outputTokens))}{row(t('Cached input tokens', '累计缓存输入 Token'), n(reading.total.cachedInputTokens))}</>}
      </dl>
      <small>{provider === 'codex' ? t('Context uses the latest model call reported by Codex. Session totals include earlier calls.', '上下文根据 Codex 报告的最近一次模型调用显示，累计用量包含此前的调用。') : t('Context is reported by Claude. Missing readings stay unknown.', '上下文由 Claude 报告；未提供的读数显示为未知。')}</small>
    </div>, document.body)}
  </>;
}

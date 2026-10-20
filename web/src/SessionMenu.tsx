import { useEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { t } from './i18n';

export type SessionAction = { key: string; label: string; hint?: string; icon: ReactNode; danger?: boolean; onClick: () => void };

/** A session row's "…" menu: one line of icon and words per action, instead of bare
 * icons whose meaning has to be guessed (the hint shows on hover). On the body, placed
 * by the button: the session list scrolls and would clip it. */
export function SessionMenu({ title, actions }: { title: string; actions: SessionAction[] }) {
  const [place, setPlace] = useState<{ right: number; top?: number; bottom?: number }>();
  const button = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!place) return;
    const close = () => setPlace(undefined);
    const outside = (event: PointerEvent) => { if (!menu.current?.contains(event.target as Node) && !button.current?.contains(event.target as Node)) close(); };
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') close(); };
    document.addEventListener('pointerdown', outside);
    document.addEventListener('keydown', escape);
    window.addEventListener('resize', close);
    document.addEventListener('scroll', close, true);
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape); window.removeEventListener('resize', close); document.removeEventListener('scroll', close, true); };
  }, [place]);
  if (!actions.length) return null;
  const toggle = () => {
    if (place) { setPlace(undefined); return; }
    const box = button.current!.getBoundingClientRect();
    const right = Math.max(8, window.innerWidth - box.right);
    // Opens upward near the bottom of the window.
    setPlace(box.bottom + 38 * actions.length + 16 > window.innerHeight ? { right, bottom: window.innerHeight - box.top + 4 } : { right, top: box.bottom + 4 });
  };
  return <>
    <button ref={button} type="button" className={`session-more${place ? ' active' : ''}`} aria-label={t(`Actions of ${title}`, `${title} 的操作`)} title={t('More actions', '更多操作')} aria-haspopup="menu" aria-expanded={!!place} onClick={toggle}>
      <svg width="15" height="15" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><circle cx="3.5" cy="8" r="1.3" /><circle cx="8" cy="8" r="1.3" /><circle cx="12.5" cy="8" r="1.3" /></svg>
    </button>
    {place && createPortal(<div ref={menu} className="session-menu popover-menu" role="menu" style={place}>
      {actions.map(action => <button key={action.key} type="button" role="menuitem" className={action.danger ? 'danger' : undefined} title={action.hint} onClick={() => { setPlace(undefined); action.onClick(); }}>
        {action.icon}<span>{action.label}</span>
      </button>)}
    </div>, document.body)}
  </>;
}

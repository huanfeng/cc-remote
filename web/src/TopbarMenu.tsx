import { useEffect, useRef, useState, type ReactNode } from 'react';
import { t } from './i18n';

export type TopbarAction = { key: string; label: string; icon: ReactNode; onClick: () => void; active?: boolean; desktop?: boolean };

/** Overflow menu of the top bar: folded actions on desktop, plus secondary actions at phone width. */
export function TopbarMenu({ actions }: { actions: TopbarAction[] }) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const outside = (event: PointerEvent) => { if (!box.current?.contains(event.target as Node)) setOpen(false); };
    const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') setOpen(false); };
    document.addEventListener('pointerdown', outside);
    document.addEventListener('keydown', escape);
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape); };
  }, [open]);
  if (!actions.length) return null;
  const hasDesktop = actions.some(action => action.desktop);
  return <div className={`topbar-more${hasDesktop ? '' : ' mobile'}`} ref={box}>
    <button className={`icon${open ? ' active' : ''}`} aria-label={t('More actions', '更多操作')} title={t('More actions', '更多操作')} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(old => !old)}>
      <svg width="17" height="17" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><circle cx="3.5" cy="8" r="1.3" /><circle cx="8" cy="8" r="1.3" /><circle cx="12.5" cy="8" r="1.3" /></svg>
    </button>
    {open && <div className="topbar-menu popover-menu" role="menu">
      {actions.map(action => <button key={action.key} type="button" role="menuitem" className={[action.active ? 'active' : '', action.desktop ? '' : 'mobile'].filter(Boolean).join(' ') || undefined} onClick={() => { setOpen(false); action.onClick(); }}>{action.icon}<span>{action.label}</span></button>)}
    </div>}
  </div>;
}

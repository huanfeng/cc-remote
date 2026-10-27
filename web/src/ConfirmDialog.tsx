import { useEffect, useRef, useState } from 'react';
import { t } from './i18n';

type Ask = (message: string) => Promise<boolean>;
let ask: Ask | undefined;

/** In-page replacement for window.confirm; falls back to the native dialog when no host is mounted (tests). */
export function confirmDialog(message: string): Promise<boolean> {
  return ask ? ask(message) : Promise.resolve(window.confirm(message));
}

/** Mount once near the app root to answer confirmDialog calls. */
export function ConfirmDialog() {
  const [state, setState] = useState<{ message: string; resolve: (value: boolean) => void }>();
  const pending = useRef<((value: boolean) => void) | undefined>(undefined);
  useEffect(() => {
    ask = message => new Promise(resolve => {
      pending.current?.(false); // a newer question supersedes the pending one
      pending.current = resolve;
      setState({ message, resolve });
    });
    return () => { pending.current?.(false); pending.current = undefined; ask = undefined; };
  }, []);
  useEffect(() => {
    if (!state) return;
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.stopPropagation(); state.resolve(false); setState(undefined); }
    };
    document.addEventListener('keydown', key, true);
    return () => document.removeEventListener('keydown', key, true);
  }, [state]);
  if (!state) return null;
  const done = (value: boolean) => { state.resolve(value); pending.current = undefined; setState(undefined); };
  return <div className="modal-backdrop confirm-backdrop" onClick={event => { if (event.target === event.currentTarget) done(false); }}>
    <div className="modal confirm-modal" role="alertdialog" aria-modal="true" aria-label={t('Confirm', '确认')}>
      <p className="confirm-message">{state.message}</p>
      <div className="modal-actions"><button type="button" className="secondary" onClick={() => done(false)}>{t('Cancel', '取消')}</button><button type="button" className="primary" onClick={() => done(true)}>{t('Confirm', '确认')}</button></div>
    </div>
  </div>;
}

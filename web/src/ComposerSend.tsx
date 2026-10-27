import { useEffect, useRef, useState, type ReactNode } from 'react';
import { ChevronDownIcon } from './icons';
import { t } from './i18n';

const shortcuts = [
  { value: 'enter', label: t('Enter sends', 'Enter 发送'), description: t('Shift + Enter adds a new line', 'Shift + Enter 换行') },
  { value: 'ctrl-enter', label: 'Ctrl / ⌘ + Enter', description: t('Enter adds a new line; the shortcut sends', 'Enter 换行，组合键发送') },
];

/** Keep the primary action and its shortcut settings together at every width. */
export function ComposerSend({ disabled, uploading, running, sendKey, onSendKey, extra }: {
  disabled: boolean; uploading: boolean; running: boolean; sendKey: string; onSendKey: (value: string) => void;
  extra?: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!open) return;
    const away = (event: PointerEvent) => { if (!box.current?.contains(event.target as Node)) setOpen(false); };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.stopPropagation(); setOpen(false); trigger.current?.focus(); } };
    document.addEventListener('pointerdown', away);
    document.addEventListener('keydown', key, true);
    return () => { document.removeEventListener('pointerdown', away); document.removeEventListener('keydown', key, true); };
  }, [open]);
  return <div className="composer-send" ref={box}>
    <button type="submit" className="primary" disabled={disabled} title={running ? t('Send after the current task', '当前任务结束后发送') : t('Send message', '发送消息')}>
      {uploading ? t('Uploading…', '上传中…') : t('Send ↑', '发送 ↑')}
    </button>
    <button type="button" ref={trigger} className="send-settings-trigger" aria-label={t('Send settings', '发送设置')} aria-haspopup="dialog" aria-expanded={open} onClick={() => setOpen(old => !old)}><ChevronDownIcon size={13} /></button>
    {open && <div className="send-settings-menu popover-menu" role="dialog" aria-label={t('Send settings', '发送设置')}>
      <span className="eyebrow">{t('Send shortcut', '发送快捷键')}</span>
      <div role="radiogroup" aria-label={t('Send shortcut', '发送快捷键')}>{shortcuts.map(option => <button key={option.value} type="button" role="radio" aria-checked={sendKey === option.value} className="popover-option" onClick={() => { onSendKey(option.value); setOpen(false); trigger.current?.focus(); }}><span aria-hidden="true">{sendKey === option.value ? '✓' : '　'}</span><span>{option.label}<small>{option.description}</small></span></button>)}</div>
      {extra && <div className="send-settings-extra">{extra}</div>}
      <small>{t('During a task, messages wait in the pending area.', '任务进行时，消息默认进入待发送区。')}</small>
    </div>}
  </div>;
}

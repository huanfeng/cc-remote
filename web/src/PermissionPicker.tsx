import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from 'react';
import { createPortal } from 'react-dom';
import { dangerousMode, permissionOptions } from './ModelPicker';
import { Select } from './Select';
import { t } from './i18n';
import type { Provider } from './providers';
import './permission-picker.css';

export function permissionNeedsConfirmation(value: string, inheritMode: string | undefined, provider: Provider) {
  return dangerousMode(value || inheritMode || '', provider);
}

/** Confirm newly selected permission modes in context, before changing the value. */
export function PermissionPicker({ provider, value, onChange, inheritLabel, inheritMode, label, className, title, disabled }: { provider: Provider; value: string; onChange: (value: string) => void; inheritLabel: string; inheritMode?: string; label: string; className?: string; title?: string; disabled?: boolean }) {
  const root = useRef<HTMLDivElement>(null);
  const dialog = useRef<HTMLDivElement>(null);
  const [pending, setPending] = useState<string | null>(null);
  const [position, setPosition] = useState<CSSProperties>({ position: 'fixed', visibility: 'hidden' });
  const options = permissionOptions(inheritLabel, value, provider);
  const effective = pending || inheritMode || '';
  const selection = permissionOptions('', effective, provider).find(option => option.value === effective)?.label || effective;

  useLayoutEffect(() => {
    if (pending === null) return;
    const place = () => {
      const rect = root.current?.getBoundingClientRect(), box = dialog.current;
      if (!rect || !box) return;
      const width = Math.min(320, window.innerWidth - 16);
      const left = Math.max(8, Math.min(rect.left, window.innerWidth - width - 8));
      const above = rect.top >= box.offsetHeight + 12;
      setPosition({ position: 'fixed', left, width, ...(above ? { bottom: window.innerHeight - rect.top + 6 } : { top: rect.bottom + 6 }) });
    };
    place();
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);
    return () => { window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true); };
  }, [pending]);
  useEffect(() => {
    if (pending === null) return;
    const dismiss = (event: PointerEvent) => { const target = event.target as Node; if (!root.current?.contains(target) && !dialog.current?.contains(target)) setPending(null); };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setPending(null); root.current?.querySelector('button')?.focus(); } };
    dialog.current?.focus();
    document.addEventListener('pointerdown', dismiss, true);
    document.addEventListener('keydown', key, true);
    return () => { document.removeEventListener('pointerdown', dismiss, true); document.removeEventListener('keydown', key, true); };
  }, [pending]);
  useEffect(() => { setPending(null); }, [provider, value, disabled]);

  const detail = provider === 'codex' ? t('Codex can access files and run commands without sandbox restrictions.', 'Codex 可以不受沙箱限制地访问文件和执行命令。') : effective === 'bypassPermissions' ? t('Claude will skip all tool approvals and can edit files and run commands directly.', 'Claude 将跳过所有工具审批，可以直接修改文件和执行命令。') : effective === 'auto' ? t('Claude will automatically decide and approve some tool calls.', 'Claude 将自动判断并批准部分工具调用。') : t('Tools not allowed in advance are denied without asking.', '未预先允许的工具会被直接拒绝，不再询问。');
  return <div className="permission-picker" ref={root}>
    <Select label={label} title={title} className={className} disabled={disabled} value={value} options={options} onChange={next => { if (permissionNeedsConfirmation(next, inheritMode, provider)) setPending(next); else onChange(next); }} />
    {pending !== null && createPortal(<div ref={dialog} className="permission-confirm popover-menu" style={position} tabIndex={-1} role="alertdialog" aria-label={t('Confirm permission change', '确认权限变更')} onKeyDown={event => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setPending(null); } }}>
      <strong>{t(`Enable ${selection}?`, `启用${selection}？`)}</strong><p>{detail}</p><small>{t('Use only in trusted projects.', '请仅在可信项目中使用。')}</small>
      <div className="permission-confirm-actions"><button type="button" className="secondary" onClick={() => setPending(null)}>{t('Cancel', '取消')}</button><button type="button" className="danger-button solid" onClick={() => { const next = pending; setPending(null); onChange(next); }}>{t('Confirm', '确认启用')}</button></div>
    </div>, document.body)}
  </div>;
}

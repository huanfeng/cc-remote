import { useLayoutEffect, useRef, useState, type TextareaHTMLAttributes } from 'react';
import { t } from './i18n';

// The message box grows with its text up to a few lines; longer text scrolls
// and a button in the top-right corner enlarges the box.
export function ComposerInput({ compact, ...props }: TextareaHTMLAttributes<HTMLTextAreaElement> & { value: string; compact?: boolean }) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const [overflow, setOverflow] = useState(false);
  const [expanded, setExpanded] = useState(false);
  useLayoutEffect(() => {
    const box = ref.current;
    if (!box) return;
    box.style.height = 'auto'; // measure the text, not the current box
    const max = parseFloat(getComputedStyle(box).maxHeight) || Infinity;
    box.style.height = `${Math.min(box.scrollHeight + 2, max)}px`;
    const over = box.scrollHeight > max + 1;
    setOverflow(over);
    if (!props.value) setExpanded(false); // sent: back to the normal size
  }, [props.value, expanded, compact]); // compact: a line while the history is read (App)
  return <div className={`composer-input${expanded ? ' expanded' : ''}`}>
    <textarea ref={ref} rows={1} {...props} />
    {(overflow || expanded) && <button type="button" className="composer-expand" aria-label={expanded ? t('Shrink message box', '收起输入框') : t('Enlarge message box', '放大输入框')} title={expanded ? t('Shrink', '收起') : t('Enlarge', '放大')} onClick={() => { setExpanded(old => !old); ref.current?.focus(); }}>
      <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{expanded ? <path d="M6.5 2v4.5H2M9.5 14V9.5H14M6.5 6.5 2 2M9.5 9.5 14 14" /> : <path d="M10 2h4v4M6 14H2v-4M14 2 9.5 6.5M2 14l4.5-4.5" />}</svg>
    </button>}
  </div>;
}

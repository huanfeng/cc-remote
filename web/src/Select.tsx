import { useEffect, useId, useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import { CheckIcon, ChevronDownIcon } from './icons';

export type SelectTone = 'warn' | 'danger';
export type SelectOption = { value: string; label: string; description?: string; tag?: string; tone?: SelectTone };

const GAP = 4, MARGIN = 8, MAX_HEIGHT = 300;
const HIDDEN: CSSProperties = { position: 'fixed', top: 0, left: 0, visibility: 'hidden' };

/** Select-only combobox: focus stays on the trigger, the listbox is portaled to body so modal overflow never clips it. */
export function Select({ value, options, onChange, label, placeholder = '请选择', className = '', title, disabled }: { value: string; options: SelectOption[]; onChange: (value: string) => void; label: string; placeholder?: string; className?: string; title?: string; disabled?: boolean }) {
  const id = useId();
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLUListElement>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [style, setStyle] = useState<CSSProperties>(HIDDEN);
  const typed = useRef({ text: '', at: 0 });
  const selectedIndex = options.findIndex(option => option.value === value);
  const selected = options[selectedIndex];

  const show = (index = Math.max(0, selectedIndex)) => { if (disabled || !options.length) return; setActive(index); setOpen(true); };
  const choose = (index: number) => { const option = options[index]; setOpen(false); if (option && option.value !== value) onChange(option.value); };

  // Position from the trigger rect; flip above when the space below is too small.
  useLayoutEffect(() => {
    if (!open) { setStyle(HIDDEN); return; }
    const place = () => {
      const button = trigger.current, list = menu.current; if (!button || !list) return;
      const rect = button.getBoundingClientRect();
      const below = window.innerHeight - rect.bottom - GAP - MARGIN, above = rect.top - GAP - MARGIN;
      const wanted = Math.min(list.scrollHeight, MAX_HEIGHT);
      const up = below < wanted && above > below;
      const maxHeight = Math.max(80, Math.min(MAX_HEIGHT, up ? above : below));
      const width = Math.min(Math.max(rect.width, list.offsetWidth), window.innerWidth - MARGIN * 2);
      const left = Math.max(MARGIN, Math.min(rect.left, window.innerWidth - MARGIN - width));
      setStyle({ position: 'fixed', left, minWidth: Math.min(rect.width, window.innerWidth - MARGIN * 2), maxWidth: window.innerWidth - MARGIN * 2, maxHeight,
        ...(up ? { bottom: window.innerHeight - rect.top + GAP } : { top: rect.bottom + GAP }) });
    };
    place();
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);
    return () => { window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true); };
  }, [open, options.length]);

  // Click or touch outside closes without changing the value.
  useEffect(() => {
    if (!open) return;
    const down = (event: PointerEvent) => { const target = event.target as Node; if (!trigger.current?.contains(target) && !menu.current?.contains(target)) setOpen(false); };
    document.addEventListener('pointerdown', down, true);
    return () => document.removeEventListener('pointerdown', down, true);
  }, [open]);

  useEffect(() => { if (open) document.getElementById(`${id}-${active}`)?.scrollIntoView({ block: 'nearest' }); }, [open, active, id]);
  useEffect(() => { if (disabled) setOpen(false); }, [disabled]);

  function typeahead(key: string) {
    const now = Date.now();
    typed.current = { text: (now - typed.current.at > 600 ? '' : typed.current.text) + key.toLowerCase(), at: now };
    const text = typed.current.text, start = open ? active : Math.max(0, selectedIndex);
    const order = options.map((_, i) => (start + (text.length === 1 ? 1 : 0) + i) % options.length);
    const match = order.find(i => options[i].label.toLowerCase().startsWith(text) || options[i].value.toLowerCase().startsWith(text));
    if (match !== undefined) show(match);
  }

  function keyDown(event: KeyboardEvent<HTMLButtonElement>) {
    const last = options.length - 1;
    const move = (index: number) => { event.preventDefault(); if (open) setActive(index); else show(index); };
    switch (event.key) {
      case 'ArrowDown': return move(open ? Math.min(last, active + 1) : Math.max(0, selectedIndex));
      case 'ArrowUp': return move(open ? Math.max(0, active - 1) : Math.max(0, selectedIndex));
      case 'Home': return move(0);
      case 'End': return move(last);
      case 'PageDown': return open ? move(Math.min(last, active + 8)) : undefined;
      case 'PageUp': return open ? move(Math.max(0, active - 8)) : undefined;
      case 'Enter': case ' ':
        if (event.key === ' ' && open && Date.now() - typed.current.at < 600) { event.preventDefault(); return typeahead(' '); }
        event.preventDefault();
        return open ? choose(active) : show();
      case 'Escape':
        // Only the popover closes; the surrounding dialog keeps its own Escape handler.
        if (open) { event.preventDefault(); event.stopPropagation(); setOpen(false); }
        return;
      case 'Tab': if (open) setOpen(false); return;
      default:
        if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) { event.preventDefault(); typeahead(event.key); }
    }
  }

  const tag = (option: SelectOption) => option.tag ? <span className={`select-tag${option.tone ? ` ${option.tone}` : ''}`}>{option.tag}</span> : null;
  return <>
    <button ref={trigger} type="button" role="combobox" className={`select-trigger ${className}`} aria-label={label} title={title} disabled={disabled}
      aria-haspopup="listbox" aria-expanded={open} aria-controls={open ? `${id}-list` : undefined} aria-activedescendant={open ? `${id}-${active}` : undefined}
      onClick={() => open ? setOpen(false) : show()} onKeyDown={keyDown} onKeyUp={event => { if (event.key === ' ') event.preventDefault(); }}>
      <span className="select-value">{selected ? <><span className="select-label">{selected.label}</span>{tag(selected)}</> : <span className="select-placeholder">{value || placeholder}</span>}</span>
      <ChevronDownIcon size={14} />
    </button>
    {open && createPortal(<ul ref={menu} id={`${id}-list`} role="listbox" aria-label={label} className="select-menu popover-menu" style={style} onMouseDown={event => event.preventDefault()}>
      {options.map((option, index) => <li key={option.value} id={`${id}-${index}`} role="option" aria-selected={index === selectedIndex} className={`popover-option${index === active ? ' active' : ''}`}
        onMouseMove={() => { if (index !== active) setActive(index); }} onClick={() => { choose(index); trigger.current?.focus(); }}>
        <span className="popover-check">{index === selectedIndex && <CheckIcon size={14} />}</span>
        <span className="popover-text"><span className="popover-label">{option.label}{tag(option)}</span>{option.description && <small>{option.description}</small>}</span>
      </li>)}
    </ul>, document.body)}
  </>;
}

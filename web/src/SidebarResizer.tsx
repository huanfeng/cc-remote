import { useEffect, useRef, useState } from 'react';
import { t } from './i18n';

export const SIDEBAR_DEFAULT = 248, SIDEBAR_MIN = 200, SIDEBAR_MAX = 480;
const STORAGE_KEY = 'cc-sidebar-width';
const clamp = (n: number, max: number) => Math.round(Math.min(max, Math.max(SIDEBAR_MIN, n)));

/** Sidebar width preference (desktop). The shown width never exceeds 40% of the viewport. */
export function useSidebarWidth() {
  const [preferred, setPreferred] = useState(() => {
    try { const n = Number(localStorage.getItem(STORAGE_KEY)); return n >= SIDEBAR_MIN && n <= SIDEBAR_MAX ? n : SIDEBAR_DEFAULT; } catch { return SIDEBAR_DEFAULT; }
  });
  const [viewport, setViewport] = useState(() => window.innerWidth);
  useEffect(() => {
    const resize = () => setViewport(window.innerWidth);
    window.addEventListener('resize', resize);
    return () => window.removeEventListener('resize', resize);
  }, []);
  const max = clamp(viewport * 0.4, SIDEBAR_MAX);
  const set = (n: number) => {
    const value = clamp(n, max);
    setPreferred(value);
    try { localStorage.setItem(STORAGE_KEY, String(value)); } catch {}
  };
  return { width: Math.min(preferred, max), max, set };
}

export function SidebarResizer({ width, max, onChange }: { width: number; max: number; onChange: (width: number) => void }) {
  const drag = useRef<{ x: number; width: number } | undefined>(undefined);
  const frame = useRef(0);
  const [dragging, setDragging] = useState(false);
  const end = () => {
    if (!drag.current) return;
    drag.current = undefined; cancelAnimationFrame(frame.current);
    setDragging(false); document.body.classList.remove('resizing-sidebar');
  };
  useEffect(() => end, []);
  return <div className={`sidebar-resizer${dragging ? ' dragging' : ''}`} role="separator" aria-orientation="vertical" aria-label={t('Resize session list (double-click to reset)', '调整会话列表宽度（双击恢复默认）')} aria-valuenow={width} aria-valuemin={SIDEBAR_MIN} aria-valuemax={max} tabIndex={0} title={t('Drag to resize, double-click to reset', '拖动调整宽度，双击恢复默认')}
    onPointerDown={event => {
      if (event.button !== 0) return;
      event.preventDefault();
      event.currentTarget.setPointerCapture(event.pointerId);
      drag.current = { x: event.clientX, width }; setDragging(true); document.body.classList.add('resizing-sidebar');
    }}
    onPointerMove={event => {
      const start = drag.current; if (!start) return;
      const next = start.width + event.clientX - start.x;
      cancelAnimationFrame(frame.current);
      frame.current = requestAnimationFrame(() => onChange(next));
    }}
    onPointerUp={end} onPointerCancel={end} onLostPointerCapture={end}
    onDoubleClick={() => onChange(SIDEBAR_DEFAULT)}
    onKeyDown={event => {
      const step = { ArrowLeft: -16, ArrowRight: 16 }[event.key];
      if (step) { event.preventDefault(); onChange(width + step); }
      else if (event.key === 'Home') { event.preventDefault(); onChange(SIDEBAR_MIN); }
      else if (event.key === 'End') { event.preventDefault(); onChange(max); }
    }} />;
}

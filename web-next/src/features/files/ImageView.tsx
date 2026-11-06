// An image that zooms by itself, not the page: pinch, wheel or double-click
// (double-tap) zooms at that point, a drag moves the zoomed image. Logic from
// the old web/src/FileViewer.tsx.
import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { Minus, Plus } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { useIsMobile } from '@/lib/hooks';

// Zoom is relative to the image fitted to the viewer.
const MAX_ZOOM = 10;
type View = { scale: number; x: number; y: number };
const FIT: View = { scale: 1, x: 0, y: 0 };

export function ImageView({ src, alt }: { src: string; alt: string }) {
  const mobile = useIsMobile();
  const box = useRef<HTMLDivElement>(null);
  const img = useRef<HTMLImageElement>(null);
  const [view, setViewState] = useState<View>(FIT);
  // The latest view at once: several wheel events can come before a render.
  const viewRef = useRef(view);
  const setView = useCallback((next: View) => { viewRef.current = next; setViewState(next); }, []);
  const [moving, setMoving] = useState(false);
  const [natural, setNatural] = useState(0);
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const start = useRef<{ view: View; x: number; y: number; dist: number } | undefined>(undefined);
  const lastTap = useRef({ time: 0, x: 0, y: 0 });
  const touched = useRef(false);
  const pinched = useRef(false);

  // Keeps the zoomed image over the box: no dragging it out of sight.
  const clamp = useCallback((scale: number, x: number, y: number): View => {
    const b = box.current, i = img.current;
    if (!b || !i || scale <= 1) return FIT;
    const maxX = Math.max(0, (i.offsetWidth * scale - b.clientWidth) / 2);
    const maxY = Math.max(0, (i.offsetHeight * scale - b.clientHeight) / 2);
    return { scale, x: Math.min(maxX, Math.max(-maxX, x)), y: Math.min(maxY, Math.max(-maxY, y)) };
  }, []);
  const fromCenter = (x: number, y: number) => {
    const r = box.current!.getBoundingClientRect();
    return { x: x - r.left - r.width / 2, y: y - r.top - r.height / 2 };
  };
  // Zooms from a view to scale, keeping the image point at `at` under `to`.
  const zoom = useCallback((from: View, scale: number, at: { x: number; y: number }, to = at) => {
    scale = Math.min(MAX_ZOOM, Math.max(1, scale));
    const k = scale / from.scale;
    setView(clamp(scale, to.x - (at.x - from.x) * k, to.y - (at.y - from.y) * k));
  }, [clamp, setView]);

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    // React listens to wheel passively; a zoom must keep the page from scrolling.
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      const r = el.getBoundingClientRect();
      const at = { x: event.clientX - r.left - r.width / 2, y: event.clientY - r.top - r.height / 2 };
      const step = event.deltaMode === 1 ? event.deltaY * 33 : event.deltaY;
      zoom(viewRef.current, viewRef.current.scale * Math.exp(-step * (event.ctrlKey ? 0.01 : 0.002)), at);
    };
    const stop = (event: Event) => event.preventDefault(); // Safari's own pinch zoom
    el.addEventListener('wheel', onWheel, { passive: false });
    el.addEventListener('gesturestart', stop);
    return () => { el.removeEventListener('wheel', onWheel); el.removeEventListener('gesturestart', stop); };
  }, [zoom]);

  const begin = () => {
    const [a, b] = [...pointers.current.values()];
    if (!a) { start.current = undefined; setMoving(false); return; }
    const mid = b ? { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 } : a;
    start.current = { view: viewRef.current, x: mid.x, y: mid.y, dist: b ? Math.hypot(a.x - b.x, a.y - b.y) : 0 };
    setMoving(true);
  };
  const toggle = (x: number, y: number) => {
    if (viewRef.current.scale > 1) setView(FIT);
    else zoom(FIT, 2.5, fromCenter(x, y));
  };
  const onPointerDown = (event: ReactPointerEvent) => {
    if (event.button !== 0) return;
    touched.current = event.pointerType !== 'mouse';
    event.currentTarget.setPointerCapture(event.pointerId);
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    pinched.current = pointers.current.size > 1;
    begin();
  };
  const onPointerMove = (event: ReactPointerEvent) => {
    if (!pointers.current.has(event.pointerId) || !start.current) return;
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    const s = start.current;
    const [a, b] = [...pointers.current.values()];
    if (b) {
      const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
      zoom(s.view, s.view.scale * Math.hypot(a.x - b.x, a.y - b.y) / s.dist, fromCenter(s.x, s.y), fromCenter(mid.x, mid.y));
    } else if (s.view.scale > 1) {
      setView(clamp(s.view.scale, s.view.x + a.x - s.x, s.view.y + a.y - s.y));
    }
  };
  const onPointerUp = (event: ReactPointerEvent) => {
    const s = start.current;
    const wasOne = pointers.current.size === 1;
    if (!pointers.current.delete(event.pointerId)) return;
    begin();
    // Two taps in a row on a touch screen zoom in there or back out (a mouse has dblclick).
    if (touched.current && wasOne && !pinched.current && s && Math.hypot(event.clientX - s.x, event.clientY - s.y) < 10) {
      const last = lastTap.current, now = event.timeStamp;
      if (now - last.time < 300 && Math.hypot(event.clientX - last.x, event.clientY - last.y) < 30) {
        lastTap.current = { time: 0, x: 0, y: 0 };
        toggle(event.clientX, event.clientY);
      } else lastTap.current = { time: now, x: event.clientX, y: event.clientY };
    }
  };
  const step = (factor: number) => zoom(view, view.scale * factor, { x: 0, y: 0 });
  const shown = natural && img.current ? Math.round(img.current.offsetWidth * view.scale / natural * 100) : 0;

  return (
    <div className="relative flex h-full min-h-0 flex-col">
      <div
        ref={box}
        className={cn(
          'flex min-h-0 flex-1 touch-none items-center justify-center overflow-hidden p-4 select-none',
          view.scale > 1 ? (moving ? 'cursor-grabbing' : 'cursor-grab') : 'cursor-zoom-in',
        )}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerUp}
        onDoubleClick={event => { if (!touched.current) toggle(event.clientX, event.clientY); }}
      >
        <img
          ref={img}
          src={src}
          alt={alt}
          draggable={false}
          onLoad={event => setNatural(event.currentTarget.naturalWidth)}
          className={cn('max-h-full max-w-full object-contain will-change-transform', !moving && 'transition-transform duration-100')}
          style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}
        />
      </div>
      <div role="toolbar" aria-label={t('Zoom', '缩放')} className="absolute bottom-4 left-1/2 flex -translate-x-1/2 items-center gap-0.5 rounded-lg border border-border/60 bg-popover p-0.5 shadow-sm">
        <Button variant="ghost" size="icon-sm" className={cn(mobile && 'size-9')} aria-label={t('Zoom out', '缩小')} disabled={view.scale <= 1} onClick={() => step(1 / 1.5)}><Minus /></Button>
        <Button variant="ghost" size="sm" className={cn('min-w-14 text-ui-sm tabular-nums', mobile && 'h-9')} disabled={view.scale === 1} onClick={() => setView(FIT)}>
          {shown ? `${shown}%` : t('Fit', '适应')}
        </Button>
        <Button variant="ghost" size="icon-sm" className={cn(mobile && 'size-9')} aria-label={t('Zoom in', '放大')} disabled={view.scale >= MAX_ZOOM} onClick={() => step(1.5)}><Plus /></Button>
      </div>
    </div>
  );
}

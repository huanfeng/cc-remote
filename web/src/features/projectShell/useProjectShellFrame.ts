import { useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type RefObject } from 'react';
import { adjustShellFrame, fitShellFrame, type FrameHandle, type ShellBounds, type ShellFrame } from './projectShellFrame';

const storageKey = 'cc-project-shell-frame';
const emptyBounds = { width: 0, height: 0 };

function readFrame(): ShellFrame | null {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) || 'null') as ShellFrame | null;
    if (saved && [saved.x, saved.y, saved.width, saved.height].every(Number.isFinite)) return saved;
  } catch { /* Storage is optional. */ }
  return null;
}

export function useProjectShellFrame(region: RefObject<HTMLElement | null>, enabled: boolean) {
  const [saved, setSaved] = useState(readFrame);
  const [draft, setDraft] = useState<ShellFrame | null>(null);
  const [bounds, setBounds] = useState<ShellBounds>(emptyBounds);
  const cancelDrag = useRef<() => void>(null);

  useLayoutEffect(() => {
    const parent = region.current?.parentElement;
    if (!enabled || !parent) return;
    const measure = () => {
      const { clientWidth: width, clientHeight: height } = parent;
      setBounds(previous => previous.width === width && previous.height === height ? previous : { width, height });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(parent);
    return () => {
      observer.disconnect();
      cancelDrag.current?.();
    };
  }, [enabled, region]);

  const commit = (frame: ShellFrame | null) => {
    setSaved(frame);
    try {
      if (frame) localStorage.setItem(storageKey, JSON.stringify(frame));
      else localStorage.removeItem(storageKey);
    } catch { /* Keep using the in-memory frame. */ }
  };

  const frame = enabled && bounds.width && bounds.height ? fitShellFrame(draft ?? saved, bounds) : null;
  const handlers = (handle: FrameHandle) => ({
    onPointerDown(event: PointerEvent<HTMLElement>) {
      const parent = region.current?.parentElement;
      if (!frame || !parent || cancelDrag.current || event.button !== 0) return;
      if (handle === 'move' && event.target instanceof Element && event.target.closest('button, a')) return;
      event.preventDefault();
      const start = { pointerId: event.pointerId, x: event.clientX, y: event.clientY };
      let latest: ShellFrame | undefined;
      const move = (pointer: WindowEventMap['pointermove']) => {
        if (pointer.pointerId !== start.pointerId) return;
        const bounds = { width: parent.clientWidth, height: parent.clientHeight };
        latest = adjustShellFrame(fitShellFrame(frame, bounds), handle, pointer.clientX - start.x, pointer.clientY - start.y, bounds);
        setDraft(latest);
      };
      const cleanup = () => {
        cancelDrag.current = null;
        window.removeEventListener('pointermove', move, true);
        window.removeEventListener('pointerup', finish, true);
        window.removeEventListener('pointercancel', finish, true);
        window.removeEventListener('blur', finish);
      };
      const finish = (pointer: WindowEventMap['pointerup'] | WindowEventMap['blur']) => {
        if ('pointerId' in pointer && pointer.pointerId !== start.pointerId) return;
        cleanup();
        if (latest) commit(latest);
        setDraft(null);
      };
      // Listen on the workspace window so a fast pointer can leave the thin
      // resize handle or lose capture while the panel is being redrawn.
      cancelDrag.current = cleanup;
      window.addEventListener('pointermove', move, true);
      window.addEventListener('pointerup', finish, true);
      window.addEventListener('pointercancel', finish, true);
      window.addEventListener('blur', finish);
    },
    onKeyDown(event: KeyboardEvent<HTMLElement>) {
      if (!frame || event.target !== event.currentTarget) return;
      const step = event.shiftKey ? 40 : 10;
      let dx = 0;
      let dy = 0;
      switch (event.key) {
        case 'ArrowLeft': dx = -step; break;
        case 'ArrowRight': dx = step; break;
        case 'ArrowUp': dy = -step; break;
        case 'ArrowDown': dy = step; break;
        default: return;
      }
      event.preventDefault();
      commit(adjustShellFrame(frame, handle, dx, dy, bounds));
    },
  });

  return { frame, handlers, reset: () => commit(null) };
}

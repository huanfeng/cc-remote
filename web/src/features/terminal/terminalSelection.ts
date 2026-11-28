import type { Terminal } from '@xterm/xterm';

export function watchTerminalSelection(
  element: HTMLElement,
  terminal: Pick<Terminal, 'onSelectionChange'>,
  enabled: () => boolean,
  copy: () => void,
) {
  let version = 0;
  let gesture: { version: number; end: 'mouseup' | 'touchend' } | undefined;
  const selection = terminal.onSelectionChange(() => { version++; });
  const startMouse = (event: MouseEvent) => {
    if (event.button === 0) gesture = { version, end: 'mouseup' };
  };
  const startTouch = () => { gesture = { version, end: 'touchend' }; };
  const finish = (event: MouseEvent | TouchEvent) => {
    if (gesture?.end !== event.type || ('button' in event && event.button !== 0)) return;
    const changed = gesture.version !== version;
    gesture = undefined;
    if (changed && enabled()) copy();
  };
  const cancel = () => { gesture = undefined; };
  element.addEventListener('mousedown', startMouse, true);
  element.addEventListener('touchstart', startTouch, { capture: true, passive: true });
  // xterm finishes its selection on document mouseup. The window listener
  // runs afterwards, still inside the user gesture for the clipboard.
  window.addEventListener('mouseup', finish);
  window.addEventListener('touchend', finish);
  window.addEventListener('touchcancel', cancel);
  window.addEventListener('blur', cancel);
  return () => {
    selection.dispose();
    element.removeEventListener('mousedown', startMouse, true);
    element.removeEventListener('touchstart', startTouch, true);
    window.removeEventListener('mouseup', finish);
    window.removeEventListener('touchend', finish);
    window.removeEventListener('touchcancel', cancel);
    window.removeEventListener('blur', cancel);
  };
}

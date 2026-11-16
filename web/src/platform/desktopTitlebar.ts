import { getCurrentWindow } from '@tauri-apps/api/window';
import type { DesktopPlatform } from './desktop';

// Tabs keep their HTML drag-to-reorder behavior; controls keep their clicks.
const interactive = 'button, a, input, select, textarea, label, summary, [contenteditable]:not([contenteditable="false"]), [role="button"], [role="tab"], [draggable="true"]';

/** Bind directly to the shell header instead of relying on injected drag-region handling. */
export function installDesktopTitlebar(header: HTMLElement, platform: DesktopPlatform) {
  const win = getCurrentWindow();
  let doubleClick: { x: number; y: number } | undefined;
  const draggable = (event: MouseEvent) => event.button === 0
    && (event.detail === 1 || event.detail === 2)
    && event.target instanceof Element && !event.target.closest(interactive);
  const maximize = () => { void win.toggleMaximize().catch(error => console.error('Could not maximize window', error)); };
  const down = (event: MouseEvent) => {
    doubleClick = undefined;
    if (!draggable(event)) return;
    if (platform === 'mac' && event.detail === 2) {
      doubleClick = { x: event.clientX, y: event.clientY };
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    if (event.detail === 2) maximize();
    else void win.startDragging().catch(error => console.error('Could not drag window', error));
  };
  const up = (event: MouseEvent) => {
    const origin = doubleClick;
    doubleClick = undefined;
    // Match the macOS title bar: moving after the second press cancels maximization.
    if (origin && draggable(event) && event.detail === 2 && event.clientX === origin.x && event.clientY === origin.y) maximize();
  };
  header.addEventListener('mousedown', down);
  header.addEventListener('mouseup', up);
  return () => {
    header.removeEventListener('mousedown', down);
    header.removeEventListener('mouseup', up);
  };
}

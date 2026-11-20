export interface ShellFrame { x: number; y: number; width: number; height: number }
export interface ShellBounds { width: number; height: number }
export type FrameHandle = 'move' | 'n' | 'e' | 's' | 'w' | 'ne' | 'se' | 'sw' | 'nw';

const MIN_WIDTH = 340;
const MIN_HEIGHT = 180;
const EDGE = 12;

const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(Math.round(value), max));

export function fitShellFrame(frame: ShellFrame | null, bounds: ShellBounds): ShellFrame {
  const width = clamp(frame?.width ?? Math.min(bounds.width * 0.82, 1080), Math.min(MIN_WIDTH, bounds.width), bounds.width);
  const height = clamp(frame?.height ?? Math.min(bounds.height * 0.62, 480), Math.min(MIN_HEIGHT, bounds.height), bounds.height);
  return {
    x: clamp(frame?.x ?? bounds.width - width - EDGE, 0, bounds.width - width),
    y: clamp(frame?.y ?? bounds.height - height - EDGE, 0, bounds.height - height),
    width,
    height,
  };
}

export function adjustShellFrame(frame: ShellFrame, handle: FrameHandle, dx: number, dy: number, bounds: ShellBounds): ShellFrame {
  if (handle === 'move') return fitShellFrame({ ...frame, x: frame.x + dx, y: frame.y + dy }, bounds);

  const minWidth = Math.min(MIN_WIDTH, bounds.width);
  const minHeight = Math.min(MIN_HEIGHT, bounds.height);
  const left = handle.includes('w') ? clamp(frame.x + dx, 0, frame.x + frame.width - minWidth) : frame.x;
  const top = handle.includes('n') ? clamp(frame.y + dy, 0, frame.y + frame.height - minHeight) : frame.y;
  const right = handle.includes('e') ? clamp(frame.x + frame.width + dx, frame.x + minWidth, bounds.width) : frame.x + frame.width;
  const bottom = handle.includes('s') ? clamp(frame.y + frame.height + dy, frame.y + minHeight, bounds.height) : frame.y + frame.height;
  return { x: left, y: top, width: right - left, height: bottom - top };
}

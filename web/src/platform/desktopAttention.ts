import { getCurrentWindow } from '@tauri-apps/api/window';
import { Image } from '@tauri-apps/api/image';
import type { AttentionCounts } from '../domain/attention.ts';

/** A host may appear in more than one restored view, but its tasks count once. */
export function aggregateDesktopAttention(tabs: { hostId: string; waiting: number; done?: number; phase?: string }[]): AttentionCounts {
  const hosts = new Map<string, { waiting: number; done: number }>();
  const count = (value: number | undefined) => Number.isFinite(value) && value! > 0 ? Math.floor(value!) : 0;
  for (const tab of tabs) {
    if (tab.phase === 'signed-out') continue;
    const previous = hosts.get(tab.hostId);
    hosts.set(tab.hostId, { waiting: Math.max(previous?.waiting ?? 0, count(tab.waiting)), done: Math.max(previous?.done ?? 0, count(tab.done)) });
  }
  let waiting = 0, done = 0;
  for (const host of hosts.values()) { waiting += host.waiting; done += host.done; }
  return { waiting, done, total: waiting + done };
}

const taskbarBadgeSize = 16;

/** Windows overlays are 16 logical pixels at 96 DPI. Rasterize at the window's scale. */
export function renderTaskbarBadge(counts: AttentionCounts, pixelRatio: number) {
  const size = Math.max(taskbarBadgeSize, Math.round(taskbarBadgeSize * pixelRatio));
  const canvas = document.createElement('canvas');
  canvas.width = canvas.height = size;
  const context = canvas.getContext('2d');
  if (!context) throw new Error('Could not create a taskbar badge canvas');
  const scale = size / taskbarBadgeSize;
  context.scale(scale, scale);

  const label = counts.total > 99 ? '99+' : String(counts.total);
  const singleDigit = label.length === 1;
  const inset = 0.5 / scale;
  const top = singleDigit ? inset : 1 + inset;
  context.roundRect(inset, top, 16 - 2 * inset, 16 - 2 * top, singleDigit ? 8 : 5);
  context.fillStyle = counts.waiting ? '#9a4705' : '#086747';
  context.fill();
  context.lineWidth = 1 / scale;
  context.strokeStyle = '#ffffffe0';
  context.stroke();

  const fontSize = singleDigit ? 12 : label.length === 2 ? 10 : 8;
  context.font = `700 ${fontSize}px "Segoe UI", Arial, sans-serif`;
  context.textAlign = 'center';
  const metrics = context.measureText(label);
  const baseline = 8 + (metrics.actualBoundingBoxAscent - metrics.actualBoundingBoxDescent) / 2;
  context.fillStyle = '#fff';
  context.fillText(label, 8, baseline, 13);
  return { pixels: new Uint8Array(context.getImageData(0, 0, size, size).data), size };
}

type BadgeImage = { close(): Promise<void> };
interface NativeAttention {
  image(counts: AttentionCounts): Promise<BadgeImage>;
  overlay(image?: BadgeImage): Promise<void>;
  badge(count?: number): Promise<void>;
}

// StrictMode can dispose and recreate the shell before an old IPC finishes.
// A module-wide queue also orders the old clear before the new controller.
let nativeWrites = Promise.resolve();

/** One writer prevents a slow old badge from overwriting newer counts or a clear. */
export function createDesktopAttention(platform: string, bridge?: NativeAttention) {
  const window = bridge ? undefined : getCurrentWindow();
  const native: NativeAttention = bridge ?? {
    image: counts => {
      const { pixels, size } = renderTaskbarBadge(counts, globalThis.devicePixelRatio || 1);
      return Image.new(pixels, size, size);
    },
    overlay: image => window!.setOverlayIcon(image as Image | undefined),
    badge: count => window!.setBadgeCount(count),
  };
  let writes = Promise.resolve(), previous = '', disposed = false;
  const write = (counts: AttentionCounts) => {
    writes = nativeWrites = nativeWrites.catch(() => {}).then(async () => {
      const key = `${counts.waiting}:${counts.done}`;
      if (key === previous) return;
      if (platform === 'windows') {
        const image = counts.total ? await native.image(counts) : undefined;
        try { await native.overlay(image); } finally { await image?.close(); }
      } else await native.badge(counts.total || undefined);
      previous = key;
    }).catch(error => { console.warn('Could not update desktop attention badge', error); });
    return writes;
  };
  return {
    update(counts: AttentionCounts) { return disposed ? writes : write(counts); },
    dispose() { disposed = true; return write({ waiting: 0, done: 0, total: 0 }); },
  };
}

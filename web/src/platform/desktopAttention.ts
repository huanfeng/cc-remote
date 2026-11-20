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

const glyphs: Record<string, string[]> = {
  '0': ['111', '101', '101', '101', '111'], '1': ['010', '110', '010', '010', '111'],
  '2': ['111', '001', '111', '100', '111'], '3': ['111', '001', '111', '001', '111'],
  '4': ['101', '101', '111', '001', '001'], '5': ['111', '100', '111', '001', '111'],
  '6': ['111', '100', '111', '101', '111'], '7': ['111', '001', '001', '001', '001'],
  '8': ['111', '101', '111', '101', '111'], '9': ['111', '101', '111', '001', '111'],
  '+': ['000', '010', '111', '010', '000'],
};

/** Raw RGBA avoids icon decoding features or replacing the user's app icon. */
export function taskbarBadgePixels(counts: AttentionCounts): Uint8Array {
  const pixels = new Uint8Array(16 * 16 * 4);
  const color = counts.waiting ? [217, 119, 6] : [22, 163, 74];
  for (let y = 0; y < 16; y++) for (let x = 0; x < 16; x++) {
    if ((x - 7.5) ** 2 + (y - 7.5) ** 2 > 7.5 ** 2) continue;
    pixels.set([...color, 255], (y * 16 + x) * 4);
  }
  const label = counts.total > 99 ? '99+' : String(counts.total);
  const scale = label.length === 1 ? 2 : 1;
  const left = Math.floor((16 - (label.length * 4 - 1) * scale) / 2), top = Math.floor((16 - 5 * scale) / 2);
  for (let i = 0; i < label.length; i++) for (let y = 0; y < 5; y++) for (let x = 0; x < 3; x++) {
    if (glyphs[label[i]][y][x] !== '1') continue;
    for (let dy = 0; dy < scale; dy++) for (let dx = 0; dx < scale; dx++) pixels.set([255, 255, 255, 255], ((top + y * scale + dy) * 16 + left + (i * 4 + x) * scale + dx) * 4);
  }
  return pixels;
}

type BadgeImage = { close(): Promise<void> };
interface NativeAttention {
  image(pixels: Uint8Array): Promise<BadgeImage>;
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
    image: pixels => Image.new(pixels, 16, 16),
    overlay: image => window!.setOverlayIcon(image as Image | undefined),
    badge: count => window!.setBadgeCount(count),
  };
  let writes = Promise.resolve(), previous = '', disposed = false;
  const write = (counts: AttentionCounts) => {
    writes = nativeWrites = nativeWrites.catch(() => {}).then(async () => {
      const key = `${counts.waiting}:${counts.done}`;
      if (key === previous) return;
      if (platform === 'windows') {
        const image = counts.total ? await native.image(taskbarBadgePixels(counts)) : undefined;
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

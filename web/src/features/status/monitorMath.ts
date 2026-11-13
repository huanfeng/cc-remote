import type { MonitorHistory, MonitorWindow } from '../../backend/types.ts';
import { lineRuns, type ChartBox } from './usageMath.ts';

export const windowKey = (w: MonitorWindow) => JSON.stringify([w.name, w.windowMinutes ?? 0]);

/** Split explicitly failed/missing readings as well as sleep/offline gaps. */
export function monitorRuns(history: MonitorHistory, key: string, start: number, end: number, box: ChartBox, intervalMinutes: number) {
  const groups: { at: string; value: number }[][] = [[]];
  let previousAt = 0, previousInterval = intervalMinutes;
  for (const point of history.points) {
    const window = point.error ? undefined : point.windows?.find(w => windowKey(w) === key);
    if (!window || !Number.isFinite(window.percent)) { if (groups.at(-1)!.length) groups.push([]); continue; }
    const at = Date.parse(point.at), interval = point.intervalMinutes || intervalMinutes;
    if (!Number.isFinite(at)) continue;
    if (groups.at(-1)!.length && at - previousAt > Math.max(interval, previousInterval) * 1.6 * 60_000) groups.push([]);
    groups.at(-1)!.push({ at: point.at, value: window.percent });
    previousAt = at; previousInterval = interval;
  }
  return groups.flatMap(points => lineRuns(points, start, end, box, Infinity));
}

export function historyWindows(history: MonitorHistory): MonitorWindow[] {
  const all = new Map<string, MonitorWindow>();
  for (const point of history.points) for (const window of point.windows ?? []) all.set(windowKey(window), window);
  return [...all.values()];
}

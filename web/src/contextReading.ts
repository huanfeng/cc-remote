import { readCodexUsage, type CodexEvent, type TokenCounts } from './codexStatus';
import type { Provider } from './providers';

type ContextLive = { contextTokens?: number; contextSize?: number; contextPercent?: number };
export type ContextReading = { percent?: number; tokens?: number; capacity?: number; total?: TokenCounts; last?: TokenCounts };
const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;

/** Cumulative session tokens are never a measure of the current context window. */
export function contextReading(provider: Provider, live?: ContextLive, events: readonly CodexEvent[] = []): ContextReading {
  if (provider === 'codex') {
    const hasReport = events.some(event => event.kind === 'status' && !!event.payload && typeof event.payload === 'object' && Object.prototype.hasOwnProperty.call(event.payload, 'tokenUsage'));
    if (hasReport) {
      const usage = readCodexUsage(events);
      const tokens = usage?.last?.totalTokens, capacity = usage?.modelContextWindow;
      return { total: usage?.total, last: usage?.last, capacity,
        tokens, percent: count(tokens) && capacity && capacity > 0 ? Math.min(100, tokens / capacity * 100) : undefined };
    }
  }
  const capacity = live?.contextSize, tokens = live?.contextTokens, percent = live?.contextPercent;
  if (!count(capacity) || capacity === 0 || !count(tokens)) return {};
  return { capacity, tokens, percent: typeof percent === 'number' && Number.isFinite(percent) && percent >= 0 ? Math.min(100, percent) : Math.min(100, tokens / capacity * 100) };
}

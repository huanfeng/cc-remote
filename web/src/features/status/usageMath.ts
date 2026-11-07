// Pure helpers for subscription usage, host status and their charts. No React,
// no aliases: node tests import this file directly.
import { t, locale } from '../../lib/i18n.ts';
import type { Provider, UsageReply, UsageWindow } from '../../backend/types.ts';

export type Level = 'ok' | 'warn' | 'high';

/** A reading that can be drawn: a finite percentage between 0 and 100. */
export const validWindow = (w?: UsageWindow): w is UsageWindow =>
  !!w && Number.isFinite(w.percent) && w.percent >= 0 && w.percent <= 100;

/** The host's severity wins; otherwise 75 % warns and 90 % is high. */
export function usageLevel(w: UsageWindow): Level {
  if (w.severity && w.severity !== 'normal') return w.severity === 'warning' ? 'warn' : 'high';
  return w.percent >= 90 ? 'high' : w.percent >= 75 ? 'warn' : 'ok';
}

/** The phone ring's tone: 80 % warns and 95 % is high, unless the host marks it worse. */
export function ringLevel(w: UsageWindow): Level {
  const own: Level = w.percent >= 95 ? 'high' : w.percent >= 80 ? 'warn' : 'ok';
  const host = w.severity && w.severity !== 'normal' ? usageLevel(w) : 'ok';
  const rank = { ok: 0, warn: 1, high: 2 } as const;
  return rank[host] > rank[own] ? host : own;
}

/** The fullest valid window, or undefined. */
export function fullest(windows: (UsageWindow | undefined)[]): UsageWindow | undefined {
  return windows.filter(validWindow).reduce<UsageWindow | undefined>((a, w) => (!a || w.percent > a.percent ? w : a), undefined);
}

export const percentText = (w?: UsageWindow) => (validWindow(w) ? `${Math.round(w.percent)}%` : '—');

/** Time left until iso, compact and localised: "3d 4h", "2h 15m", "9m"; '' when unknown. */
export function countdown(iso: string | undefined, now = Date.now()): string {
  const at = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(at)) return '';
  const minutes = Math.max(0, Math.ceil((at - now) / 60_000));
  const d = Math.floor(minutes / 1440), h = Math.floor((minutes % 1440) / 60), m = minutes % 60;
  if (d) return t(`${d}d ${h}h`, `${d} 天 ${h} 小时`);
  if (h) return t(`${h}h ${m}m`, `${h} 小时 ${m} 分`);
  return t(`${m}m`, `${m} 分钟`);
}

/** "resets in 2h 15m", or '' when the reset time is unknown. */
export function resetsIn(iso: string | undefined, now = Date.now()): string {
  const left = countdown(iso, now);
  return left ? t(`resets in ${left}`, `${left}后重置`) : '';
}

/** "just now", "5 min ago", "3 h ago", else the local date and time. */
export function agoText(iso: string | undefined, now = Date.now()): string {
  const at = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(at)) return '';
  const s = Math.max(0, (now - at) / 1000);
  if (s < 60) return t('just now', '刚刚');
  if (s < 3600) return t(`${Math.floor(s / 60)} min ago`, `${Math.floor(s / 60)} 分钟前`);
  if (s < 86400) return t(`${Math.floor(s / 3600)} h ago`, `${Math.floor(s / 3600)} 小时前`);
  return new Date(at).toLocaleString(locale);
}

/** A duration in seconds as "3d 4h", "2h 5m", "4m 10s". */
export function durationText(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  if (d) return t(`${d}d ${h}h`, `${d} 天 ${h} 小时`);
  if (h) return t(`${h}h ${m}m`, `${h} 小时 ${m} 分`);
  if (m) return t(`${m}m ${s % 60}s`, `${m} 分 ${s % 60} 秒`);
  return t(`${s}s`, `${s} 秒`);
}

/** 1234 → "1.2K", 48_200_000 → "48.2M". */
export function compactNumber(n: number): string {
  if (!Number.isFinite(n)) return '—';
  const abs = Math.abs(n);
  const fmt = (v: number, unit: string) => `${v >= 100 ? Math.round(v) : Number(v.toFixed(1))}${unit}`;
  if (abs >= 1e9) return fmt(n / 1e9, 'B');
  if (abs >= 1e6) return fmt(n / 1e6, 'M');
  if (abs >= 1e3) return fmt(n / 1e3, 'K');
  return String(Math.round(n));
}

export const costText = (usd: number) => (Number.isFinite(usd) ? `$${usd >= 100 ? usd.toFixed(0) : usd.toFixed(2)}` : '—');

export interface QuotaWindow { key: string; label: string; title: string; window?: UsageWindow }

/** The windows a provider reports, in display order: Claude 5h + 7d; Codex its own windows. */
export function quotaWindows(usage: UsageReply | undefined, provider: Provider): QuotaWindow[] {
  if (provider === 'claude') {
    return [
      { key: '5h', label: '5h', title: t('5-hour limit', '5 小时额度'), window: usage?.usage?.fiveHour },
      { key: '7d', label: '7d', title: t('Weekly limit', '每周额度'), window: usage?.usage?.weekly },
    ];
  }
  const windows = usage?.usage?.windows;
  if (!windows?.length) return [];
  return windows.map((w, i) => {
    const mins = w.windowMinutes;
    const label = mins === 10080 ? '7d'
      : mins && mins % 1440 === 0 ? `${mins / 1440}d`
      : mins && mins % 60 === 0 ? `${mins / 60}h`
      : mins ? `${mins}m`
      : w.name === 'secondary' ? t('Secondary', '次要') : t('Primary', '主要');
    const title = label === '7d' ? t('Weekly limit', '每周额度') : label === '5h' ? t('5-hour limit', '5 小时额度') : label;
    return { key: `${i}:${label}`, label, title, window: w };
  });
}

/**
 * Whether the header shows a meter at all. A non-subscription login has no
 * quota; an old host answering provider=codex with Claude's reading (no
 * native windows) is not Codex's quota, so it is hidden too.
 */
export function meterVisible(usage: UsageReply | undefined, provider: Provider): boolean {
  if (!usage) return false;
  if (!usage.available && !usage.error) return false;
  if (provider === 'codex' && !usage.usage?.windows) return false;
  return true;
}

/** A reading that is not current: marked stale by the host or the last read failed. */
export const usageStale = (u: UsageReply) => !!u.stale || !!u.error || !u.available;

export function staleText(u: UsageReply, provider: Provider): string {
  if (provider === 'codex' && u.error === 'expired') return t('Codex login expired; sign in again on the host.', 'Codex 登录已过期，请在宿主重新登录。');
  if (u.error === 'expired' && u.profile) return t('This OAuth token is no longer accepted; replace it in Settings.', '此 OAuth 令牌已失效，请在设置里更换。');
  if (u.error === 'expired') return t('The Claude Code login expired; it renews the next time Claude Code runs.', 'Claude Code 登录已过期，下次运行 Claude Code 时会自动续期。');
  if (u.error === 'rate-limited') return t('Too many requests; trying again later.', '请求过于频繁，稍后重试。');
  if (u.error) return t(`Could not update: ${u.error}`, `更新失败：${u.error}`);
  if (u.stale) return t('Usage is out of date.', '用量已过期。');
  return '';
}

export function unavailableText(u: UsageReply, providerLabel: string): string {
  if (u.reason === 'third-party') return t(`${providerLabel} uses ${u.provider || 'another provider'}; subscription usage applies to the official account only.`, `${providerLabel} 当前使用 ${u.provider || '第三方服务'}，订阅用量只适用于官方账号。`);
  if (u.reason === 'api-key') return t(`${providerLabel} uses an API key, billed per token; there are no subscription limits.`, `${providerLabel} 当前使用 API Key，按 token 计费，没有订阅额度。`);
  return t(`${providerLabel} is not signed in with a subscription on this host.`, `此宿主上的 ${providerLabel} 没有用订阅账号登录。`);
}

// ---------- Charts ----------

export interface ChartBox { width: number; height: number; padLeft: number; padRight: number; padTop: number; padBottom: number }

/**
 * A percentage series as polyline runs inside box. A gap longer than maxGapMs
 * (the host was off) breaks the line; each run is a list of [x, y].
 */
export function lineRuns(
  points: { at: string; value?: number }[], start: number, end: number, box: ChartBox, maxGapMs = 30 * 60_000,
): [number, number][][] {
  const span = Math.max(1, end - start);
  const innerW = box.width - box.padLeft - box.padRight, innerH = box.height - box.padTop - box.padBottom;
  const runs: [number, number][][] = [];
  let prev = -Infinity;
  for (const p of points) {
    if (p.value === undefined || !Number.isFinite(p.value)) continue;
    const at = Date.parse(p.at);
    if (!Number.isFinite(at) || at < start || at > end) continue;
    if (!runs.length || at - prev > maxGapMs) runs.push([]);
    const v = Math.min(100, Math.max(0, p.value));
    const x = box.padLeft + ((at - start) / span) * innerW;
    const y = box.padTop + (1 - v / 100) * innerH;
    runs[runs.length - 1].push([round1(x), round1(y)]);
    prev = at;
  }
  return runs;
}

const round1 = (n: number) => Math.round(n * 10) / 10;

/** A "nice" axis maximum ≥ max: 1, 2, 2.5, 5 or 10 times a power of ten (0 → 1). */
export function niceMax(max: number): number {
  if (!Number.isFinite(max) || max <= 0) return 1;
  const pow = 10 ** Math.floor(Math.log10(max));
  for (const step of [1, 2, 2.5, 5, 10]) if (step * pow >= max) return step * pow;
  return 10 * pow;
}

/** Bars for values in box: x, width and the top y of each, scaled to niceMax. */
export function barLayout(values: number[], box: ChartBox, gapRatio = 0.35) {
  const max = niceMax(Math.max(0, ...values));
  const innerW = box.width - box.padLeft - box.padRight, innerH = box.height - box.padTop - box.padBottom;
  const slot = values.length ? innerW / values.length : innerW;
  const width = Math.max(1, slot * (1 - gapRatio));
  const bars = values.map((v, i) => {
    const h = (Math.max(0, v) / max) * innerH;
    return { x: round1(box.padLeft + i * slot + (slot - width) / 2), y: round1(box.padTop + innerH - h), width: round1(width), height: round1(h) };
  });
  return { max, bars, slot };
}

/** Tick positions for the history chart: every 6 h for a day, else every day (thinned to ≤ 8). */
export function historyTicks(hours: number, end: number): number[] {
  if (hours <= 24) return [24, 18, 12, 6, 0].map(n => end - n * 3600_000);
  const days = Math.round(hours / 24);
  const step = Math.max(1, Math.ceil(days / 7));
  const out: number[] = [];
  for (let n = days; n >= 0; n -= step) out.push(end - n * 86_400_000);
  return out;
}

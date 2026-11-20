import type { ProfileUsageReply, ProfileUsageSummary } from '../../backend/types.ts';
import { t } from '../../lib/i18n.ts';
import { fetchedText, countdown, validWindow } from '../status/usageMath.ts';

/** Missing readings remain unknown; cached failures never look like fresh quota. */
export function supplierUsageDescription(usage: ProfileUsageSummary | undefined, now = Date.now()): string {
  if (!usage) return t('Quota unavailable', '用量暂不可用');
  const parts: string[] = [];
  const windows = Array.isArray(usage.windows) ? usage.windows.filter(window => window && typeof window === 'object') : [];
  if (usage.error) parts.push(usage.error === 'expired' ? t('Login expired', '登录已过期') : t('Update failed', '更新失败'));
  if (usage.stale || usage.error && windows.length) parts.push(t('Cached reading', '旧数据'));
  if (windows.length) {
    parts.push(t('Used', '已用'));
    for (const window of windows.slice(0, 2)) {
      const name = window.windowMinutes === 300 ? '5h' : window.windowMinutes === 10080 ? '7d' : window.name;
      const reset = countdown(window.resetsAt, now);
      parts.push(`${name} ${validWindow(window) ? `${Math.round(window.percent)}%` : '—'}${reset ? ` ↺${reset}` : ''}`);
    }
  } else if (!usage.error) {
    parts.push(usage.reason === 'no-login' ? t('Not signed in', '未登录')
      : usage.reason === 'api-key' || usage.reason === 'third-party' ? t('No subscription quota', '无订阅额度')
        : t('No reading yet', '尚无用量数据'));
  }
  if (windows.length || usage.fetchedAt) parts.push(fetchedText(usage.fetchedAt, now));
  if (usage.shared) parts.push(t('Shared quota', '共用额度'));
  return parts.join(' · ');
}

export function supplierUsageFor(usage: ProfileUsageReply | null | undefined, id: string): string | undefined {
  if (usage === null) return undefined; // optional endpoint on older hosts
  return usage === undefined ? t('Loading quota…', '正在读取用量…') : supplierUsageDescription(usage.profiles[id]);
}

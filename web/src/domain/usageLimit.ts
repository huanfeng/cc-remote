import type { ToolCall } from './toolGroups.ts';
import { t } from '../lib/i18n.ts';

export function usageLimitView(call: ToolCall, interaction?: { outcome: string; answers?: Record<string, string> }) {
  let result: { outcome?: string; option?: string } | null = {};
  try { result = JSON.parse(call.resultText); } catch { /* Pending calls have no result. */ }
  const labels: Record<string, string> = {
    '1': t('Stop and wait for the reset', '停止并等待额度重置'),
    '2': t('Resume automatically at reset', '额度重置后自动继续'),
    '3': t('Use extra usage credits', '使用额外用量额度'),
    '4': t('Upgrade plan', '升级套餐'),
  };
  const options: { id: string; label: string }[] = Array.isArray(call.input?.options)
    ? call.input.options.filter((option: any) => option && typeof option.id === 'string' && labels[option.id]).map((option: any) => ({ id: option.id, label: labels[option.id] })) : [];
  return {
    title: t('Usage limit reached', '用量已达上限'),
    reset: typeof call.input?.reset === 'string' ? call.input.reset : '',
    options,
    selected: String(result?.option ?? interaction?.answers?.option ?? ''),
    outcome: result?.outcome ?? interaction?.outcome,
    expired: (result?.outcome ?? interaction?.outcome) === 'expired',
  };
}

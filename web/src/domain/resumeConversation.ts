import type { ToolCall } from './toolGroups.ts';
import { t } from '../lib/i18n.ts';

/** A recorded terminal resume choice, including its original cost explanation. */
export function resumeConversationView(call: ToolCall, interaction?: { outcome: string; answers?: Record<string, string> }) {
  let result: { outcome?: string; option?: string } | null = {};
  try { result = JSON.parse(call.resultText); } catch { /* A pending call has no result. */ }
  const labels: Record<string, string> = {
    resume: t('Resume', '恢复对话'),
    new_conversation: t('Start a new conversation', '新开对话'),
    compact: t('Resume from summary', '从摘要恢复'),
  };
  const options: { id: string; label: string }[] = Array.isArray(call.input?.options)
    ? call.input.options.filter((option: any) => option && typeof option.id === 'string' && Object.hasOwn(labels, option.id))
      .map((option: any) => ({ id: option.id, label: labels[option.id] })) : [];
  const outcome = result?.outcome ?? interaction?.outcome;
  return {
    title: t('Resume this conversation?', '恢复此对话？'),
    message: typeof call.input?.message === 'string' ? call.input.message : '',
    options,
    selected: String(result?.option ?? interaction?.answers?.option ?? ''),
    outcome,
    expired: outcome === 'expired',
  };
}

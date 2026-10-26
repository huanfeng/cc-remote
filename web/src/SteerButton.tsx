import { t } from './i18n';

export function canSteer(info?: { features?: string[] }, session?: { provider?: string; mode?: string; status?: string; queued?: unknown[] }, native = false) {
  return !!info?.features?.includes('codexSteer') && !native && session?.provider === 'codex' && session.mode !== 'terminal' && session.status === 'running' && !session.queued?.length;
}

// A lost reply keeps the original operation available even after the turn ends.
export const needsSteerConfirmation = (held: { content: string } | undefined, text: string) => !!held && held.content === text.trim();

export function SteerButton({ supported, confirming = false, disabled, send }: { supported: boolean; confirming?: boolean; disabled: boolean; send: () => void }) {
  if (!supported) return null;
  return <button type="button" className="secondary" disabled={disabled} title={confirming ? t('Check the original submission using its send ID; do not send another message.', '使用原发送 ID 确认结果，不会重复提交消息。') : t('Add this instruction to the current task. It will not start another task if the current turn has ended. Text only.', '把这条指令追加到当前任务；若当前轮次已结束，不会启动新任务。仅支持文本。')} onClick={send}>{confirming ? t('Check additional instruction', '确认追加结果') : t('Add to current task', '追加到当前任务')}</button>;
}

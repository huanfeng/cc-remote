import type { ReactNode } from 'react';
import { t } from './i18n';

/** Primary send button with optional inline action (e.g. Codex steer). */
export function ComposerSend({ disabled, uploading, running, extra }: {
  disabled: boolean; uploading: boolean; running: boolean; sendKey?: string; onSendKey?: (value: string) => void;
  extra?: ReactNode;
}) {
  return <div className="composer-send">
    {extra}
    <button type="submit" className="primary" disabled={disabled} title={running ? t('Send after the current task', '当前任务结束后发送') : t('Send message', '发送消息')}>
      {uploading ? t('Uploading…', '上传中…') : t('Send ↑', '发送 ↑')}
    </button>
  </div>;
}

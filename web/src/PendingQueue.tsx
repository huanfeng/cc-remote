import { useState } from 'react';
import { AttachmentList, type Attachment } from './Attachments';
import { t } from './i18n';

export type PendingMessage = { id?: string; text: string; payload?: { attachments?: Attachment[] }; editable?: boolean; canSendNow?: boolean; delivery?: string };
export type QueueAction = 'send' | 'remove' | 'edit';

/** Accepted messages remain outside the transcript until the provider consumes them. */
export function PendingQueue({ items, token, perform }: { items: readonly PendingMessage[]; token: string; perform: (item: PendingMessage, action: QueueAction, text?: string) => Promise<void> }) {
  const [editing, setEditing] = useState<string>();
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  async function run(item: PendingMessage, action: QueueAction) {
    if (busy) return;
    setBusy(true); setError('');
    try { await perform(item, action, action === 'edit' ? text.trim() : undefined); if (action === 'edit') setEditing(undefined); }
    catch (error) { setError((error as Error).message); }
    finally { setBusy(false); }
  }
  if (!items.length && !error) return null;
  return <section className="pending-queue" aria-label={t('Pending messages', '待发送消息')}>
    <div className="pending-heading">{t('Pending · after the current task', '待发送 · 当前任务结束后处理')} <span>{items.length}</span></div>
    <div className="pending-items">{items.map((item, index) => <div className="pending-item" key={item.id || `legacy-${index}`}>
      <div className="pending-content">{editing === item.id && item.id ? <textarea aria-label={t('Edit pending message', '修改待发送消息')} value={text} disabled={busy} onChange={event => setText(event.target.value)} /> : <p>{item.text}</p>}
        {!!item.payload?.attachments?.length && <AttachmentList items={item.payload.attachments} token={token} />}
        {!item.editable && <small>{item.delivery === 'unknown' ? t('Delivery could not be confirmed. This message will not be sent again automatically; removing it only dismisses this notice.', '无法确认是否送达，不会自动重发；移除仅清除此提示。') : item.delivery === 'pending' ? t('Confirming delivery…', '正在确认送达…') : t('Already delivered; waiting for the provider to process it.', '已投递，等待提供方处理。')}</small>}
      </div>
      <div className="pending-actions">{editing === item.id && item.id ? <>
        <button type="button" disabled={busy || (!text.trim() && !item.payload?.attachments?.length)} onClick={() => void run(item, 'edit')}>{t('Save', '保存')}</button>
        <button type="button" disabled={busy} onClick={() => setEditing(undefined)}>{t('Cancel', '取消')}</button>
      </> : <>
        {item.canSendNow && <button type="button" disabled={busy} onClick={() => void run(item, 'send')}>{t('Send now', '立即发送')}</button>}
        {item.editable && item.id && <button type="button" disabled={busy} onClick={() => { setEditing(item.id); setText(item.text); setError(''); }}>{t('Edit', '修改')}</button>}
        {(item.editable || item.delivery === 'unknown') && item.id && <button type="button" disabled={busy} aria-label={item.delivery === 'unknown' ? t('Dismiss delivery notice', '清除投递提示') : t('Remove pending message', '移除待发送消息')} onClick={() => void run(item, 'remove')}>×</button>}
      </>}</div>
    </div>)}</div>
    {error && <p className="pending-error" role="alert">{error} <button type="button" aria-label={t('Dismiss queue error', '关闭队列错误提示')} onClick={() => setError('')}>×</button></p>}
  </section>;
}

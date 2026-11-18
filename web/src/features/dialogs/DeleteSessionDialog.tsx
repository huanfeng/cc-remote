import { useState } from 'react';
import { BackendError, useBackend } from '@/backend/backend';
import type { Session } from '@/backend/types';
import { select, useApp } from '@/store/app';
import { t } from '@/lib/i18n';
import { ConfirmDialog, ErrorText, errorMessage } from './common';

/** Shared by the session menu and the management section. */
export function DeleteSessionDialog({ session: s, open, onOpenChange, onDeleted }: {session: Session; open: boolean; onOpenChange: (open: boolean) => void; onDeleted?: () => void}) {
  const backend = useBackend();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // The host refuses (409) to delete a busy session without stop; then the dialog asks to stop first.
  const [mustStop, setMustStop] = useState(false);
  const active = mustStop || s.status === 'running' || s.status === 'waiting' || (s.activeTasks ?? 0) > 0 || (s.mode === 'terminal' && !!s.online);

  const remove = async () => {
    if (busy) return;
    setBusy(true); setError('');
    try {
      await backend.deleteSession(s.id, active ? { stop: true } : undefined);
      onOpenChange(false);
      const sel = useApp.getState().selection;
      if (sel?.kind === 'session' && sel.id === s.id) select(null);
      onDeleted?.();
    } catch (e) {
      if (e instanceof BackendError && e.status === 409 && !active) setMustStop(true);
      else setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
      <ConfirmDialog
        open={open}
        onOpenChange={o => { if (busy) return; onOpenChange(o); if (!o) { setError(''); setMustStop(false); } }}
        title={t(`Delete "${s.title}"?`, `删除「${s.title}」？`)}
        description={active ? t('This session is still running. It will be stopped first.', '此会话仍在运行，将先停止它。') : undefined}
        confirmLabel={busy ? t('Deleting…', '删除中…') : active ? t('Stop and delete', '先停止并删除') : t('Delete', '删除')}
        destructive
        busy={busy}
        onConfirm={() => void remove()}
      >
        <div className="flex flex-col gap-2 text-ui-sm">
          <div className="font-medium text-foreground">{t('This deletes:', '将删除：')}</div>
          <ul className="flex list-disc flex-col gap-1 pl-5 text-foreground-subtle">
            <li>{t('The conversation recorded by CC Remote', 'CC Remote 记录的对话')}</li>
            <li>{t('Uploaded attachments and queued messages', '上传的附件和排队消息')}</li>
            <li>{t('Pending approvals and questions', '待处理的审批和提问')}</li>
            {active && <li>{t('The running process and its background tasks', '正在运行的进程及其后台任务')}</li>}
          </ul>
          <div className="text-foreground-subtlest">{t('Kept: the project directory and the provider history.', '保留：项目目录和提供方的历史记录。')}</div>
          <ErrorText>{error}</ErrorText>
        </div>
      </ConfirmDialog>
  );
}

export function DeleteSessionSurface({id, open, onOpenChange}: {id: string; open: boolean; onOpenChange: (open: boolean) => void}) {
  const session = useApp(s => s.sessions.find(item => item.id === id));
  if (!session) return null;
  return <DeleteSessionDialog session={session} open={open} onOpenChange={onOpenChange} />;
}

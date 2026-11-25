import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { ArrowUp, GitBranch, X } from 'lucide-react';
import { nanoid } from 'nanoid';
import { useBackend } from '@/backend/backend';
import type { Attachment, Provider, Session } from '@/backend/types';
import { cn } from '@/components/lib/utils';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { toast } from '@/components/ui/toast';
import { AttachmentStrip } from '@/features/composer/parts';
import { isSendKey, sendContent } from '@/features/composer/composerLogic';
import { useDrafts } from '@/features/composer/drafts';
import { sendIds } from '@/features/composer/sendIds';
import { appendFileReference, useFileReferenceTarget, type FileReferenceTarget } from '@/features/files/fileReference';
import { t } from '@/lib/i18n';
import { usePrefs } from '@/lib/prefs';
import { hasFeature, openFork, selectionKey, useApp } from '@/store/app';

interface HistoryDraft {
  seq: number;
  token: string;
  text: string;
  attachments: Attachment[];
}

const HistoryEditContext = createContext<{
  draft?: HistoryDraft; busy: boolean;
  edit(draft: HistoryDraft | undefined): void; setBusy(busy: boolean): void;
} | null>(null);

export const useHistoryEdit = () => useContext(HistoryEditContext);

export function HistoryEditProvider({ children }: { children: ReactNode }) {
  const [draft, edit] = useState<HistoryDraft>();
  const [busy, setBusy] = useState(false);
  return <HistoryEditContext.Provider value={{ draft, edit, busy, setBusy }}>{children}</HistoryEditContext.Provider>;
}

export function HistoryComposer({ session, native, mobile, children, referenceTarget }: {
  session: Session; native?: Provider; mobile: boolean; children: ReactNode; referenceTarget?: FileReferenceTarget;
}) {
  const editor = useHistoryEdit();
  return (
    <>
      <div className={editor?.draft ? 'hidden' : 'contents'}>{children}</div>
      {editor?.draft && <BranchComposer key={editor.draft.token} session={session} native={native} mobile={mobile} draft={editor.draft} referenceTarget={referenceTarget} />}
    </>
  );
}

function BranchComposer({ session, native, mobile, draft, referenceTarget }: {
  session: Session; native?: Provider; mobile: boolean; draft: HistoryDraft; referenceTarget?: FileReferenceTarget;
}) {
  const backend = useBackend();
  const editor = useHistoryEdit()!;
  const sendKey = usePrefs(s => s.sendKey);
  const box = useRef<HTMLTextAreaElement>(null);
  const composing = useRef(false);
  const active = useRef(false);
  const attempt = useRef<{ signature: string; requestId: string }>(undefined);
  const [error, setError] = useState('');
  const [text, setText] = useState(draft.text);
  const [attachments, setAttachments] = useState(draft.attachments);
  useFileReferenceTarget(referenceTarget, reference => {
    if (editor.busy) return;
    setText(value => appendFileReference(value, reference));
    requestAnimationFrame(() => {
      const input = box.current;
      if (!input) return;
      input.focus();
      input.setSelectionRange(input.value.length, input.value.length);
    });
  });
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
  }, []);

  async function submit() {
    const body = text.trim();
    if ((!body && !attachments.length) || active.current) return;
    const attachmentIds = attachments.map(a => a.id);
    const signature = JSON.stringify(attachmentIds);
    const saved = attempt.current;
    const requestId = saved?.signature === signature ? saved.requestId : nanoid();
    attempt.current = { signature, requestId };
    const origin = selectionKey(useApp.getState().selection);
    active.current = true;
    editor.setBusy(true);
    setError('');
    try {
      const reply = await backend.forkSession(session.id, {
        requestId, anchorSeq: draft.seq, anchorToken: draft.token, before: true, attachmentIds,
      }, native);
      const child = reply.session.id;
      const copied = reply.attachmentIds ?? [];
      const childAttachments = reply.attachments ?? [];
      const childDrafts = useDrafts.getState();
      childDrafts.setText(child, body);
      childDrafts.setAttachments(child, () => childAttachments);
      const open = () => openFork(reply.session);
      try {
        const sendId = sendIds.idFor(child, sendContent(body, copied));
        const info = useApp.getState().info;
        await backend.send(child, {
          text: body, attachmentIds: copied,
          ...(hasFeature(info, 'sendId') ? { sendId } : {}),
          ...(hasFeature(info, 'queueControls') ? { defer: true } : {}),
        });
        sendIds.clear(child);
        childDrafts.clear(child, { text: body, attachmentIds: copied });
        if (selectionKey(useApp.getState().selection) === origin) open();
        else toast(t('Message sent in the new branch.', '已在新分支发送消息。'), { actionLabel: t('Open', '打开'), onAction: open });
      } catch (err) {
        const detail = err instanceof Error ? err.message : String(err);
        toast(t(`Send could not be confirmed. The draft is saved in the new branch. ${detail}`, `暂时无法确认送达，草稿已保存到新分支。${detail}`),
          { variant: 'warning', actionLabel: t('Open branch', '打开分支'), onAction: open });
        if (selectionKey(useApp.getState().selection) === origin) open();
      }
      editor.edit(undefined);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      active.current = false;
      editor.setBusy(false);
    }
  }

  const cancel = () => { if (!active.current) editor.edit(undefined); };
  const label = t('Send in new branch', '在新分支发送');
  return (
    <div className={cn('mx-auto w-full max-w-3xl shrink-0 pb-safe', !mobile && 'px-4 pb-4')}>
      <form className="flex flex-col rounded-2xl border border-input-border bg-input" onSubmit={e => { e.preventDefault(); void submit(); }}>
        <div className="flex items-center gap-2 px-3 pt-2 text-ui-sm text-foreground-subtle">
          <GitBranch className="size-4" />
          <span className="flex-1">{t('Editing for a new branch', '正在编辑，将发送到新分支')}</span>
          <Button type="button" variant="ghost" size={mobile ? 'icon-md' : 'icon-sm'} aria-label={t('Cancel edit', '取消编辑')}
            title={t('Cancel edit', '取消编辑')} disabled={editor.busy} onClick={cancel}><X /></Button>
        </div>
        <AttachmentStrip attachments={attachments} uploading={[]} mobile={mobile} onCancel={() => {}}
          onRemove={id => { if (!active.current) setAttachments(items => items.filter(a => a.id !== id)); }} />
        <textarea ref={box} data-session-composer rows={3} aria-label={t('Edit message', '编辑消息')} value={text} readOnly={editor.busy}
          className={cn('max-h-40 w-full resize-y px-4 py-2 text-ui-base outline-none', mobile && 'text-mobile-input-safe')}
          onChange={e => setText(e.target.value)}
          onCompositionStart={() => { composing.current = true; }} onCompositionEnd={() => { composing.current = false; }}
          onKeyDown={e => {
            if (e.key === 'Escape' && !e.nativeEvent.isComposing && !composing.current) { e.preventDefault(); cancel(); }
            if (isSendKey({ ...e, isComposing: e.nativeEvent.isComposing, keyCode: e.keyCode }, sendKey, composing.current)) {
              e.preventDefault(); void submit();
            }
          }} />
        <div className="flex items-center justify-end gap-2 px-3 py-2">
          {error && <span role="alert" className="mr-auto text-ui-sm text-destructive">{error}</span>}
          <Button type="submit" size={mobile ? 'lg' : 'sm'} disabled={editor.busy || (!text.trim() && !attachments.length)}>
            {editor.busy ? <Spinner /> : <ArrowUp />}{label}
          </Button>
        </div>
      </form>
    </div>
  );
}

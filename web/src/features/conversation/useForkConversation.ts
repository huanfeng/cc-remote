import { useRef, useState } from 'react';
import { nanoid } from 'nanoid';
import { useBackend } from '@/backend/backend';
import type { Provider, Session } from '@/backend/types';
import { toast } from '@/components/ui/toast';
import { t } from '@/lib/i18n';
import { hasFeature, openFork, selectionKey, useApp } from '@/store/app';

export function useForkConversation(session: Session, native?: Provider) {
  const backend = useBackend();
  const enabled = useApp(s => hasFeature(s.info, 'session-fork')) && (session.canFork ?? (!!native && !!session.cwd));
  const unavailable = !native && (session.status === 'running' || session.status === 'waiting' || !!session.activeTasks || !!session.queued?.length);
  const [busy, setBusy] = useState(false);
  const active = useRef(false);
  const retry = useRef<{ anchorSeq?: number; anchorToken?: string; requestId: string }>(undefined);

  async function fork(anchorSeq?: number, anchorToken?: string) {
    if (!enabled || unavailable || active.current) return;
    const origin = selectionKey(useApp.getState().selection);
    const saved = retry.current;
    const body = saved && saved.anchorSeq === anchorSeq && saved.anchorToken === anchorToken
      ? saved : { requestId: nanoid(), anchorSeq, anchorToken };
    retry.current = body;
    active.current = true;
    setBusy(true);
    try {
      const result = await backend.forkSession(session.id, body, native);
      retry.current = undefined;
      if (selectionKey(useApp.getState().selection) === origin) openFork(result.session);
      else toast(t('Conversation branched.', '已创建会话分支。'), {
        actionLabel: t('Open', '打开'), onAction: () => openFork(result.session),
      });
    } catch (error) {
      toast(error instanceof Error ? error.message : String(error), { variant: 'warning' });
    } finally {
      active.current = false;
      setBusy(false);
    }
  }

  return { enabled, unavailable, busy, fork };
}

// The main pane for a selected session: header, conversation or terminal, and
// the composer. Shared by the desktop and mobile shells.
import { useCallback } from 'react';
import { useBackend } from '@/backend/backend';
import type { Provider, Session } from '@/backend/types';
import { selectionKey, useApp } from '@/store/app';
import { useTranscript } from '@/store/transcripts';
import { usePrefs } from '@/lib/prefs';
import { useUnclaimedEscape } from '@/lib/hooks';
import { t } from '@/lib/i18n';
import { SessionHeader } from './SessionHeader';
import { ConversationView } from '@/features/conversation/ConversationView';
import { Composer } from '@/features/composer/Composer';
import { NativeFooter } from './NativeFooter';
import { TerminalView } from '@/features/terminal/TerminalView';

export function SessionWorkspace({ mobile }: { mobile: boolean }) {
  const selection = useApp(s => s.selection);
  const sessions = useApp(s => s.sessions);
  const loaded = useApp(s => s.sessionsLoaded);
  if (!selection) return null;
  if (selection.kind === 'native') return <NativeWorkspace key={selectionKey(selection)} id={selection.id} provider={selection.provider} mobile={mobile} />;
  const session = sessions.find(s => s.id === selection.id);
  if (!session) {
    return (
      <div className="flex h-full items-center justify-center p-6 text-center text-ui-base text-foreground-subtle">
        {loaded ? t('The session does not exist or was deleted.', '会话不存在或已被删除。') : t('Loading…', '加载中…')}
      </div>
    );
  }
  return <ManagedWorkspace key={session.id} session={session} mobile={mobile} />;
}

function ManagedWorkspace({ session, mobile }: { session: Session; mobile: boolean }) {
  const backend = useBackend();
  const key = selectionKey({ kind: 'session', id: session.id });
  const transcript = useTranscript(key, session.id);
  const terminalView = usePrefs(s => !!s.terminalView[session.id]) && session.mode === 'terminal';
  const running = session.status === 'running';
  const stop = useCallback(() => { void backend.stop(session.id); }, [backend, session.id]);
  useUnclaimedEscape(running && !terminalView ? stop : undefined);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <SessionHeader session={session} mobile={mobile} terminalView={terminalView} />
      <div className="relative flex min-h-0 flex-1 flex-col">
        {terminalView ? (
          <TerminalView session={session} mobile={mobile} />
        ) : (
          <>
            <ConversationView tkey={key} session={session} transcript={transcript} mobile={mobile} />
            <Composer session={session} transcript={transcript} mobile={mobile} />
          </>
        )}
      </div>
    </div>
  );
}

function NativeWorkspace({ id, provider, mobile }: { id: string; provider: Provider; mobile: boolean }) {
  const key = selectionKey({ kind: 'native', id, provider });
  const transcript = useTranscript(key, id, provider);
  const session: Session = transcript.session ?? { id, provider, title: '…', cwd: '', updatedAt: '' };
  return (
    <div className="flex h-full min-h-0 flex-col">
      <SessionHeader session={session} mobile={mobile} native />
      <div className="relative flex min-h-0 flex-1 flex-col">
        <ConversationView tkey={key} session={session} transcript={transcript} native={provider} mobile={mobile} />
        <NativeFooter id={id} provider={provider} cwd={session.cwd} />
      </div>
    </div>
  );
}

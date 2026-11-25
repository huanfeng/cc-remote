// The main pane for a selected session: header, conversation or terminal, and
// the composer. Shared by the desktop and mobile shells.
import { useCallback, useRef } from 'react';
import { useBackend } from '@/backend/backend';
import type { Provider, Session } from '@/backend/types';
import { selectionKey, useApp } from '@/store/app';
import { useTranscript } from '@/store/transcripts';
import { usePrefs } from '@/lib/prefs';
import { useUnclaimedEscape } from '@/lib/hooks';
import { t } from '@/lib/i18n';
import { SessionHeader } from './SessionHeader';
import { ConversationView } from '@/features/conversation/ConversationView';
import { HistoryComposer, HistoryEditProvider } from '@/features/conversation/HistoryComposer';
import { Composer } from '@/features/composer/Composer';
import { FileDockLayout } from '@/features/files/FileDock';
import type { FileReference } from '@/features/files/fileReference';
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
  const preferTerminal = usePrefs(s => !!s.terminalView[session.id]);
  const terminalView = session.mode === 'terminal' && (session.provider === 'codex' || preferTerminal);
  const referenceTarget = useRef<((reference: FileReference) => void) | null>(null);
  const referenceFile = useCallback((reference: FileReference) => referenceTarget.current?.(reference), []);
  const running = session.status === 'running';
  const stop = useCallback(() => { void backend.stop(session.id); }, [backend, session.id]);
  useUnclaimedEscape(running && !terminalView ? stop : undefined);

  return (
    <HistoryEditProvider>
      <div className="flex h-full min-h-0 flex-col">
        <SessionHeader session={session} dockKey={session.id} mobile={mobile} terminalView={terminalView} />
        <FileDockLayout dockKey={session.id} projectCwd={session.cwd} mobile={mobile} onReference={terminalView ? undefined : referenceFile} footer={!terminalView && (
          <HistoryComposer session={session} mobile={mobile} referenceTarget={referenceTarget}>
            <Composer session={session} transcript={transcript} mobile={mobile} referenceTarget={referenceTarget} />
          </HistoryComposer>
        )}>
          {terminalView ? (
            <TerminalView session={session} mobile={mobile} />
          ) : (
            <ConversationView tkey={key} session={session} transcript={transcript} mobile={mobile} />
          )}
        </FileDockLayout>
      </div>
    </HistoryEditProvider>
  );
}

function NativeWorkspace({ id, provider, mobile }: { id: string; provider: Provider; mobile: boolean }) {
  const key = selectionKey({ kind: 'native', id, provider });
  const transcript = useTranscript(key, id, provider);
  const session: Session = transcript.session ?? { id, provider, title: '…', cwd: '', updatedAt: '' };
  return (
    <HistoryEditProvider>
      <div className="flex h-full min-h-0 flex-col">
        <SessionHeader session={session} dockKey={key} mobile={mobile} native />
        <FileDockLayout dockKey={key} mobile={mobile} footer={(
          <HistoryComposer session={session} native={provider} mobile={mobile}>
            <NativeFooter id={id} provider={provider} cwd={session.cwd} profileId={session.profileId} loaded={!!transcript.session} />
          </HistoryComposer>
        )}>
          <ConversationView tkey={key} session={session} transcript={transcript} native={provider} mobile={mobile} />
        </FileDockLayout>
      </div>
    </HistoryEditProvider>
  );
}

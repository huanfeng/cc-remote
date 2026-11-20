import { useState } from 'react';
import { ArrowUpRight, Square, SquareTerminal } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import { BackendError } from '@/backend/errors';
import { Button } from '@/components/ui/button';
import { toast } from '@/components/ui/toast';
import { conversationTime } from '@/domain/conversationTime';
import { providerName, providerOf } from '@/domain/providers';
import { useConnection } from '@/features/auth/connection';
import { ConfirmDialog } from '@/features/dialogs/common';
import { SurfaceShell } from '@/features/dialogs/SurfaceShell';
import { useProjectShellWorkspace } from '@/features/projectShell/ProjectShellWorkspace';
import { folderName, sessionStatusLabel } from '@/features/session/status';
import { useNow } from '@/lib/hooks';
import { lang, t } from '@/lib/i18n';
import { overlayHistory } from '@/lib/overlayHistory';
import { setTerminalView } from '@/lib/prefs';
import { select, useApp } from '@/store/app';

type Terminal = { id: string; title: string; cwd: string; detail: string; activity?: string; runId?: string };

export function RunningTerminals({ open, onOpenChange, mobile }: {
  open: boolean;
  onOpenChange(open: boolean): void;
  mobile: boolean;
}) {
  const backend = useBackend();
  const workspace = useProjectShellWorkspace();
  const sessions = useApp(s => s.sessions);
  const sessionsLoaded = useApp(s => s.sessionsLoaded);
  const hostName = useApp(s => s.info?.name);
  const offline = useConnection(s => !!s.offline);
  const [target, setTarget] = useState<Terminal>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const now = useNow();

  const terminals: Terminal[] = sessions.filter(session => session.mode === 'terminal' && session.online).map(session => {
    const last = conversationTime(session, now, lang);
    return {
      id: session.id,
      title: session.title || folderName(session.cwd),
      cwd: session.cwd,
      detail: [
        providerName(providerOf(session)),
        sessionStatusLabel(session),
        session.attached ? t('Local terminal attached', '本机终端已接入') : '',
        session.archived ? t('Archived', '已归档') : '',
      ].filter(Boolean).join(' · '),
      activity: last.iso ? t(`Last conversation ${last.label}`, `最近对话 ${last.label}`) : undefined,
    };
  });
  for (const shell of workspace.enabled ? workspace.shells : []) {
    if (shell.state !== 'running' || !shell.runId) continue;
    terminals.push({ id: shell.id, runId: shell.runId, title: folderName(shell.cwd), cwd: shell.cwd, detail: t('Project terminal', '项目终端') });
  }
  const shellError = workspace.enabled && workspace.shellListState === 'error';
  const loading = !sessionsLoaded || (workspace.enabled && workspace.shellListState === 'loading');

  function reconnect(item: Terminal) {
    if (item.runId) {
      overlayHistory().afterClose(() => workspace.open({ cwd: item.cwd, shellId: item.id, runId: item.runId }));
      return;
    }
    overlayHistory().afterClose(() => {
      workspace.close();
      setTerminalView(item.id, true);
      select({ kind: 'session', id: item.id });
    });
  }

  async function stop() {
    if (!target || busy || offline) return;
    setBusy(true);
    setError('');
    try {
      if (target.runId) {
        try {
          await backend.stopProjectShell(target.id, target.runId);
        } catch (err) {
          if (!(err instanceof BackendError && err.status === 410)) throw err;
        }
        workspace.finishRun(target.id, target.runId, 'stopped');
      } else {
        await backend.exit(target.id);
        const selected = useApp.getState().selection;
        if (selected?.kind === 'session' && selected.id === target.id) setTerminalView(target.id, false);
      }
      setTarget(undefined);
      toast(t('Terminal ended', '终端已结束'));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <SurfaceShell open={open} mobile={mobile} onOpenChange={onOpenChange}
      title={t('Running terminals', '运行中的终端')} description={hostName} className="sm:max-w-xl">
      {offline && <p role="status" className="py-2 text-ui-sm text-destructive">{t('Host connection lost. The list may be out of date.', '与宿主的连接已断开，列表可能已过期。')}</p>}
      {shellError && !offline && <p role="status" className="py-2 text-ui-sm text-destructive">{t('Could not load project terminals. Retrying…', '无法加载项目终端，正在重试…')}</p>}
      {loading && !offline && <p role="status" className="py-3 text-ui-sm text-foreground-subtle">{t('Loading terminals…', '正在加载终端…')}</p>}
      {!terminals.length && !loading && !offline && !shellError &&
        <p role="status" className="py-6 text-center text-ui-sm text-foreground-subtle">{t('No running terminals on this host', '此宿主没有运行中的终端')}</p>}
      {terminals.length > 0 && (
        <ul className="divide-y divide-border/60 overflow-hidden rounded-lg border border-border/60">
          {terminals.map(item => {
            const disabled = offline || (!!item.runId && shellError);
            return (
              <li key={item.runId ? `shell:${item.id}/${item.runId}` : `session:${item.id}`} className="flex items-start gap-2.5 px-3 py-2.5">
                <SquareTerminal className="mt-0.5 size-4 shrink-0 text-foreground-subtle" aria-hidden />
                <div className="min-w-0 flex-1">
                  <div className="break-words text-ui-base font-medium">{item.title}</div>
                  <div className="break-all text-ui-xs text-foreground-subtle">{item.cwd}</div>
                  <div className="mt-1 text-ui-xs text-foreground-subtle">{item.detail}{item.activity && ` · ${item.activity}`}</div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <Button type="button" size="icon-sm" variant="ghost" disabled={disabled} onClick={() => reconnect(item)}
                    aria-label={t('Open terminal', '接回终端')} title={t('Open terminal', '接回终端')}><ArrowUpRight /></Button>
                  <Button type="button" size="icon-sm" variant="ghost" disabled={disabled} onClick={() => { setError(''); setTarget(item); }}
                    aria-label={t('End terminal', '结束终端')} title={t('End terminal', '结束终端')} className="text-destructive"><Square /></Button>
                </div>
              </li>
            );
          })}
        </ul>
      )}
      <ConfirmDialog open={open && !!target} onOpenChange={next => { if (!next && !busy) setTarget(undefined); }}
        title={t('End terminal?', '结束终端？')}
        description={t('This closes the terminal and interrupts any work still running in it.', '结束终端会中断其中仍在运行的任务。')}
        confirmLabel={t('End terminal', '结束终端')} destructive busy={busy} onConfirm={() => void stop()}>
        {target && <p className="break-words text-ui-sm font-medium">{target.title}</p>}
        {error && <p role="alert" className="break-words text-ui-sm text-destructive">{error}</p>}
      </ConfirmDialog>
    </SurfaceShell>
  );
}

import { useEffect, useState } from 'react';
import { ArrowUpRight, Square } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import { BackendError } from '@/backend/errors';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
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
const terminalKey = (item: Terminal) => item.runId ? `shell:${item.id}/${item.runId}` : `session:${item.id}`;

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
  const [checked, setChecked] = useState<Set<string>>(() => new Set());
  const [target, setTarget] = useState<{ items: Terminal[]; bulk: boolean }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [batchError, setBatchError] = useState('');
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
    terminals.push({ id: shell.id, runId: shell.runId, title: shell.name || folderName(shell.cwd), cwd: shell.cwd, detail: t('Project terminal', '项目终端') });
  }
  const shellError = workspace.enabled && workspace.shellListState === 'error';
  const loading = !sessionsLoaded || (workspace.enabled && workspace.shellListState === 'loading');
  const available = (item: Terminal) => !item.runId || !shellError;
  const eligible = terminals.filter(available);
  const selectedItems = eligible.filter(item => checked.has(terminalKey(item)));
  const allSelected = eligible.length > 0 && selectedItems.length === eligible.length;

  useEffect(() => {
    setChecked(current => {
      if (!current.size) return current;
      const live = new Set(terminals.map(terminalKey));
      const next = new Set([...current].filter(key => live.has(key)));
      return next.size === current.size ? current : next;
    });
  }, [terminals]);

  function confirm(items: Terminal[], bulk = false) {
    setError('');
    if (bulk) setBatchError('');
    setTarget({ items, bulk });
  }

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

  async function stopOne(item: Terminal) {
    try {
      if (item.runId) await backend.stopProjectShell(item.id, item.runId);
      else await backend.exit(item.id);
    } catch (err) {
      if (!(err instanceof BackendError && (err.status === 404 || (item.runId && err.status === 410)))) throw err;
    }
    if (item.runId) workspace.finishRun(item.id, item.runId, 'stopped');
    else {
      const selected = useApp.getState().selection;
      if (selected?.kind === 'session' && selected.id === item.id) setTerminalView(item.id, false);
    }
  }

  async function stop() {
    if (!target || busy || offline) return;
    setBusy(true);
    setError('');
    const live = new Set(terminals.map(terminalKey));
    const ended = new Set<string>();
    const failures: { item: Terminal; message: string }[] = [];
    try {
      // The host exposes one stop request per terminal. Keep fan-out bounded for large selections.
      const batchSize = 4;
      for (let index = 0; index < target.items.length; index += batchSize) {
        const batch = target.items.slice(index, index + batchSize);
        await Promise.all(batch.map(async item => {
          try {
            if (live.has(terminalKey(item))) {
              if (!available(item)) throw new Error(t('Could not load project terminals. Try again after the list updates.', '无法加载项目终端，请等列表更新后重试。'));
              await stopOne(item);
            }
            ended.add(terminalKey(item));
          } catch (err) {
            failures.push({ item, message: err instanceof Error ? err.message : String(err) });
          }
        }));
      }
      setChecked(current => new Set([...current].filter(key => !ended.has(key))));
      if (!target.bulk && failures.length) {
        setError(failures[0].message);
        return;
      }
      setTarget(undefined);
      if (failures.length) {
        const { item, message } = failures[0];
        setBatchError(t(`Ended ${ended.size}. Could not end ${failures.length}. ${item.title} — ${message}`,
          `已结束 ${ended.size} 个终端，${failures.length} 个未能结束。${item.title} — ${message}`));
      } else {
        toast(target.bulk ? t(`Ended ${ended.size} terminals`, `已结束 ${ended.size} 个终端`) : t('Terminal ended', '终端已结束'));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <SurfaceShell open={open} mobile={mobile} onOpenChange={onOpenChange}
      title={t('Running terminals', '运行中的终端')} description={hostName} className="sm:max-w-xl"
      footer={terminals.length > 0 && <div className="flex w-full flex-wrap items-center gap-x-3 gap-y-1">
        <label className="flex min-h-9 items-center gap-2 text-ui-sm">
          <Checkbox checked={allSelected || (selectedItems.length > 0 ? 'indeterminate' : false)}
            disabled={busy || offline || !eligible.length}
            onCheckedChange={value => setChecked(value === true ? new Set(eligible.map(terminalKey)) : new Set())} />
          {t('Select all', '全选')}
        </label>
        <span role="status" className="flex-1 text-ui-xs text-foreground-subtle">{t(`${selectedItems.length} selected`, `已选 ${selectedItems.length} 个`)}</span>
        <Button type="button" size="sm" variant="destructive" disabled={busy || offline || !selectedItems.length}
          onClick={() => confirm(selectedItems, true)}>{t('End selected', '批量停止')}</Button>
        {batchError && <p role="alert" className="max-h-24 w-full overflow-y-auto break-words text-ui-sm text-destructive">{batchError}</p>}
      </div>}>
      {offline && <p role="status" className="py-2 text-ui-sm text-destructive">{t('Host connection lost. The list may be out of date.', '与宿主的连接已断开，列表可能已过期。')}</p>}
      {shellError && !offline && <p role="status" className="py-2 text-ui-sm text-destructive">{t('Could not load project terminals. Retrying…', '无法加载项目终端，正在重试…')}</p>}
      {loading && !offline && <p role="status" className="py-3 text-ui-sm text-foreground-subtle">{t('Loading terminals…', '正在加载终端…')}</p>}
      {!terminals.length && !loading && !offline && !shellError &&
        <p role="status" className="py-6 text-center text-ui-sm text-foreground-subtle">{t('No running terminals on this host', '此宿主没有运行中的终端')}</p>}
      {terminals.length > 0 && (
        <ul className="divide-y divide-border/60 overflow-hidden rounded-lg border border-border/60">
          {terminals.map(item => {
            const key = terminalKey(item);
            const disabled = busy || offline || !available(item);
            return (
              <li key={key} className="flex items-start gap-2.5 px-3 py-2.5">
                <label className="flex min-w-0 flex-1 items-start gap-2.5">
                  <Checkbox className="mt-1" checked={checked.has(key)} disabled={disabled}
                    aria-label={t(`Select ${item.title}`, `选择 ${item.title}`)} onCheckedChange={value => setChecked(current => {
                      const next = new Set(current);
                      if (value === true) next.add(key);
                      else next.delete(key);
                      return next;
                    })} />
                  <span className="min-w-0 flex-1">
                    <span className="block break-words text-ui-base font-medium">{item.title}</span>
                    <span className="block break-all text-ui-xs text-foreground-subtle">{item.cwd}</span>
                    <span className="mt-1 block text-ui-xs text-foreground-subtle">{item.detail}{item.activity && ` · ${item.activity}`}</span>
                  </span>
                </label>
                <div className="flex shrink-0 items-center gap-1">
                  <Button type="button" size="icon-sm" variant="ghost" disabled={disabled} onClick={() => reconnect(item)}
                    aria-label={t('Open terminal', '接回终端')} title={t('Open terminal', '接回终端')}><ArrowUpRight /></Button>
                  <Button type="button" size="icon-sm" variant="ghost" disabled={disabled} onClick={() => confirm([item])}
                    aria-label={t('End terminal', '结束终端')} title={t('End terminal', '结束终端')} className="text-destructive"><Square /></Button>
                </div>
              </li>
            );
          })}
        </ul>
      )}
      <ConfirmDialog open={open && !!target} onOpenChange={next => { if (!next && !busy) setTarget(undefined); }}
        title={target?.bulk ? t(`End ${target.items.length} selected terminals?`, `停止选中的 ${target.items.length} 个终端？`) : t('End terminal?', '结束终端？')}
        description={t('Any work still running in these terminals will be interrupted.', '终端中仍在运行的任务将被中断。')}
        confirmLabel={busy ? t('Ending…', '正在停止…') : target?.bulk ? t('End selected', '批量停止') : t('End terminal', '结束终端')}
        destructive busy={busy} confirmDisabled={offline} onConfirm={() => void stop()}>
        {target && !target.bulk && <p className="break-words text-ui-sm font-medium">{target.items[0].title}</p>}
        {error && <p role="alert" className="break-words text-ui-sm text-destructive">{error}</p>}
      </ConfirmDialog>
    </SurfaceShell>
  );
}

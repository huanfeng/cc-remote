// Quick switch for a session's model or effort (header chips, "/model",
// "/effort"). A managed session saves the override (PATCH; it applies to the
// next turn); a terminal session has "/model X" typed into Claude Code, after
// a warning that the prompt cache is lost (inventory §4.9).
import { useCallback, useRef, useState, type ReactNode } from 'react';
import { useBackend, type Backend } from '@/backend/backend';
import type { Session } from '@/backend/types';
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter,
  AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { toast } from '@/components/ui/toast';
import { hasFeature, useApp } from '@/store/app';
import { sendIds } from '@/features/composer/sendIds';
import { t } from '@/lib/i18n';
import { dangerousPermission, providerOf } from '@/domain/providers';
import { PickerSurface } from './PickerSurface';
import { EffortList, ModelList } from './lists';

export type QuickKind = 'model' | 'effort';

/** Confirms a terminal session's model switch; resolves false when cancelled. */
export function useModelSwitchConfirm() {
  const [ask, setAsk] = useState<{ model: string; tokens: number } | null>(null);
  const resolver = useRef<(ok: boolean) => void>(undefined);
  const confirm = useCallback((session: Session, model: string) => {
    const tokens = session.live?.contextTokens ?? 0;
    if (!tokens) return Promise.resolve(true);
    return new Promise<boolean>(resolve => { resolver.current = resolve; setAsk({ model, tokens }); });
  }, []);
  const settle = (ok: boolean) => { resolver.current?.(ok); resolver.current = undefined; setAsk(null); };
  const dialog = (
    <AlertDialog open={!!ask} onOpenChange={o => { if (!o) settle(false); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(`Switch to ${ask?.model}?`, `切换到 ${ask?.model}？`)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(
              `The conversation's cache only works with the current model. After switching, the next message reads the whole history again (about ${Math.round((ask?.tokens ?? 0) / 1000)}k tokens), which is slower and uses more tokens.`,
              `当前对话的缓存只对原模型有效。切换后下一条消息会重新读取全部历史（约 ${Math.round((ask?.tokens ?? 0) / 1000)}k tokens），更慢也更耗 token。`,
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => settle(false)}>{t('Cancel', '取消')}</AlertDialogCancel>
          <AlertDialogAction onClick={() => settle(true)}>{t('Switch', '切换')}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
  return { confirm, dialog };
}

/** Saves a managed session's override, keeping the other two fields as they are. */
export async function patchOverride(backend: Backend, s: Session, patch: { model?: string; effort?: string; permissionMode?: string }) {
  const permissionMode = patch.permissionMode ?? s.permissionMode ?? '';
  const updated = await backend.patchSession(s.id, {
    permissionMode, model: patch.model ?? s.model ?? '', effort: patch.effort ?? s.effort ?? '',
    confirmDangerous: dangerousPermission(permissionMode, providerOf(s)),
  });
  const app = useApp.getState();
  if (app.sessions.some(x => x.id === updated.id)) app.setSessions(app.sessions.map(x => (x.id === updated.id ? { ...x, ...updated } : x)));
  return updated;
}

export function useQuickSwitchApply(session: Session) {
  const backend = useBackend();
  const { confirm, dialog } = useModelSwitchConfirm();
  const apply = useCallback(async (kind: QuickKind, value: string) => {
    try {
      if (session.mode === 'terminal') {
        if (!value) return;
        if (kind === 'model' && !(await confirm(session, value))) return;
        // Typed into Claude Code: one send ID per command until the host takes it.
        const text = `/${kind} ${value}`, key = `quick:${session.id}`;
        const sendId = hasFeature(useApp.getState().info, 'sendId') ? { sendId: sendIds.idFor(key, text) } : {};
        await backend.send(session.id, { text, ...sendId });
        sendIds.clear(key);
        toast(t(`Ran /${kind} ${value} in the terminal`, `已在终端执行 /${kind} ${value}`), { position: 'bottom-center' });
        return;
      }
      await patchOverride(backend, session, { [kind]: value });
      toast(
        providerOf(session) === 'codex' || session.status === 'running'
          ? t('Saved. Applies to the next turn.', '已保存，下一轮生效。')
          : t('Saved.', '已保存。'),
        { position: 'bottom-center', variant: 'success' },
      );
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), { position: 'bottom-center', variant: 'warning' });
    }
  }, [backend, confirm, session]);
  return { apply, dialog };
}

export interface QuickSwitchProps {
  session: Session;
  kind: QuickKind;
  open: boolean;
  onOpenChange(open: boolean): void;
  /** Opens the switch (a chip). */
  trigger?: ReactNode;
  /** Positions it when opened elsewhere ("/model"). */
  anchor?: ReactNode;
  side?: 'top' | 'bottom';
  mobile?: boolean;
}

export function QuickSwitch({ session, kind, open, onOpenChange, trigger, anchor, side, mobile }: QuickSwitchProps) {
  const { apply, dialog } = useQuickSwitchApply(session);
  const provider = providerOf(session);
  const terminal = session.mode === 'terminal';
  // A terminal session reports what it runs; a managed one has an override ('' = inherit).
  const value = kind === 'model'
    ? (terminal ? session.live?.modelId || session.model || '' : session.model || '')
    : (terminal ? session.live?.effort || session.effort || '' : session.effort || '');
  const inheritLabel = terminal ? undefined : '';
  const pick = (v: string) => { onOpenChange(false); if (v !== value) void apply(kind, v); };
  return (
    <>
      <PickerSurface
        open={open} onOpenChange={onOpenChange} mobile={mobile} side={side}
        title={kind === 'model' ? t('Switch model', '切换模型') : t('Switch effort', '切换推理强度')}
        trigger={trigger} anchor={anchor} className={kind === 'model' ? 'w-80' : 'w-64'}
      >
        {kind === 'model'
          ? <ModelList provider={provider} sessionId={session.id} value={value} inheritLabel={inheritLabel} mobile={mobile} onPick={pick} />
          : <EffortList provider={provider} sessionId={session.id} model={session.model || session.live?.modelId} value={value} inheritLabel={inheritLabel} mobile={mobile} onPick={pick} />}
      </PickerSurface>
      {dialog}
    </>
  );
}

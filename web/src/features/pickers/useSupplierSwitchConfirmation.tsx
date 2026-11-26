import { useEffect, useRef, useState } from 'react';
import { useBackend } from '@/backend/backend';
import { Checkbox } from '@/components/ui/checkbox';
import { toast } from '@/components/ui/toast';
import { ConfirmDialog, errorMessage } from '@/features/dialogs/common';
import { t } from '@/lib/i18n';

type SwitchRequest = {
  label: string;
  apply(force: boolean): Promise<{ status: 'confirmation' } | { status: 'switched'; finish(): void }>;
};

type Confirmation = {
  request: SwitchRequest;
  interrupt: boolean;
  canRemember: boolean;
  skip: boolean;
};

export function useSupplierSwitchConfirmation(beforePrompt?: () => void) {
  const backend = useBackend();
  const [confirmation, setConfirmation] = useState<Confirmation>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const pending = useRef(false);
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  const attempt = async (run: () => Promise<void>) => {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError('');
    try {
      await run();
    } catch (e) {
      if (mounted.current) {
        setConfirmation(undefined);
        setError(errorMessage(e));
      }
    } finally {
      pending.current = false;
      if (mounted.current) setBusy(false);
    }
  };

  const apply = async (request: SwitchRequest, force: boolean, skip: boolean) => {
    const result = await request.apply(force);
    if (result.status === 'confirmation') {
      if (mounted.current) {
        beforePrompt?.();
        setConfirmation({ request, interrupt: true, canRemember: false, skip });
      }
      return;
    }
    if (skip) {
      try {
        await backend.setSupplierSwitchWarning(false);
      } catch {
        toast(t('The supplier changed, but the host could not save your warning preference.', '供应商已切换，但宿主未能保存提示偏好。'), { variant: 'warning', position: 'bottom-center' });
      }
    }
    if (mounted.current) {
      setConfirmation(undefined);
      result.finish();
    }
  };

  const start = (request: SwitchRequest, warn: boolean) => {
    if (confirmation) return;
    void attempt(async () => {
      // Read from the host for each switch so another device's choice takes effect immediately.
      const enabled = warn ? await backend.supplierSwitchWarning().catch(() => undefined) : false;
      if (!mounted.current) return;
      if (enabled === false) {
        await apply(request, false, false);
      } else {
        beforePrompt?.();
        setConfirmation({ request, interrupt: false, canRemember: enabled === true, skip: false });
      }
    });
  };

  const interrupt = confirmation?.interrupt;
  const label = confirmation?.request.label ?? '';
  const dialog = <ConfirmDialog open={!!confirmation} busy={busy}
    onOpenChange={open => { if (!open && !pending.current) setConfirmation(undefined); }}
    title={interrupt ? t('Interrupt and switch supplier?', '中断并切换供应商？') : t(`Switch supplier to ${label}?`, `切换供应商为「${label}」？`)}
    description={interrupt
      ? t('This session is busy. Switching now stops the current work and background tasks, keeps the saved context and does not replay the interrupted request. Finished actions cannot be undone.', '此会话正在运行。立即切换将停止当前执行及后台任务，保留已保存的上下文，不会自动重发被中断的请求。已经完成的操作无法撤销。')
      : t('The new supplier may be unable to reuse this conversation’s prompt cache. Your next message may process the conversation history again and cost more in input tokens.', '切换后，新供应商可能无法复用当前对话的缓存。下一条消息可能需要重新处理对话历史，产生更高的输入 token 费用。')}
    confirmLabel={interrupt ? t('Interrupt and switch', '中断并切换') : t('Switch supplier', '切换供应商')}
    destructive={interrupt}
    onConfirm={() => {
      if (confirmation) void attempt(() => apply(confirmation.request, confirmation.interrupt, confirmation.skip));
    }}>
    {confirmation?.canRemember && <label className="flex min-h-9 cursor-pointer items-center gap-2 text-ui-sm max-md:min-h-11">
      <Checkbox checked={confirmation.skip} disabled={busy}
        onCheckedChange={checked => setConfirmation(current => current && { ...current, skip: checked === true })} />
      {t('Don’t show this warning again', '下次不再提示')}
    </label>}
  </ConfirmDialog>;

  return { start, busy, error, clearError: () => setError(''), dialog };
}

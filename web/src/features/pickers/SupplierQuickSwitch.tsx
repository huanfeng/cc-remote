import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useBackend } from '@/backend/backend';
import type { ProfileCatalog, Session } from '@/backend/types';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { toast } from '@/components/ui/toast';
import { ConfirmDialog, errorMessage } from '@/features/dialogs/common';
import { providerOf } from '@/domain/providers';
import { t } from '@/lib/i18n';
import { useApp } from '@/store/app';
import { PickerGroup, PickerItem, PickerList, PickerSurface } from './PickerSurface';
import { currentSupplierValue, profileLabel, profilesFor, supplierOptions, switchSupplier } from './supplierSwitch';
import { useSupplierUsage } from './useSupplierUsage';

export function SupplierQuickSwitch({ session, open, onOpenChange, trigger, anchor, mobile }: {
  session: Session; open: boolean; onOpenChange(open: boolean): void; trigger?: ReactNode; anchor?: ReactNode; mobile?: boolean;
}) {
  const backend = useBackend();
  const [catalog, setCatalog] = useState<ProfileCatalog>();
  const [catalogError, setCatalogError] = useState('');
  const [version, setVersion] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [forceTarget, setForceTarget] = useState<string | null>(null);
  const pending = useRef(false);
  const generation = useRef(0);
  useEffect(() => {
    generation.current++;
    pending.current = false;
    setBusy(false); setError(''); setForceTarget(null);
    return () => { generation.current++; };
  }, [backend, session.id]);
  useEffect(() => {
    if (!open) return;
    let live = true;
    setCatalog(undefined); setCatalogError(''); setError('');
    backend.profiles().then(value => { if (live) setCatalog(value); }, e => { if (live) setCatalogError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend, session.id, open, version]);
  const provider = providerOf(session);
  const usage = useSupplierUsage(provider, open);
  const current = currentSupplierValue(session);
  const currentProfile = profilesFor(catalog, provider).find(p => p.id === current);
  const missing = !!current && current !== '@native' && !currentProfile;
  const options = supplierOptions(catalog, provider, false, usage);
  const run = async (value: string, force = false) => {
    if (pending.current || (!force && value === current)) return;
    pending.current = true; setBusy(true); setError('');
    const started = generation.current;
    try {
      const result = await switchSupplier(backend, session, value, force);
      if (started !== generation.current) return;
      if (result.status === 'confirmation') {
        onOpenChange(false);
        setForceTarget(value);
      } else {
        setForceTarget(null);
        const app = useApp.getState();
        app.setSessions(app.sessions.map(s => s.id === result.session.id ? { ...s, ...result.session } : s));
        onOpenChange(false);
        toast(t('Supplier switched. Interrupted requests are not resent; continue when ready.', '供应商已切换。被中断的请求不会自动重发，准备好后可继续对话。'), { variant: 'success', position: 'bottom-center' });
      }
    } catch (e) {
      if (started === generation.current) {
        setForceTarget(null); setError(errorMessage(e));
        // Forced switching happens after the picker closes; keep failures visible.
        if (force) toast(errorMessage(e), { variant: 'warning', position: 'bottom-center' });
      }
    } finally {
      if (started === generation.current) { pending.current = false; setBusy(false); }
    }
  };
  return <>
    <PickerSurface open={open} onOpenChange={value => { if (!busy) onOpenChange(value); }} trigger={trigger} anchor={anchor} mobile={mobile} title={t('Switch supplier', '切换供应商')} className="w-80">
      <div className="px-2 pb-2 pt-1.5 text-ui-sm text-foreground-subtle">
        {t('Current', '当前')}：{session.profileName || (currentProfile && profileLabel(currentProfile)) || session.profileId || t('Host configuration', '宿主原生配置')}
      </div>
      <PickerList label={t('Switch supplier', '切换供应商')}>
      {catalogError ? <div role="alert" className="px-2 py-2 text-ui-sm text-destructive">{catalogError}<Button variant="ghost" size="sm" className="ml-1 cursor-pointer" onClick={() => setVersion(v => v + 1)}>{t('Retry', '重试')}</Button></div>
        : !catalog ? <div role="status" className="flex items-center gap-2 px-2 py-3 text-ui-sm text-foreground-subtle"><Spinner className="size-3.5" />{t('Loading suppliers…', '正在读取供应商…')}</div>
        : !catalog.capabilities?.switchSupplier ? <p className="px-2 py-3 text-ui-sm text-foreground-subtle">{t('This host does not support switching suppliers. Update the host to continue.', '此宿主不支持切换供应商，请先更新宿主。')}</p>
        : <>
          <PickerGroup>{options.map(option => <PickerItem key={option.value} option={option} selected={option.value === current} disabled={busy} mobile={mobile} onSelect={value => { void run(value); }} />)}</PickerGroup>
          {missing && <p className="px-2 py-2 text-ui-sm text-foreground-subtlest">{t('The current supplier is unavailable; its saved session configuration is kept until you switch.', '当前供应商已不可用；切换前仍保留此会话保存的配置。')}</p>}
        </>}
      {busy && <div role="status" className="flex items-center gap-2 px-2 py-2 text-ui-sm text-foreground-subtle"><Spinner className="size-3.5" />{t('Switching…', '正在切换…')}</div>}
      {error && <p role="alert" className="px-2 py-2 text-ui-sm text-destructive">{error}</p>}
      </PickerList>
    </PickerSurface>
    <ConfirmDialog open={forceTarget !== null} onOpenChange={value => { if (!value && !busy) setForceTarget(null); }}
      title={t('Interrupt and switch supplier?', '中断并切换供应商？')}
      description={t('This session is busy. Switching now stops the current work and background tasks, keeps the saved context and does not replay the interrupted request. Finished actions cannot be undone.', '此会话正在运行。立即切换将停止当前执行及后台任务，保留已保存的上下文，不会自动重发被中断的请求。已经完成的操作无法撤销。')}
      confirmLabel={t('Interrupt and switch', '中断并切换')} destructive busy={busy} onConfirm={() => { if (forceTarget !== null) void run(forceTarget, true); }} />
  </>;
}

import { useSupplierOrder } from '@/lib/supplierOrder';
import { orderSuppliers } from '@/domain/supplierOrder';
import { useEffect, useState, type ReactNode } from 'react';
import { useBackend } from '@/backend/backend';
import type { ProfileCatalog, Session } from '@/backend/types';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { toast } from '@/components/ui/toast';
import { errorMessage } from '@/features/dialogs/common';
import { providerOf } from '@/domain/providers';
import { t } from '@/lib/i18n';
import { useApp } from '@/store/app';
import { PickerGroup, PickerItem, PickerList, PickerSurface } from './PickerSurface';
import { changesSupplier, currentSupplierValue, hasLockedCodexAccount, profileLabel, profilesFor, supplierSwitchOptions, switchSupplier } from './supplierSwitch';
import { useSupplierSwitchConfirmation } from './useSupplierSwitchConfirmation';
import { useSupplierUsage } from './useSupplierUsage';

export function SupplierQuickSwitch({ session, open, onOpenChange, trigger, anchor, mobile }: {
  session: Session; open: boolean; onOpenChange(open: boolean): void; trigger?: ReactNode; anchor?: ReactNode; mobile?: boolean;
}) {
  const backend = useBackend();
  const [rawCatalog, setCatalog] = useState<ProfileCatalog>();
  const { order } = useSupplierOrder();
  const catalog = rawCatalog && { ...rawCatalog, profiles: orderSuppliers(rawCatalog.profiles, order) };
  const [catalogError, setCatalogError] = useState('');
  const [version, setVersion] = useState(0);
  const { start, busy, error, clearError, dialog } = useSupplierSwitchConfirmation(() => onOpenChange(false));
  useEffect(() => {
    if (!open && error) toast(error, { variant: 'warning', position: 'bottom-center' });
  }, [open, error]);
  useEffect(() => {
    if (!open) return;
    let live = true;
    setCatalog(undefined); setCatalogError('');
    backend.profiles().then(value => { if (live) setCatalog(value); }, e => { if (live) setCatalogError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend, session.id, open, version]);
  const provider = providerOf(session);
  const usage = useSupplierUsage(provider, open);
  const current = currentSupplierValue(session);
  const currentProfile = profilesFor(catalog, provider).find(p => p.id === current);
  const missing = !!current && current !== '@native' && !currentProfile;
  const accountLocked = hasLockedCodexAccount(session);
  const options = supplierSwitchOptions(catalog, provider, usage, session);
  const choose = (value: string, label: string) => {
    if (value === current) return;
    start({
      label,
      apply: async force => {
        const result = await switchSupplier(backend, session, value, force);
        if (result.status === 'confirmation') return result;
        return { status: 'switched', finish: () => {
          const app = useApp.getState();
          app.setSessions(app.sessions.map(s => s.id === result.session.id ? { ...s, ...result.session } : s));
          onOpenChange(false);
          toast(t('Supplier switched. Interrupted requests are not resent; continue when ready.', '供应商已切换。被中断的请求不会自动重发，准备好后可继续对话。'), { variant: 'success', position: 'bottom-center' });
        } };
      },
    }, changesSupplier(session, value || catalog?.defaultProfiles[provider] || ''));
  };
  return <>
    <PickerSurface open={open} onOpenChange={value => {
      if (busy) return;
      if (value) clearError();
      onOpenChange(value);
    }} trigger={trigger} anchor={anchor} mobile={mobile} title={t('Switch supplier', '切换供应商')} className="w-80">
      <div className="px-2 pb-2 pt-1.5 text-ui-sm text-foreground-subtle">
        {t('Current', '当前')}：{session.profileName || (currentProfile && profileLabel(currentProfile)) || session.profileId || t('Host configuration', '宿主原生配置')}
      </div>
      <PickerList label={t('Switch supplier', '切换供应商')}>
       {accountLocked ? <p className="px-2 py-3 text-ui-sm text-foreground-subtle">{t('This session is bound to its original ChatGPT account. Start a new session to choose another supplier.', '此会话绑定了原 ChatGPT 账号。要使用其他供应商，请新建会话。')}</p>
        : catalogError ? <div role="alert" className="px-2 py-2 text-ui-sm text-destructive">{catalogError}<Button variant="ghost" size="sm" className="ml-1 cursor-pointer" onClick={() => setVersion(v => v + 1)}>{t('Retry', '重试')}</Button></div>
        : !catalog ? <div role="status" className="flex items-center gap-2 px-2 py-3 text-ui-sm text-foreground-subtle"><Spinner className="size-3.5" />{t('Loading suppliers…', '正在读取供应商…')}</div>
        : !catalog.capabilities?.switchSupplier ? <p className="px-2 py-3 text-ui-sm text-foreground-subtle">{t('This host does not support switching suppliers. Update the host to continue.', '此宿主不支持切换供应商，请先更新宿主。')}</p>
        : <>
          {options.length
            ? <PickerGroup>{options.map(option => <PickerItem key={option.value} option={option} selected={option.value === current} disabled={busy} mobile={mobile} onSelect={value => choose(value, option.label)} />)}</PickerGroup>
            : <p className="px-2 py-3 text-ui-sm text-foreground-subtle">{t('No suppliers available to switch to.', '暂无可切换的供应商。')}</p>}
          {missing && <p className="px-2 py-2 text-ui-sm text-foreground-subtlest">{t('The current supplier is unavailable; its saved session configuration is kept until you switch.', '当前供应商已不可用；切换前仍保留此会话保存的配置。')}</p>}
        </>}
      {busy && <div role="status" className="flex items-center gap-2 px-2 py-2 text-ui-sm text-foreground-subtle"><Spinner className="size-3.5" />{t('Switching…', '正在切换…')}</div>}
      {error && <p role="alert" className="px-2 py-2 text-ui-sm text-destructive">{error}</p>}
      </PickerList>
    </PickerSurface>
    {dialog}
  </>;
}

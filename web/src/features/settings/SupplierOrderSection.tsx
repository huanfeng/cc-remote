import { ArrowDown, ArrowUp } from 'lucide-react';
import type { ProfileCatalog } from '@/backend/types';
import { Button } from '@/components/ui/button';
import { GroupCard } from '@/features/dialogs/SurfaceShell';
import { moveSupplier, orderSuppliers, supplierOrderKey } from '@/domain/supplierOrder';
import { useSupplierOrder } from '@/lib/supplierOrder';
import { t } from '@/lib/i18n';
import { GroupTitle } from './parts';

export function SupplierOrderSection({ catalog }: { catalog?: ProfileCatalog }) {
  const { order, setOrder } = useSupplierOrder();
  if (!catalog?.profiles.length) return null;
  return <>
    <GroupTitle>{t('Supplier order', '供应商顺序')}</GroupTitle>
    <p className="mb-2 text-ui-sm text-foreground-subtle">{t('Mix OAuth and API suppliers in your preferred order. Applies to supplier choices on this browser for this host; defaults stay unchanged.', 'OAuth 和 API 可混合排序，应用于当前浏览器中此宿主的供应商选项，不改变默认供应商。')}</p>
    <GroupCard>
      {(['claude', 'codex'] as const).map(app => {
        const entries = orderSuppliers(catalog.profiles.filter(p => p.app === app), order);
        return entries.map((p, index) => <div key={supplierOrderKey(p)} className="flex items-center gap-2 border-t border-border/60 px-4 py-2 first:border-t-0">
          <div className="min-w-0 flex-1 break-words text-ui-sm">{p.name}<span className="text-foreground-subtle"> · {app === 'claude' ? 'Claude' : 'Codex'} · {p.kind === 'oauth' ? 'OAuth' : 'API'}</span></div>
          <Button variant="ghost" size="icon" disabled={index === 0} aria-label={t(`Move ${p.name} up`, `上移 ${p.name}`)} onClick={() => setOrder(moveSupplier(entries, order, supplierOrderKey(p), -1))}><ArrowUp className="size-4" /></Button>
          <Button variant="ghost" size="icon" disabled={index === entries.length - 1} aria-label={t(`Move ${p.name} down`, `下移 ${p.name}`)} onClick={() => setOrder(moveSupplier(entries, order, supplierOrderKey(p), 1))}><ArrowDown className="size-4" /></Button>
        </div>);
      })}
    </GroupCard>
  </>;
}

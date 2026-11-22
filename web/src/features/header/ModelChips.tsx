// Header chips (desktop): supplier, model and effort as one compact group of
// ghost chips, each with its own icon. Model and effort open the quick switch;
// supplier choices open an anchored list with interruption confirmation.
import { forwardRef, useState, type ComponentProps, type ReactNode } from 'react';
import { Building2, Cpu, Gauge } from 'lucide-react';
import { cn } from '@/components/lib/utils';
import type { Session } from '@/backend/types';
import { SupplierQuickSwitch } from '@/features/pickers/SupplierQuickSwitch';
import { useIsMobile } from '@/lib/hooks';
import { t } from '@/lib/i18n';
import { QuickSwitch, type QuickKind } from '@/features/pickers/QuickSwitch';
import { shortModel } from '@/features/pickers/options';

export function ModelChips({ session }: { session: Session }) {
  const [open, setOpen] = useState<QuickKind | 'supplier' | null>(null);
  const mobile = useIsMobile();
  const model = session.live?.model || shortModel(session.model) || t('Default model', '默认模型');
  const effort = session.live?.effort || session.effort;
  // Always shown: which supplier a session runs on matters as much as its model.
  const supplier = session.profileName || session.profileId || t('Host configuration', '宿主原生配置');
  const toggle = (k: QuickKind | 'supplier') => (o: boolean) => setOpen(o ? k : null);
  return (
    <div className="flex max-w-[45%] shrink-0 items-center gap-0.5">
      <SupplierQuickSwitch session={session} open={open === 'supplier'} onOpenChange={toggle('supplier')} mobile={mobile}
        trigger={<Chip icon={<Building2 />} className="max-w-44" aria-label={t(`Supplier: ${supplier}. Switch`, `供应商：${supplier}，点击切换`)}>{supplier}</Chip>} />
      <QuickSwitch session={session} kind="model" open={open === 'model'} onOpenChange={toggle('model')}
        trigger={<Chip icon={<Cpu />} aria-label={t(`Model: ${model}. Switch`, `模型：${model}，点击切换`)}>{model}</Chip>} />
      {effort && (
        <QuickSwitch session={session} kind="effort" open={open === 'effort'} onOpenChange={toggle('effort')}
          trigger={<Chip icon={<Gauge />} aria-label={t(`Effort: ${effort}. Switch`, `推理强度：${effort}，点击切换`)}>{effort}</Chip>} />
      )}
    </div>
  );
}

export const Chip = forwardRef<HTMLButtonElement, ComponentProps<'button'> & { icon: ReactNode }>(function Chip({ icon, className, children, ...props }, ref) {
  return (
    <button
      ref={ref} type="button"
      className={cn(
        'inline-flex cursor-pointer disabled:cursor-not-allowed h-6 min-w-7 shrink items-center gap-1 rounded-md px-1.5 text-ui-sm text-foreground-subtle transition-colors',
        'hover:bg-surface-hover hover:text-foreground aria-expanded:bg-surface-hover aria-expanded:text-foreground',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 [&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:opacity-70',
        className,
      )}
      {...props}
    >
      {icon}
      <span className="truncate">{children}</span>
    </button>
  );
});

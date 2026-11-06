// Header chips (desktop): model, effort and supplier as one compact group of
// ghost chips, each with its own icon. Model and effort open the quick switch;
// the supplier opens the session details (switching it lives there).
import { forwardRef, useState, type ComponentProps, type ReactNode } from 'react';
import { Building2, Cpu, Gauge } from 'lucide-react';
import { cn } from '@/components/lib/utils';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import type { Session } from '@/backend/types';
import { useApp } from '@/store/app';
import { t } from '@/lib/i18n';
import { QuickSwitch, type QuickKind } from '@/features/pickers/QuickSwitch';
import { shortModel } from '@/features/pickers/options';

export function ModelChips({ session }: { session: Session }) {
  const [open, setOpen] = useState<QuickKind | null>(null);
  const openSurface = useApp(s => s.open);
  const model = session.live?.model || shortModel(session.model) || t('Default model', '默认模型');
  const effort = session.live?.effort || session.effort;
  const toggle = (k: QuickKind) => (o: boolean) => setOpen(o ? k : null);
  return (
    <div className="flex min-w-0 shrink items-center gap-0.5">
      <QuickSwitch session={session} kind="model" open={open === 'model'} onOpenChange={toggle('model')}
        trigger={<Chip icon={<Cpu />} aria-label={t(`Model: ${model}. Switch`, `模型：${model}，点击切换`)}>{model}</Chip>} />
      {effort && (
        <QuickSwitch session={session} kind="effort" open={open === 'effort'} onOpenChange={toggle('effort')}
          trigger={<Chip icon={<Gauge />} aria-label={t(`Effort: ${effort}. Switch`, `推理强度：${effort}，点击切换`)}>{effort}</Chip>} />
      )}
      {session.profileName && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Chip icon={<Building2 />} className="max-w-40" onClick={() => openSurface({ type: 'session-details', id: session.id })}>
              {session.profileName}
            </Chip>
          </TooltipTrigger>
          <TooltipContent>{t('Supplier · open session details', '供应商 · 打开会话详情')}</TooltipContent>
        </Tooltip>
      )}
    </div>
  );
}

const Chip = forwardRef<HTMLButtonElement, ComponentProps<'button'> & { icon: ReactNode }>(function Chip({ icon, className, children, ...props }, ref) {
  return (
    <button
      ref={ref} type="button"
      className={cn(
        'inline-flex h-6 min-w-0 shrink-0 items-center gap-1 rounded-md px-1.5 text-ui-sm text-foreground-subtle transition-colors',
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

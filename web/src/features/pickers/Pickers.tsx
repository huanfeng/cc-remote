// Standalone pickers with their own trigger: the composer's permission chip
// and the new session dialog's fields. Props stay simple: value, onChange,
// provider; '' means "inherit the default".
import { forwardRef, useState, type ComponentProps, type ReactNode } from 'react';
import { ChevronDown, Shield, ShieldAlert, TriangleAlert } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { cn } from '@/components/lib/utils';
import type { Provider } from '@/backend/types';
import { t } from '@/lib/i18n';
import { dangerousPermission } from '@/domain/providers';
import { dangerDetail, permissionLabel, permissionOptions, shortModel } from './options';
import { PickerGroup, PickerItem, PickerList, PickerSurface } from './PickerSurface';
import { EffortList, ModelList } from './lists';

export type PickerVariant = 'field' | 'chip' | 'icon';

export interface PickerProps {
  value: string;
  onChange(value: string): void;
  provider: Provider;
  /** Label of the '' option; omit to hide it. */
  inheritLabel?: string;
  variant?: PickerVariant;
  mobile?: boolean;
  disabled?: boolean;
  className?: string;
}

const PickerTrigger = forwardRef<HTMLButtonElement, ComponentProps<'button'> & { variant: PickerVariant; icon?: ReactNode; label: string; mobile?: boolean; tone?: 'danger' }>(
  function PickerTrigger({ variant, icon, label, mobile, tone, className, ...props }, ref) {
    if (variant === 'icon') {
      return (
        <Button ref={ref} type="button" variant="ghost" size="icon-lg" aria-label={label} title={label}
          className={cn(mobile && 'size-9', tone === 'danger' && 'text-destructive', className)} {...props}>
          {icon}
        </Button>
      );
    }
    if (variant === 'chip') {
      return (
        <Button ref={ref} type="button" variant="ghost" size="sm"
          className={cn('h-7 max-w-48 gap-1 rounded-lg px-2 text-ui-sm text-foreground-subtle hover:text-foreground', tone === 'danger' && 'text-destructive hover:text-destructive', className)} {...props}>
          {icon}<span className="truncate">{label}</span><ChevronDown className="size-3 opacity-60" />
        </Button>
      );
    }
    return (
      <Button ref={ref} type="button" variant="outline"
        className={cn('h-8 w-full justify-between gap-2 rounded-lg px-2.5 font-normal', mobile && 'h-10', tone === 'danger' && 'text-destructive', className)} {...props}>
        <span className="flex min-w-0 items-center gap-1.5">{icon}<span className="truncate">{label}</span></span>
        <ChevronDown className="size-3.5 shrink-0 opacity-60" />
      </Button>
    );
  },
);

export function ModelPicker({ value, onChange, provider, inheritLabel, variant = 'field', mobile, disabled, className, sessionId }: PickerProps & { sessionId?: string }) {
  const [open, setOpen] = useState(false);
  const label = value ? shortModel(value) : inheritLabel || t('Default model', '默认模型');
  return (
    <PickerSurface
      open={open} onOpenChange={setOpen} mobile={mobile} title={t('Model', '模型')} className="w-80"
      trigger={<PickerTrigger variant={variant} label={label} mobile={mobile} disabled={disabled} className={className} />}
    >
      <ModelList provider={provider} sessionId={sessionId} value={value} inheritLabel={inheritLabel} mobile={mobile}
        onPick={v => { setOpen(false); onChange(v); }} />
    </PickerSurface>
  );
}

export function EffortPicker({ value, onChange, provider, inheritLabel, variant = 'field', mobile, disabled, className, model, sessionId }: PickerProps & { model?: string; sessionId?: string }) {
  const [open, setOpen] = useState(false);
  const label = value || inheritLabel || t('Default effort', '默认强度');
  return (
    <PickerSurface
      open={open} onOpenChange={setOpen} mobile={mobile} title={t('Reasoning effort', '推理强度')} className="w-64"
      trigger={<PickerTrigger variant={variant} label={label} mobile={mobile} disabled={disabled} className={className} />}
    >
      <EffortList provider={provider} sessionId={sessionId} model={model} value={value} inheritLabel={inheritLabel} mobile={mobile}
        onPick={v => { setOpen(false); onChange(v); }} />
    </PickerSurface>
  );
}

/**
 * Permission mode. A dangerous mode (dangerousPermission) is confirmed in
 * place before onChange; the caller then sends confirmDangerous.
 * `effective` is what '' resolves to, for the trigger label and the warning.
 */
export function PermissionPicker({ value, onChange, provider, inheritLabel, variant = 'field', mobile, disabled, className, effective }: PickerProps & { effective?: string }) {
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState<string | null>(null);
  const shown = value || effective || '';
  const danger = dangerousPermission(shown, provider);
  const label = value ? permissionLabel(value, provider) : inheritLabel || (effective ? permissionLabel(effective, provider) : t('Default permissions', '默认权限'));
  const Icon = danger ? ShieldAlert : Shield;

  const pick = (v: string) => {
    if (v === value) { setOpen(false); return; }
    if (dangerousPermission(v || effective || '', provider)) { setPending(v); return; }
    setOpen(false); onChange(v);
  };
  const close = (o: boolean) => { setOpen(o); if (!o) setPending(null); };

  return (
    <PickerSurface
      open={open} onOpenChange={close} mobile={mobile} side={variant === 'field' ? 'bottom' : 'top'}
      title={t('Permission mode', '权限模式')} className="w-80"
      // Escape in the confirmation goes back to the list; otherwise it closes the picker.
      onEscapeKeyDown={e => { if (pending !== null) { e.preventDefault(); setPending(null); } }}
      trigger={
        <PickerTrigger variant={variant} label={variant === 'icon' ? `${t('Permission mode', '权限模式')}: ${label}` : label}
          icon={<Icon className="size-3.5" />} mobile={mobile} disabled={disabled} className={className} tone={danger ? 'danger' : undefined} />
      }
    >
      {pending !== null ? (
        <DangerConfirm mode={pending || effective || ''} provider={provider} mobile={mobile}
          onCancel={() => setPending(null)}
          onConfirm={() => { const v = pending; setPending(null); setOpen(false); onChange(v); }} />
      ) : (
        <div className="flex flex-col">
          <div className="px-2 pb-1 pt-1.5 text-ui-sm font-medium text-foreground-subtle">{t('Permission mode', '权限模式')}</div>
          <PickerList label={t('Permission mode', '权限模式')}>
            {inheritLabel !== undefined && (
              <PickerGroup>
                <PickerItem mobile={mobile} option={{ value: '', label: inheritLabel || t('Use default', '跟随默认'), description: effective ? permissionLabel(effective, provider) : undefined }} selected={value === ''} onSelect={pick} />
              </PickerGroup>
            )}
            <PickerGroup>
              {permissionOptions(provider, value).map(o => <PickerItem key={o.value} mobile={mobile} option={o} selected={o.value === value} onSelect={pick} />)}
            </PickerGroup>
          </PickerList>
        </div>
      )}
    </PickerSurface>
  );
}

function DangerConfirm({ mode, provider, mobile, onCancel, onConfirm }: { mode: string; provider: Provider; mobile?: boolean; onCancel(): void; onConfirm(): void }) {
  return (
    <div
      role="alertdialog" aria-label={t('Confirm permission change', '确认权限变更')}
      className="flex flex-col gap-2 p-2"
    >
      <div className="flex items-center gap-2 text-ui-base font-medium">
        <TriangleAlert className="size-4 text-destructive" />
        {t(`Enable ${permissionLabel(mode, provider)}?`, `启用「${permissionLabel(mode, provider)}」？`)}
      </div>
      <p className="text-ui-sm text-foreground-subtle">{dangerDetail(mode, provider)}</p>
      <p className="text-ui-sm text-foreground-subtlest">{t('Use only in trusted projects.', '请仅在可信项目中使用。')}</p>
      <div className="mt-1 flex justify-end gap-2">
        <Button variant="ghost" size={mobile ? 'lg' : 'default'} className={cn(mobile && 'h-10')} onClick={onCancel}>{t('Cancel', '取消')}</Button>
        <Button variant="destructive" size={mobile ? 'lg' : 'default'} className={cn(mobile && 'h-10')} autoFocus onClick={onConfirm}>{t('Enable', '确认启用')}</Button>
      </div>
    </div>
  );
}

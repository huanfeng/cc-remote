// The model / effort / permission fields used by the new-session dialog,
// settings defaults and session details: the pickers package's choosers in
// their 'field' form. '' means "inherit" (inheritLabel). PermissionPicker
// confirms a dangerous mode itself before onChange.
import type { Provider } from '@/backend/types';
import { EffortPicker, ModelPicker, PermissionPicker } from '@/features/pickers';
import { t } from '@/lib/i18n';
import { useIsMobile } from '@/lib/hooks';
import { cn } from '@/components/lib/utils';

interface FieldProps {
  value: string;
  onChange: (value: string) => void;
  provider: Provider;
  inheritLabel?: string;
  disabled?: boolean;
  className?: string;
}

const width = 'w-56 max-sm:w-full';

export function PermissionField({ inheritLabel, className, ...p }: FieldProps & { effective?: string }) {
  const mobile = useIsMobile();
  return <PermissionPicker {...p} mobile={mobile} variant="field" inheritLabel={inheritLabel ?? t('Use default', '跟随默认')} className={cn(width, className)} />;
}

export function ModelField({ inheritLabel, className, ...p }: FieldProps & { sessionId?: string }) {
  const mobile = useIsMobile();
  return <ModelPicker {...p} mobile={mobile} variant="field" inheritLabel={inheritLabel ?? t('Use default', '跟随默认')} className={cn(width, className)} />;
}

export function EffortField({ inheritLabel, className, ...p }: FieldProps & { model?: string; sessionId?: string }) {
  const mobile = useIsMobile();
  return <EffortPicker {...p} mobile={mobile} variant="field" inheritLabel={inheritLabel ?? t('Use default', '跟随默认')} className={cn(width, className)} />;
}

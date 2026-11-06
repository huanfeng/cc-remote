// Small pieces shared by the dialogs, settings and status surfaces.
import { useEffect, useState, type ReactNode } from 'react';
import { Check, Copy } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter,
  AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { cn } from '@/components/lib/utils';
import { useBackend } from '@/backend/backend';
import type { ProfileCatalog, Provider, SupplierProfile } from '@/backend/types';
import { copyText } from '@/domain/clipboard';
import { t } from '@/lib/i18n';

/** A copy button that reports the result in place. */
export function CopyButton({ text, label, className, size = 'icon-sm' }: { text: string; label?: string; className?: string; size?: 'icon-sm' | 'icon-md' | 'sm' }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');
  useEffect(() => {
    if (state === 'idle') return;
    const id = window.setTimeout(() => setState('idle'), 1600);
    return () => window.clearTimeout(id);
  }, [state]);
  const copy = () => copyText(text).then(() => setState('copied'), () => setState('failed'));
  const tip = state === 'copied' ? t('Copied', '已复制') : state === 'failed' ? t('Copy failed; select the text', '复制失败，请手动选择') : label || t('Copy', '复制');
  return (
    <Button type="button" variant="ghost" size={size} className={cn('text-foreground-subtle', className)} onClick={() => void copy()} aria-label={tip} title={tip}>
      {state === 'copied' ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
      {size === 'sm' && <span>{state === 'copied' ? t('Copied', '已复制') : label || t('Copy', '复制')}</span>}
    </Button>
  );
}

/** A monospace value with a copy button, selectable as a fallback. */
export function CopyField({ value, className, multiline }: { value: string; className?: string; multiline?: boolean }) {
  return (
    <div className={cn('flex items-start gap-1 rounded-lg border border-border/70 bg-surface py-1.5 pl-3 pr-1', className)}>
      <code className={cn('min-w-0 flex-1 select-all py-0.5 font-mono text-ui-sm text-foreground', multiline ? 'whitespace-pre-wrap break-all' : 'truncate')} title={value}>{value}</code>
      <CopyButton text={value} />
    </div>
  );
}

/** A controlled confirmation dialog. */
export function ConfirmDialog({ open, onOpenChange, title, description, confirmLabel, destructive, busy, onConfirm, children }: {
  open: boolean; onOpenChange: (open: boolean) => void; title: ReactNode; description?: ReactNode; confirmLabel: string;
  destructive?: boolean; busy?: boolean; onConfirm: () => void; children?: ReactNode;
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          {description && <AlertDialogDescription>{description}</AlertDialogDescription>}
        </AlertDialogHeader>
        {children}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy} className="max-md:h-10">{t('Cancel', '取消')}</AlertDialogCancel>
          <AlertDialogAction
            variant={destructive ? 'destructive' : 'default'}
            className="max-md:h-10"
            disabled={busy}
            onClick={e => { e.preventDefault(); onConfirm(); }}
          >
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/** Radix Select cannot hold '' as a value; this stands in for "inherit / default". */
export const NONE = '__none__';
export const toSelect = (v: string) => v || NONE;
export const fromSelect = (v: string) => (v === NONE ? '' : v);

export interface Option { value: string; label: ReactNode; description?: ReactNode; disabled?: boolean }

/** A Select over plain options, where '' is a valid value. */
export function SimpleSelect({ value, onChange, options, disabled, className, ariaLabel, size = 'lg', placeholder }: {
  value: string; onChange: (v: string) => void; options: Option[]; disabled?: boolean; className?: string; ariaLabel?: string;
  size?: 'default' | 'lg'; placeholder?: string;
}) {
  return (
    <Select value={toSelect(value)} onValueChange={v => onChange(fromSelect(v))} disabled={disabled}>
      <SelectTrigger size={size} aria-label={ariaLabel} className={cn('w-56 min-w-0 justify-between max-sm:w-full', className)}>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent position="popper" align="end">
        {options.map(o => (
          <SelectItem key={o.value || NONE} value={toSelect(o.value)} disabled={o.disabled}>{o.label}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/** Inline error text. */
export function ErrorText({ children }: { children?: ReactNode }) {
  if (!children) return null;
  return <p role="alert" className="rounded-lg bg-destructive-surface px-3 py-2 text-ui-sm text-destructive-surface-foreground">{children}</p>;
}

export const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e));
export const errorStatus = (e: unknown) => (e as { status?: number } | null)?.status;

// ---------- Supplier profiles ----------

export function useProfileCatalog() {
  const backend = useBackend();
  const [catalog, setCatalog] = useState<ProfileCatalog>();
  const [error, setError] = useState('');
  const [version, setVersion] = useState(0);
  useEffect(() => {
    let live = true;
    setError('');
    backend.profiles().then(c => { if (live) setCatalog(c); }, e => { if (live) setError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend, version]);
  return { catalog, error, reload: () => setVersion(v => v + 1) };
}

export const profilesFor = (catalog: ProfileCatalog | undefined, app: Provider): SupplierProfile[] =>
  catalog?.profiles.filter(p => p.app === app) ?? [];

export const profileLabel = (p: { name: string; kind?: string }) => (p.kind === 'oauth' && !/oauth/i.test(p.name) ? `${p.name} · OAuth` : p.name);

/** What "default" resolves to for app: a profile, the native configuration, or a profile that is gone. */
export function defaultSupplierLabel(catalog: ProfileCatalog | undefined, app: Provider): string {
  const id = catalog?.defaultProfiles[app];
  if (!id || id === '@native') return t('Default · native configuration', '默认 · 原生配置');
  const p = profilesFor(catalog, app).find(x => x.id === id);
  return p ? t(`Default · ${profileLabel(p)}`, `默认 · ${profileLabel(p)}`) : t('Default · unavailable', '默认 · 已不可用');
}

/** The supplier choices for a new or resumed session. */
export function supplierOptions(catalog: ProfileCatalog | undefined, app: Provider, resuming = false): Option[] {
  return [
    { value: '', label: resuming ? t('Keep the session configuration', '沿用原会话配置') : defaultSupplierLabel(catalog, app) },
    { value: '@native', label: t('Native host configuration', '宿主原生配置') },
    ...profilesFor(catalog, app).map(p => ({ value: p.id, label: profileLabel(p) })),
  ];
}

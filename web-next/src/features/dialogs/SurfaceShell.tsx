// The frame every top-level surface shares. Desktop: a centred rounded-2xl
// dialog with a header, a scrolling body and an optional footer. Phones: a
// full-screen page (pt-safe/pb-safe, large close button) or a bottom sheet.
// All three are Radix dialogs, so focus trapping, Escape and the open/close
// animations come from the primitives.
import type { ReactNode } from 'react';
import { Dialog as DialogPrimitive } from 'radix-ui';
import { ChevronLeft, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { BottomSheet, BottomSheetContent, BottomSheetDescription, BottomSheetTitle } from '@/components/mobile-ui/bottom-sheet';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';

export interface SurfaceShellProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  mobile: boolean;
  title: ReactNode;
  description?: ReactNode;
  /** Phone presentation. */
  mobileVariant?: 'full' | 'sheet';
  /** Desktop content classes (width, height). */
  className?: string;
  bodyClassName?: string;
  footer?: ReactNode;
  /** Extra controls at the right of the header (left of the close button on desktop). */
  actions?: ReactNode;
  /** Phone full-screen only: a back button instead of the close button. */
  onBack?: () => void;
  /** Renders children directly, without the header/body/footer frame (desktop). */
  bare?: boolean;
  children: ReactNode;
}

/** Phone surfaces: buttons and selects inside are at least 36 px. */
export const TOUCH = '[&_[data-slot=button]]:min-h-9 [&_[data-slot=button]]:min-w-9 [&_[data-slot=select-trigger]]:min-h-9';

export function SurfaceShell(props: SurfaceShellProps) {
  const { open, onOpenChange, mobile, mobileVariant = 'full' } = props;
  if (mobile && mobileVariant === 'sheet') return <SheetFrame {...props} />;
  if (mobile) return <FullFrame {...props} />;
  const { title, description, className, bodyClassName, footer, actions, bare, children } = props;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={!bare}
        className={cn('flex max-h-[min(86vh,820px)] flex-col gap-0 overflow-hidden p-0 sm:max-w-lg', className)}
        {...(description ? {} : { 'aria-describedby': undefined })}
      >
        {bare ? children : (
          <>
            <div className="flex shrink-0 items-start gap-3 px-5 pb-3 pt-4 pr-12">
              <div className="min-w-0 flex-1">
                <DialogTitle className="text-ui-lg font-semibold">{title}</DialogTitle>
                {description && <DialogDescription className="mt-0.5 text-ui-sm text-foreground-subtle">{description}</DialogDescription>}
              </div>
              {actions && <div className="flex shrink-0 items-center gap-1">{actions}</div>}
            </div>
            <div className={cn('min-h-0 flex-1 overflow-y-auto px-5 pb-5', bodyClassName)}>{children}</div>
            {footer && <div className="flex shrink-0 items-center justify-end gap-2 border-t border-border/60 px-5 py-3">{footer}</div>}
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

function FullFrame({ open, onOpenChange, title, description, bodyClassName, footer, actions, onBack, children }: SurfaceShellProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Content
          {...(description ? {} : { 'aria-describedby': undefined })}
          className="fixed inset-0 z-50 flex flex-col bg-background pt-safe text-foreground outline-none duration-200 data-open:animate-in data-open:fade-in-0 data-open:slide-in-from-bottom-6 data-closed:animate-out data-closed:fade-out-0 data-closed:slide-out-to-bottom-6"
        >
          <div className="flex h-12 shrink-0 items-center gap-1 border-b border-border/60 px-1">
            {onBack ? (
              <Button variant="ghost" size="icon-lg" className="size-10" aria-label={t('Back', '返回')} onClick={onBack}>
                <ChevronLeft className="size-5" />
              </Button>
            ) : (
              <DialogPrimitive.Close asChild>
                <Button variant="ghost" size="icon-lg" className="size-10" aria-label={t('Close', '关闭')}>
                  <X className="size-5" />
                </Button>
              </DialogPrimitive.Close>
            )}
            <DialogPrimitive.Title className="min-w-0 flex-1 truncate text-ui-lg font-semibold">{title}</DialogPrimitive.Title>
            {actions && <div className={cn('flex shrink-0 items-center gap-1 pr-1', TOUCH)}>{actions}</div>}
          </div>
          {description && <DialogPrimitive.Description className="sr-only">{description}</DialogPrimitive.Description>}
          {/* Phones: every button and select in the body is at least 36 px. */}
          <div className={cn('min-h-0 flex-1 overflow-y-auto px-4 pb-6 pt-3', TOUCH, !footer && 'pb-[max(1.5rem,env(safe-area-inset-bottom))]', bodyClassName)}>{children}</div>
          {footer && (
            <div className="flex shrink-0 items-center justify-end gap-2 border-t border-border/60 px-4 pt-3 pb-[calc(env(safe-area-inset-bottom)+12px)] [&_[data-slot=button]]:h-10 [&_[data-slot=button]]:px-4">
              {footer}
            </div>
          )}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

function SheetFrame({ open, onOpenChange, title, description, bodyClassName, footer, actions, children }: SurfaceShellProps) {
  return (
    <BottomSheet open={open} onOpenChange={onOpenChange}>
      <BottomSheetContent {...(description ? {} : { 'aria-describedby': undefined })} className="max-w-none">
        <div className="flex shrink-0 items-center gap-2 px-4 pb-2 pt-3">
          <div className="min-w-0 flex-1">
            <BottomSheetTitle>{title}</BottomSheetTitle>
            {description && <BottomSheetDescription>{description}</BottomSheetDescription>}
          </div>
          {actions}
        </div>
        <div className={cn('min-h-0 flex-1 overflow-y-auto px-4 pb-3', TOUCH, bodyClassName)}>{children}</div>
        {footer && (
          <div className="flex shrink-0 items-center justify-end gap-2 border-t border-border/60 px-4 pt-3 [&_[data-slot=button]]:h-10 [&_[data-slot=button]]:px-4">
            {footer}
          </div>
        )}
      </BottomSheetContent>
    </BottomSheet>
  );
}

/** A labelled section inside a surface: small caps-like heading, then content. */
export function Section({ title, description, actions, children, className }: { title?: ReactNode; description?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cn('flex flex-col gap-2', className)}>
      {(title || actions) && (
        <div className="flex items-end gap-2">
          <div className="min-w-0 flex-1">
            {title && <h3 className="text-ui-sm font-medium text-foreground-subtle">{title}</h3>}
            {description && <p className="mt-0.5 text-ui-sm text-foreground-subtlest">{description}</p>}
          </div>
          {actions}
        </div>
      )}
      {children}
    </section>
  );
}

/** A rounded card of rows separated by hairlines (ZCode's SettingsGroupCard). */
export function GroupCard({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn('overflow-hidden rounded-xl border border-border/70 bg-card', className)}>{children}</div>;
}

/**
 * One aligned row: label (+ description) on the left, control on the right;
 * stacks on narrow screens. Derived from ZCode (Apache-2.0)
 * settings/SettingsPageParts.tsx SettingsRow; modified by cc-remote.
 */
export function Row({ label, description, control, children, stack, className }: {
  label: ReactNode; description?: ReactNode; control?: ReactNode; children?: ReactNode; stack?: boolean; className?: string;
}) {
  return (
    <div className={cn('border-t border-border/60 px-4 py-3 first:border-t-0', className)}>
      <div className={cn('flex gap-x-4 gap-y-2', stack ? 'flex-col' : 'flex-col sm:flex-row sm:items-center')}>
        <div className="min-w-0 flex-1">
          <div className="text-ui-base font-medium text-foreground">{label}</div>
          {description && <div className="mt-0.5 text-ui-sm leading-5 text-foreground-subtle">{description}</div>}
        </div>
        {control && <div className={cn('flex min-w-0 items-center gap-2', !stack && 'sm:justify-end')}>{control}</div>}
      </div>
      {children && <div className="mt-3">{children}</div>}
    </div>
  );
}

/** A read-only key/value row (details panels). */
export function InfoRow({ label, children, mono, title }: { label: ReactNode; children: ReactNode; mono?: boolean; title?: string }) {
  return (
    <div className="flex items-start gap-4 border-t border-border/60 px-4 py-2.5 first:border-t-0">
      <div className="w-32 shrink-0 text-ui-sm text-foreground-subtle max-sm:w-24">{label}</div>
      <div className={cn('min-w-0 flex-1 break-words text-ui-base text-foreground', mono && 'font-mono text-ui-sm')} title={title}>{children}</div>
    </div>
  );
}

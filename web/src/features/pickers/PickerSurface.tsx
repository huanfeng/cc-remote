// Where a picker's list appears: a popover on desktop, a bottom sheet on
// phones. `trigger` opens it; `anchor` only positions it (opened elsewhere).
import type { ReactNode } from 'react';
import { Command as CommandPrimitive } from 'cmdk';
import { Check } from 'lucide-react';
import { Popover, PopoverAnchor, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import {
  BottomSheet, BottomSheetContent, BottomSheetDescription, BottomSheetHeader, BottomSheetTitle, BottomSheetTrigger,
} from '@/components/mobile-ui/bottom-sheet';
import { cn } from '@/components/lib/utils';
import type { PickerOption } from './options';

export interface PickerSurfaceProps {
  open: boolean;
  onOpenChange(open: boolean): void;
  mobile?: boolean;
  title: string;
  trigger?: ReactNode;
  anchor?: ReactNode;
  side?: 'top' | 'bottom';
  align?: 'start' | 'center' | 'end';
  className?: string;
  /** Escape inside the surface; preventDefault keeps it open. */
  onEscapeKeyDown?(e: KeyboardEvent): void;
  children: ReactNode;
}

export function PickerSurface({ open, onOpenChange, mobile, title, trigger, anchor, side = 'bottom', align = 'start', className, onEscapeKeyDown, children }: PickerSurfaceProps) {
  if (mobile) {
    return (
      <BottomSheet open={open} onOpenChange={onOpenChange}>
        {trigger && <BottomSheetTrigger asChild>{trigger}</BottomSheetTrigger>}
        {anchor}
        <BottomSheetContent onEscapeKeyDown={onEscapeKeyDown}>
          <BottomSheetHeader>
            <BottomSheetTitle>{title}</BottomSheetTitle>
            <BottomSheetDescription className="sr-only">{title}</BottomSheetDescription>
          </BottomSheetHeader>
          <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">{children}</div>
        </BottomSheetContent>
      </BottomSheet>
    );
  }
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      {trigger && <PopoverTrigger asChild>{trigger}</PopoverTrigger>}
      {anchor && <PopoverAnchor asChild>{anchor}</PopoverAnchor>}
      <PopoverContent
        side={side}
        align={align}
        sideOffset={6}
        aria-label={title}
        className={cn('w-72 gap-0 p-1', className)}
        onEscapeKeyDown={onEscapeKeyDown}
        onOpenAutoFocus={e => {
          // Hand focus to the list so ↑ ↓ Enter work at once.
          e.preventDefault();
          const root = e.currentTarget as HTMLElement | null;
          (root?.querySelector<HTMLElement>('[cmdk-input]') ?? root?.querySelector<HTMLElement>('[cmdk-root]'))?.focus();
        }}
      >
        {children}
      </PopoverContent>
    </Popover>
  );
}

/** A keyboard-navigable list of options (cmdk without filtering unless `search`). */
export function PickerList({ label, children, search, className }: { label: string; children: ReactNode; search?: ReactNode; className?: string }) {
  return (
    <CommandPrimitive
      label={label}
      loop
      shouldFilter={false}
      tabIndex={0}
      className={cn('flex flex-col outline-none', className)}
    >
      {search}
      <CommandPrimitive.List className="max-h-80 overflow-y-auto overscroll-contain outline-none">
        {children}
      </CommandPrimitive.List>
    </CommandPrimitive>
  );
}

export function PickerGroup({ heading, children }: { heading?: ReactNode; children: ReactNode }) {
  return (
    <CommandPrimitive.Group
      heading={heading}
      className="py-0.5 **:[[cmdk-group-heading]]:px-2 **:[[cmdk-group-heading]]:pb-1 **:[[cmdk-group-heading]]:pt-1.5 **:[[cmdk-group-heading]]:text-ui-xs **:[[cmdk-group-heading]]:font-medium **:[[cmdk-group-heading]]:text-foreground-subtlest"
    >
      {children}
    </CommandPrimitive.Group>
  );
}

export function PickerItem({ option, selected, onSelect, mobile, id }: { option: PickerOption; selected: boolean; onSelect(value: string): void; mobile?: boolean; id?: string }) {
  return (
    <CommandPrimitive.Item
      value={id ?? `opt:${option.value}`}
      onSelect={() => onSelect(option.value)}
      className={cn(
        'flex cursor-default items-start gap-2 rounded-md px-2 py-1.5 text-ui-base outline-none select-none data-[selected=true]:bg-menu-hover',
        mobile && 'min-h-11 items-center px-3',
      )}
    >
      <span className="mt-0.5 flex size-4 shrink-0 items-center justify-center">
        {selected && <Check className="size-3.5 text-foreground" />}
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="flex items-center gap-1.5">
          <span className="truncate">{option.label}</span>
          {option.tag && (
            <span className={cn(
              'rounded px-1 text-ui-xs leading-4',
              option.tone === 'danger' ? 'bg-destructive-surface text-destructive-surface-foreground' : 'bg-warning-surface text-warning-surface-foreground',
            )}>{option.tag}</span>
          )}
        </span>
        {option.description && <span className="truncate text-ui-sm text-foreground-subtle">{option.description}</span>}
      </span>
    </CommandPrimitive.Item>
  );
}

import type { ReactNode } from 'react';
import { Button } from '@/components/ui/button';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/components/lib/utils';

export function ViewerIconButton({ label, disabled, expanded, onClick, className, children }: {
  label: string; disabled?: boolean; expanded?: boolean; onClick: () => void; className?: string; children: ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button type="button" variant="ghost" size="icon-md" aria-label={label} aria-expanded={expanded} disabled={disabled}
          className={cn('text-foreground-subtle hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring [&_svg]:size-4', className)}
          onClick={onClick}>{children}</Button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  );
}

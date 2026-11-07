// Shared chrome of approval, question and plan cards: a quiet bordered card
// with an eyebrow, a state label, the body and an action row.
import type { ReactNode } from 'react';
import { cn } from '@/components/lib/utils';

export type RequestTone = 'pending' | 'done' | 'declined' | 'neutral';

export function RequestShell({ icon, eyebrow, state, tone = 'neutral', children, actions, error, className }: {
  icon: ReactNode;
  eyebrow: ReactNode;
  state?: ReactNode;
  tone?: RequestTone;
  children: ReactNode;
  actions?: ReactNode;
  error?: string;
  className?: string;
}) {
  return (
    <section
      className={cn(
        'overflow-hidden rounded-xl border bg-card',
        tone === 'pending' ? 'border-border-hover shadow-[0_1px_2px_rgb(0_0_0/0.04)]' : 'border-border/60',
        className,
      )}
    >
      <header className="flex h-9 items-center gap-2 border-b border-border/60 px-3">
        <span className="flex shrink-0 items-center text-foreground-subtlest [&_svg]:size-3.5">{icon}</span>
        <span className="min-w-0 flex-1 truncate text-ui-sm font-medium text-foreground-subtle">{eyebrow}</span>
        {state && (
          <span
            className={cn(
              'shrink-0 text-ui-sm',
              tone === 'pending' && 'text-warning',
              tone === 'done' && 'text-success',
              tone === 'declined' && 'text-foreground-subtlest',
              tone === 'neutral' && 'text-foreground-subtlest',
            )}
          >
            {state}
          </span>
        )}
      </header>
      <div className="px-3 py-3">{children}</div>
      {error && <p className="px-3 pb-2 text-ui-sm text-destructive">{error}</p>}
      {actions && <footer className="flex flex-wrap items-center justify-end gap-2 border-t border-border/60 bg-surface/40 px-3 py-2">{actions}</footer>}
    </section>
  );
}

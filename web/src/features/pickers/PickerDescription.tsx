import { Fragment } from 'react';
import { cn } from '@/components/lib/utils';
import type { PickerOption } from './options';

const tones = {
  ok: 'bg-success-surface text-success-surface-foreground',
  warn: 'bg-warning-surface text-warning-surface-foreground',
  high: 'bg-destructive-surface text-destructive-surface-foreground',
};

export function PickerDescription({ description, className }: { description: NonNullable<PickerOption['description']>; className?: string }) {
  return <span title={typeof description === 'string' ? description : undefined} className={cn('whitespace-normal break-words text-foreground-subtle', className)}>
    {typeof description === 'string' ? description : description.map(({ text, tone }, i) => (
      <Fragment key={i}>
        {i > 0 && ' · '}
        {tone ? <span className={cn('inline-block rounded px-1 font-medium tabular-nums', tones[tone])}>{text}</span> : text}
      </Fragment>
    ))}
  </span>;
}

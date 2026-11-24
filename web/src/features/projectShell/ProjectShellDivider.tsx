import { useRef } from 'react';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { clampSplitRatio, MAX_SPLIT_RATIO, MIN_SPLIT_RATIO, type projectShellLayout } from './projectShellLayout';

type Divider = ReturnType<typeof projectShellLayout>['dividers'][number];

export function ProjectShellDivider({ divider, onResize }: { divider: Divider; onResize(ratio: number): void }) {
  const drag = useRef<{ pointerId: number; start: number; size: number; ratio: number } | null>(null);
  const { direction, rect, ratio } = divider;
  const columns = direction === 'columns';
  const label = columns ? t('Resize left and right panes', '调整左右窗格比例') : t('Resize top and bottom panes', '调整上下窗格比例');
  return <div
    role="separator" aria-label={label} aria-orientation={columns ? 'vertical' : 'horizontal'}
    aria-valuemin={MIN_SPLIT_RATIO * 100} aria-valuemax={MAX_SPLIT_RATIO * 100} aria-valuenow={Math.round(ratio * 100)}
    tabIndex={0}
    title={label}
    className={cn('absolute z-10 touch-none select-none rounded-sm outline-none hover:bg-ring/45 focus-visible:bg-ring/60 active:bg-ring/60', columns
      ? 'w-2 -translate-x-1/2 cursor-col-resize' : 'h-2 -translate-y-1/2 cursor-row-resize')}
    style={columns
      ? { left: `${rect.x + rect.width * ratio}%`, top: `${rect.y}%`, height: `${rect.height}%` }
      : { left: `${rect.x}%`, top: `${rect.y + rect.height * ratio}%`, width: `${rect.width}%` }}
    onPointerDown={event => {
      if (event.button !== 0 || drag.current) return;
      const bounds = event.currentTarget.parentElement?.getBoundingClientRect();
      if (!bounds) return;
      const size = columns ? bounds.width * rect.width / 100 : bounds.height * rect.height / 100;
      if (!size) return;
      drag.current = { pointerId: event.pointerId, start: columns ? event.clientX : event.clientY, ratio, size };
      event.currentTarget.setPointerCapture(event.pointerId);
    }}
    onPointerMove={event => {
      const start = drag.current;
      if (!start || start.pointerId !== event.pointerId) return;
      const position = columns ? event.clientX : event.clientY;
      onResize(clampSplitRatio(start.ratio + (position - start.start) / start.size));
    }}
    onLostPointerCapture={event => { if (drag.current?.pointerId === event.pointerId) drag.current = null; }}
    onDoubleClick={() => onResize(0.5)}
    onKeyDown={event => {
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      const step = event.shiftKey ? 0.1 : 0.02;
      let next: number;
      if (event.key === (columns ? 'ArrowLeft' : 'ArrowUp')) next = ratio - step;
      else if (event.key === (columns ? 'ArrowRight' : 'ArrowDown')) next = ratio + step;
      else if (event.key === 'Home') next = MIN_SPLIT_RATIO;
      else if (event.key === 'End') next = MAX_SPLIT_RATIO;
      else return;
      event.preventDefault();
      onResize(clampSplitRatio(next));
    }}
  />;
}

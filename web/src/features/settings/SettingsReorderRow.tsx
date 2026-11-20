import type { ReactNode } from 'react';
import { GripVertical } from 'lucide-react';
import { Reorder, useDragControls } from 'motion/react';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';

export function SettingsReorderRow({ id, name, disabled, onMove, onDragEnd, className, children }: {
  id: string;
  name: string;
  disabled: boolean;
  onMove: (direction: -1 | 1) => void;
  onDragEnd: (cancelled: boolean) => void;
  className?: string;
  children: ReactNode;
}) {
  const controls = useDragControls();
  return <Reorder.Item
    value={id}
    layout="position"
    dragListener={false}
    dragControls={controls}
    dragMomentum={false}
    whileDrag={{ boxShadow: '0 4px 12px rgb(0 0 0 / 0.15)' }}
    onDragEnd={event => onDragEnd(event.type === 'pointercancel')}
    className={cn('relative flex min-w-0 items-center gap-2 bg-card', className)}
  >
    <button type="button" disabled={disabled} aria-label={t(`Reorder ${name}`, `调整 ${name} 顺序`)}
      title={t('Drag to reorder; use arrow keys for keyboard', '拖动排序；键盘可使用方向键')}
      className="flex size-10 shrink-0 touch-none select-none cursor-grab items-center justify-center text-foreground-subtle active:cursor-grabbing disabled:opacity-40"
      onPointerDown={event => { if (!disabled && event.isPrimary && event.button === 0) controls.start(event); }}
      onKeyDown={event => {
        if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return;
        event.preventDefault();
        onMove(event.key === 'ArrowUp' ? -1 : 1);
      }}><GripVertical className="size-4" /></button>
    {children}
  </Reorder.Item>;
}

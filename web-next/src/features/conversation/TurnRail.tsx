// A thin rail on the conversation's left edge (desktop): one tick per user
// turn, the active one emphasised; clicking a tick scrolls to that turn.
import { memo, useEffect, useState, type RefObject } from 'react';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { pickActiveTurn, type TurnEntry } from './fold';

export const TurnRail = memo(function TurnRail({ turns, scroller }: { turns: TurnEntry[]; scroller: RefObject<HTMLElement | null> }) {
  const [active, setActive] = useState<number | undefined>(turns.at(-1)?.seq);
  const [hover, setHover] = useState<{ seq: number; x: number; y: number }>();

  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    let frame = 0;
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const box = el.getBoundingClientRect();
        const offsets = [...el.querySelectorAll<HTMLElement>('[data-turn-seq]')].map(node => ({
          seq: Number(node.dataset.turnSeq), top: node.getBoundingClientRect().top - box.top + el.scrollTop,
        })).filter(entry => Number.isFinite(entry.seq));
        setActive(pickActiveTurn(offsets, el.scrollTop, el.clientHeight, el.scrollHeight) ?? turns.at(-1)?.seq);
      });
    };
    update();
    el.addEventListener('scroll', update, { passive: true });
    window.addEventListener('resize', update);
    return () => { cancelAnimationFrame(frame); el.removeEventListener('scroll', update); window.removeEventListener('resize', update); };
  }, [scroller, turns]);

  if (turns.length < 2) return null;
  const jump = (seq: number) => {
    const node = scroller.current?.querySelector<HTMLElement>(`[data-turn-seq="${seq}"]`);
    node?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  };
  const show = (seq: number, el: HTMLElement) => { const r = el.getBoundingClientRect(); setHover({ seq, x: r.right + 6, y: r.top + r.height / 2 }); };
  const hovered = hover && turns.find(turn => turn.seq === hover.seq);
  return (
    <>
    <nav aria-label={t('Turns', '对话轮次')}
      className="pointer-events-none absolute top-1/2 left-1.5 z-10 flex max-h-[70%] -translate-y-1/2 flex-col items-start overflow-hidden py-2">
      {turns.map(turn => {
        const on = turn.seq === active;
        return (
          <div key={turn.seq} className="pointer-events-auto flex items-center">
            <button type="button" onClick={() => jump(turn.seq)}
              onMouseEnter={e => show(turn.seq, e.currentTarget)} onMouseLeave={() => setHover(h => h?.seq === turn.seq ? undefined : h)}
              onFocus={e => show(turn.seq, e.currentTarget)} onBlur={() => setHover(undefined)}
              aria-label={t(`Turn ${turn.index}: ${turn.summary}`, `第 ${turn.index} 轮：${turn.summary}`)}
              aria-current={on ? 'true' : undefined}
              className="group/tick flex h-2.5 w-6 items-center rounded-sm">
              <span className={cn('h-0.5 rounded-full transition-all duration-150',
                on ? 'w-4 bg-foreground-subtle' : 'w-2.5 bg-border group-hover/tick:w-4 group-hover/tick:bg-foreground-subtlest')} />
            </button>
          </div>
        );
      })}
    </nav>
    {hover && hovered && (
      <span style={{ left: hover.x, top: hover.y }}
        className="pointer-events-none fixed z-50 max-w-72 -translate-y-1/2 truncate rounded-md border border-popover-border bg-popover px-2 py-1 text-ui-sm whitespace-nowrap text-popover-foreground shadow-md">
        <span className="mr-1.5 tabular-nums text-foreground-subtlest">{hovered.index}</span>{hovered.summary}
      </span>
    )}
    </>
  );
});

import { useEffect, useState, type RefObject } from 'react';
import { t } from './i18n';

export type TurnEntry = {
  seq: number;
  index: number;
  summary: string;
};

/** Extract user turns from rendered conversation events for the left navigation rail. */
export function extractTurns(events: readonly { seq: number; kind: string; text?: string; payload?: any }[]): TurnEntry[] {
  const turns: TurnEntry[] = [];
  for (const event of events) {
    if (event.kind !== 'user') continue;
    const raw = (event.text || '').trim();
    if (raw.startsWith('<task-notification>') || raw.startsWith('Another Claude session sent a message:')) continue;
    const singleLine = raw.replace(/\s+/g, ' ').trim();
    const attachmentCount = Array.isArray(event.payload?.attachments) ? event.payload.attachments.length : 0;
    const fallback = attachmentCount > 0 ? t(`[${attachmentCount} attachment(s)]`, `[${attachmentCount} 个附件]`) : t('Message', '消息');
    const summary = singleLine ? (singleLine.length > 56 ? `${singleLine.slice(0, 56)}…` : singleLine) : fallback;
    turns.push({ seq: event.seq, index: turns.length + 1, summary });
  }
  return turns;
}

/** Pick the active turn from relative element offsets inside a scrolling container. */
export function pickActiveTurn(turns: readonly TurnEntry[], offsets: readonly { seq: number; top: number }[], scrollTop: number, clientHeight: number, scrollHeight: number): number | undefined {
  if (!turns.length) return undefined;
  if (!offsets.length) return turns[turns.length - 1].seq;
  if (scrollHeight - scrollTop - clientHeight < 48) return offsets[offsets.length - 1].seq;
  const threshold = scrollTop + Math.min(180, clientHeight * 0.32);
  let active = offsets[0].seq;
  for (const item of offsets) {
    if (item.top <= threshold) active = item.seq;
    else break;
  }
  return active;
}

/** Vertical tick-bar navigation on the left edge of the conversation viewport. */
export function TurnRail({ turns, container }: { turns: readonly TurnEntry[]; container: RefObject<HTMLDivElement | null> }) {
  const [activeSeq, setActiveSeq] = useState<number | undefined>(() => turns[turns.length - 1]?.seq);
  const [hovered, setHovered] = useState<number | null>(null);

  useEffect(() => {
    const el = container.current;
    if (!el || !turns.length) {
      setActiveSeq(turns[turns.length - 1]?.seq);
      return;
    }
    const update = () => {
      const nodes = Array.from(el.querySelectorAll<HTMLElement>('[data-turn-seq]'));
      const containerRect = el.getBoundingClientRect();
      const offsets = nodes.map(node => ({
        seq: Number(node.dataset.turnSeq),
        top: node.getBoundingClientRect().top - containerRect.top + el.scrollTop,
      })).filter(item => Number.isFinite(item.seq));
      setActiveSeq(pickActiveTurn(turns, offsets, el.scrollTop, el.clientHeight, el.scrollHeight));
    };
    update();
    el.addEventListener('scroll', update, { passive: true });
    window.addEventListener('resize', update);
    return () => {
      el.removeEventListener('scroll', update);
      window.removeEventListener('resize', update);
    };
  }, [container, turns]);

  if (!turns.length) return null;

  const jumpTo = (seq: number) => {
    const el = container.current;
    const target = el?.querySelector<HTMLElement>(`[data-turn-seq="${seq}"]`);
    if (!el || !target) return;
    setActiveSeq(seq);
    const containerRect = el.getBoundingClientRect();
    const targetTop = target.getBoundingClientRect().top - containerRect.top + el.scrollTop;
    el.scrollTo({ top: Math.max(0, targetTop - 18), behavior: 'smooth' });
    target.classList.remove('turn-flash');
    void target.offsetWidth;
    target.classList.add('turn-flash');
  };

  const activeIndex = turns.findIndex(turn => turn.seq === activeSeq);

  return (
    <nav className="turn-rail" aria-label={t('Conversation turns', '对话轮次导航')}>
      <div className="turn-rail-list" role="list">
        {turns.map((turn, idx) => {
          const isActive = turn.seq === activeSeq;
          const isPast = activeIndex >= 0 && idx < activeIndex;
          const label = `#${turn.index} · ${turn.summary}`;
          return (
            <button
              key={turn.seq}
              type="button"
              role="listitem"
              className={`turn-rail-tick${isActive ? ' active' : isPast ? ' past' : ''}`}
              aria-label={t(`Turn ${turn.index}: ${turn.summary}`, `第 ${turn.index} 轮：${turn.summary}`)}
              aria-current={isActive ? 'step' : undefined}
              onMouseEnter={() => setHovered(turn.seq)}
              onMouseLeave={() => setHovered(old => old === turn.seq ? null : old)}
              onFocus={() => setHovered(turn.seq)}
              onBlur={() => setHovered(old => old === turn.seq ? null : old)}
              onClick={() => jumpTo(turn.seq)}
            >
              <span className="turn-rail-bar" />
              {hovered === turn.seq && (
                <span className="turn-rail-tooltip" role="tooltip">
                  <strong>#{turn.index}</strong>
                  <span>{turn.summary}</span>
                </span>
              )}
            </button>
          );
        })}
      </div>
    </nav>
  );
}

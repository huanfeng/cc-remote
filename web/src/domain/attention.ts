import type { Session } from '../backend/types.ts';
import { isCompletionPending, type CompletionRead } from './completionRead.ts';

export interface AttentionCounts { waiting: number; done: number; total: number }

/** Attention is actionable waiting work plus genuinely unread completed turns. */
export function attentionCounts(sessions: Session[], recordForSession: (session: Session) => CompletionRead | undefined): AttentionCounts {
  let waiting = 0, done = 0;
  const seen = new Set<string>();
  for (const session of sessions) {
    if (session.archived || seen.has(session.id)) continue;
    seen.add(session.id);
    if (session.status === 'waiting') waiting++;
    else if (isCompletionPending(session, recordForSession(session))) done++;
  }
  return { waiting, done, total: waiting + done };
}

export function attentionTitle(base: string, counts: AttentionCounts, lang: 'en' | 'zh' = 'en'): string {
  const parts = [
    counts.waiting > 0 ? lang === 'zh' ? `${counts.waiting} 待处理` : `${counts.waiting} waiting` : '',
    counts.done > 0 ? lang === 'zh' ? `${counts.done} 待查看` : `${counts.done} unread` : '',
  ].filter(Boolean);
  return parts.length ? `(${parts.join(' · ')}) ${base}` : base;
}

/** A desktop shell adds its aggregate prefix to the child's original identity. */
export const stripAttentionTitle = (title: string): string => title.replace(/^\((?:\d+ (?:waiting|unread|待处理|待查看))(?: · \d+ (?:waiting|unread|待处理|待查看))?\) |^\(\d+\) /, '');

/** Preserve the host glyph; the small contrasting dot remains legible in a tab. */
export function attentionIcon(svg: string, counts: AttentionCounts): string {
  if (!counts.total) return svg;
  const color = counts.waiting > 0 ? '#d97706' : '#16a34a';
  return svg.replace('</svg>', `<circle cx="153" cy="39" r="35" fill="${color}" stroke="#fff" stroke-width="10"/></svg>`);
}

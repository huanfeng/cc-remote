// Question and plan cards parse their call's whole input. The compact listing
// may cut it (or a provider may leave it out of the listed event), so a card
// in the transcript loads its events whole as soon as it appears — once per
// event — and the transcript store swaps them in by seq.
import { useEffect, useState } from 'react';
import { useBackend } from '@/backend/backend';
import type { ToolCall } from '@/domain/toolGroups';
import { mergeEvents, useTranscripts } from '@/store/transcripts';
import { useConversation } from '@/features/conversation/context';

/** Whole loads by transcript key and seq: each event is fetched once, every card waits on the same load. */
const loads = new Map<string, Promise<void>>();

/** The card has what it needs to render: a plan, or at least one question. */
export function hasCardContent(call: ToolCall): boolean {
  const input = call.input ?? {};
  if (call.name === 'ExitPlanMode') return typeof input.plan === 'string' && input.plan.trim() !== '';
  return Array.isArray(input.questions) && input.questions.length > 0;
}

/** Loads a question/plan call's events whole when they were cut or empty; true until that settles. */
export function useWholeCall(call: ToolCall, o: {
  /** The card has its content from elsewhere (a plan found in the transcript). */
  content?: boolean;
  /** Other events the card reads (the Write that saved a plan), when the listing cut them. */
  extra?: number[];
} = {}): boolean {
  const backend = useBackend();
  const { tkey, session, native } = useConversation();
  const content = o.content ?? hasCardContent(call);
  const own = call.truncated?.length ? call.truncated : content ? [] : [call.seq];
  const wanted = [...new Set([...own, ...(o.extra ?? [])])].join(',');
  const [done, setDone] = useState('');
  useEffect(() => {
    if (!wanted) return;
    let live = true;
    Promise.allSettled(wanted.split(',').map(Number).map(seq => {
      const key = `${tkey}#${seq}`;
      let p = loads.get(key);
      if (!p) {
        p = backend.fullEvent(session.id, seq, native ? { native } : undefined)
          .then(event => useTranscripts.getState().patch(tkey, s => ({ events: mergeEvents(s.events, [event]) })));
        // A failed load may be tried again when the card shows next time.
        p.catch(() => loads.delete(key));
        loads.set(key, p);
      }
      return p;
    })).then(() => { if (live) setDone(wanted); });
    return () => { live = false; };
  }, [backend, wanted, native, session.id, tkey]);
  return !!wanted && !content && done !== wanted;
}

// Renders transcript items (top level or a subagent's nested transcript).
import { memo, useMemo } from 'react';
import type { ToolCall, TranscriptItem } from '@/domain/toolGroups';
import { parseCodexStatus } from '@/domain/codexStatus';
import { QuestionCard } from '@/features/requests/QuestionCard';
import { PlanCard } from '@/features/requests/PlanCard';
import { useWholeCall } from '@/features/requests/wholeCall';
import { findPlan } from '@/domain/planSource';
import { useTranscripts } from '@/store/transcripts';
import { useConversation } from './context';
import { AssistantMessage, CompactDivider, ErrorCallout, Recap, TeammateMessage, UserMessage } from './Messages';
import { ToolGroup } from './tools/ToolGroup';
import { AgentCard } from './AgentCard';
import { CodexStatusCard } from './CodexCards';

const StatusItem = memo(function StatusItem({ payload, kind, seq }: { payload: unknown; kind: string; seq: number }) {
  const status = useMemo(() => parseCodexStatus({ seq, kind, payload }), [seq, kind, payload]);
  return status ? <CodexStatusCard status={status} /> : null;
});

/** A question or plan card in the transcript; its call loads whole first if the listing cut it. */
function SpecialCard({ call }: { call: ToolCall }) {
  const { tkey, session, mobile, requestsByCall, interactions } = useConversation();
  const request = requestsByCall.get(call.seq);
  const plan = call.name === 'ExitPlanMode';
  // ExitPlanMode comes with input {}: the plan is in the request or the Write that saved it.
  const events = useTranscripts(s => (plan ? s.byKey[tkey]?.events : undefined));
  const source = useMemo(() => (plan ? findPlan(events ?? [], call.seq, call.input, request?.input) : undefined), [plan, events, call.seq, call.input, request?.input]);
  const loading = useWholeCall(call, plan ? {
    content: !!source?.plan.trim() && !source.truncated,
    extra: source?.truncated && source.seq ? [source.seq] : [],
  } : {});
  const props = { call, request, session, interaction: interactions.get(call.seq), mobile, loading };
  return <div className="my-1 rounded-xl" data-call-id={call.id || undefined}>{plan ? <PlanCard {...props} source={source} /> : <QuestionCard {...props} />}</div>;
}

const Item = memo(function Item({ item, nested }: { item: TranscriptItem; nested: boolean }) {
  const { streamingSeq, openers } = useConversation();
  switch (item.type) {
    case 'tools': return <ToolGroup calls={item.calls} />;
    case 'agent':
      return <AgentCard call={item.call} items={item.items} renderNested={() => <TranscriptItems items={item.items} nested />} />;
    case 'special': return <SpecialCard call={item.call} />;
    case 'event': {
      const e = item.event;
      switch (e.kind) {
        case 'user': return <UserMessage event={e} />;
        case 'assistant': return <AssistantMessage event={e} streaming={!nested && e.seq === streamingSeq} opener={!nested && openers.has(item.key)} />;
        case 'error': return <ErrorCallout event={e} />;
        case 'compact': return <CompactDivider event={e} />;
        case 'recap': return <Recap event={e} />;
        case 'teammate': return <TeammateMessage event={e} />;
        case 'status': return <StatusItem payload={e.payload} kind={e.kind} seq={e.seq} />;
        default: return null;
      }
    }
  }
});

export function TranscriptItems({ items, nested = false }: { items: TranscriptItem[]; nested?: boolean }) {
  return (
    <div className={nested ? 'flex flex-col gap-2' : 'flex flex-col gap-3'}>
      {items.map(item => <Item key={`${item.type}:${item.key}:${item.type === 'event' ? item.event.payload?.part ?? 0 : 0}`} item={item} nested={nested} />)}
    </div>
  );
}

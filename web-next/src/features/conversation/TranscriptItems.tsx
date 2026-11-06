// Renders transcript items (top level or a subagent's nested transcript).
import { memo, useMemo } from 'react';
import type { TranscriptItem } from '@/domain/toolGroups';
import { parseCodexStatus } from '@/domain/codexStatus';
import { QuestionCard } from '@/features/requests/QuestionCard';
import { PlanCard } from '@/features/requests/PlanCard';
import { useConversation } from './context';
import { AssistantMessage, CompactDivider, ErrorCallout, Recap, TeammateMessage, UserMessage } from './Messages';
import { ToolGroup } from './tools/ToolGroup';
import { AgentCard } from './AgentCard';
import { CodexStatusCard } from './CodexCards';

const StatusItem = memo(function StatusItem({ payload, kind, seq }: { payload: unknown; kind: string; seq: number }) {
  const status = useMemo(() => parseCodexStatus({ seq, kind, payload }), [seq, kind, payload]);
  return status ? <CodexStatusCard status={status} /> : null;
});

const Item = memo(function Item({ item, nested }: { item: TranscriptItem; nested: boolean }) {
  const { session, mobile, streamingSeq, openers, requestsByCall, interactions } = useConversation();
  switch (item.type) {
    case 'tools': return <ToolGroup calls={item.calls} />;
    case 'agent':
      return <AgentCard call={item.call} items={item.items} renderNested={() => <TranscriptItems items={item.items} nested />} />;
    case 'special': {
      const request = requestsByCall.get(item.call.seq);
      const props = { call: item.call, request, session, interaction: interactions.get(item.call.seq), mobile };
      return <div className="my-1 rounded-xl" data-call-id={item.call.id || undefined}>{item.call.name === 'ExitPlanMode' ? <PlanCard {...props} /> : <QuestionCard {...props} />}</div>;
    }
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

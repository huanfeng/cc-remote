// A pending request not (yet) tied to a call in the transcript: rendered at
// the bottom of the conversation, just above the composer.
import { memo, useMemo } from 'react';
import type { PendingRequest, Session } from '@/backend/types';
import { requestCall } from '@/domain/toolGroups';
import { ApprovalCard } from './ApprovalCard';
import { QuestionCard } from './QuestionCard';
import { PlanCard } from './PlanCard';

export const RequestCard = memo(function RequestCard({ request, session, mobile }: { request: PendingRequest; session: Session; mobile: boolean }) {
  // A request's content never changes; polling recreates its object.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const call = useMemo(() => requestCall(request), [request.id]);
  if (request.kind === 'question' || request.toolName === 'AskUserQuestion') return <QuestionCard call={call} request={request} session={session} mobile={mobile} />;
  if (request.kind === 'plan' || request.toolName === 'ExitPlanMode') return <PlanCard call={call} request={request} session={session} mobile={mobile} />;
  return <ApprovalCard request={request} session={session} />;
}, (a, b) => a.request.id === b.request.id && a.request.status === b.request.status && a.session === b.session && a.mobile === b.mobile);

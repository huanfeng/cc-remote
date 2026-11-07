// Where an ExitPlanMode card finds its plan. Current Claude Code calls
// ExitPlanMode with input {}: the plan lives in the pending request (the host
// adds plan and planFilePath) and in the Write call that saved it under
// ~/.claude/plans/ just before. Pure, for node tests.

export interface PlanSource {
  plan: string;
  /** The plan file, when known. */
  path?: string;
  /** The Write event the plan came from, and whether the listing cut its text. */
  seq?: number;
  truncated?: boolean;
}

type Event = { seq: number; kind: string; payload?: any; truncated?: boolean };

const PLANS_DIR = /[\\/]\.claude[\\/]plans[\\/]/;

export const isPlanFile = (path: unknown): path is string => typeof path === 'string' && PLANS_DIR.test(path);

const str = (v: unknown) => (typeof v === 'string' ? v : '');

/**
 * The plan of the ExitPlanMode call at callSeq: its own input, else the pending
 * request's input, else the latest Write to a plan file before the call.
 */
export function findPlan(events: readonly Event[], callSeq: number, callInput?: any, requestInput?: any): PlanSource {
  const own = str(callInput?.plan);
  const ownPath = str(callInput?.planFilePath) || str(requestInput?.planFilePath) || undefined;
  if (own.trim()) return { plan: own, path: ownPath };
  const asked = str(requestInput?.plan);
  if (asked.trim()) return { plan: asked, path: ownPath };
  for (let i = events.length - 1; i >= 0; i--) {
    const e = events[i];
    if (e.seq >= callSeq) continue;
    // Only the turn that wrote the plan: an earlier ExitPlanMode closes the search.
    if (e.kind === 'tool' && e.payload?.name === 'ExitPlanMode') break;
    if (e.kind !== 'tool' || e.payload?.name !== 'Write' || !isPlanFile(e.payload?.input?.file_path)) continue;
    return { plan: str(e.payload.input.content), path: ownPath ?? e.payload.input.file_path, seq: e.seq, truncated: !!e.truncated };
  }
  return { plan: '', path: ownPath };
}

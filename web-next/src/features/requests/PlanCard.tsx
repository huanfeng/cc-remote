// ExitPlanMode: the plan to approve, the permission mode to continue in, and
// feedback for "keep planning". Read-only once decided.
import { memo, useState } from 'react';
import { ChevronDownIcon, ListChecksIcon } from 'lucide-react';
import type { PendingRequest, Session } from '@/backend/types';
import type { ToolCall } from '@/domain/toolGroups';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { Markdown } from '@/features/markdown/Markdown';
import { RequestShell, type RequestTone } from './RequestShell';
import { useRequest } from './drafts';
import type { Interaction } from './QuestionCard';

const PLAN_LINES = 16;

const MODES = [
  { value: 'acceptEdits', label: t('Approve and auto-accept edits', '批准并自动接受编辑'), description: t('File edits are no longer confirmed one by one', '文件编辑不再逐个确认') },
  { value: 'default', label: t('Approve and review edits manually', '批准，手动审批编辑'), description: t('Each edit still needs confirmation', '每次编辑仍需确认') },
  { value: 'auto', label: t('Approve and use auto mode', '批准并使用自动模式'), description: t('A classifier decides what needs confirmation', '由分类器自动判断是否需要确认') },
];
const BYPASS = { value: 'bypassPermissions', label: t('Approve and keep bypassing permissions', '批准并继续跳过权限确认'), description: t('The mode this session uses: nothing needs confirmation', '本会话使用的模式：无需任何确认') };

const modeLabel = (mode?: string) => [...MODES, BYPASS].find(m => m.value === mode)?.label;

function planState(call: ToolCall, interaction: Interaction | undefined, interactive: boolean): { label: string; tone: RequestTone } {
  const where = interaction?.outcome === 'web' ? t(' · on the web', ' · 网页提交') : interaction?.outcome === 'terminal' ? t(' · in the terminal', ' · 终端') : '';
  if (interaction?.outcome === 'expired') return { label: t('Expired', '已过期'), tone: 'declined' };
  if (call.status === 'ok') return { label: t('Approved', '已批准') + where, tone: 'done' };
  if (call.status === 'error') return { label: t('Kept planning', '继续规划') + where, tone: 'declined' };
  if (interaction?.outcome === 'handoff') return { label: t('Answering in the terminal', '已交给终端'), tone: 'neutral' };
  if (call.status === 'missing') return { label: t('No decision recorded', '未记录决定'), tone: 'declined' };
  return { label: interactive ? t('Waiting for approval', '等待批准') : t('Waiting in the terminal', '等待终端确认'), tone: 'pending' };
}

export const PlanCard = memo(function PlanCard({ call, request, session, interaction, mobile }: {
  call: ToolCall; request?: PendingRequest; session: Session; interaction?: Interaction; mobile: boolean;
}) {
  const { draft, busy, settled, error, update, answer } = useRequest(request?.id ?? '');
  const [full, setFull] = useState(false);
  const plan = String(request?.input?.plan ?? call.input?.plan ?? '');
  const interactive = !!request && call.status === 'running';
  const modes = session.permissionMode === 'bypassPermissions' ? [BYPASS, ...MODES] : MODES;
  const mode = draft.mode ?? modes[0].value;
  const long = plan.split('\n').length > PLAN_LINES;
  const state = settled && interactive ? { label: t('Submitted', '已提交'), tone: 'done' as const } : planState(call, interaction, interactive || session.mode !== 'terminal');
  return (
    <RequestShell
      icon={<ListChecksIcon />}
      eyebrow={t('Plan', '执行计划')}
      state={state.label}
      tone={state.tone}
      error={error}
      actions={interactive ? (
        <>
          {session.mode === 'terminal' && (
            <Button variant="ghost" size="lg" className="mr-auto text-foreground-subtle" disabled={busy} onClick={() => void answer({ decision: 'terminal' })}>
              {t('Answer in terminal', '在终端回答')}
            </Button>
          )}
          <Button variant="outline" size="lg" disabled={busy} onClick={() => void answer({ decision: 'deny', message: draft.feedback?.trim() || undefined })}>
            {t('Keep planning', '继续规划')}
          </Button>
          <Button size="lg" disabled={busy} onClick={() => void answer({ decision: 'allow', mode })}>{settled ? t('Submitted', '已提交') : t('Approve plan', '批准计划')}</Button>
        </>
      ) : undefined}
    >
      <div className={cn('relative', long && !full && 'max-h-[22rem] overflow-hidden')}>
        <Markdown text={plan || t('(empty plan)', '（空计划）')} />
      </div>
      {long && (
        <button type="button" onClick={() => setFull(!full)}
          className="mt-2 inline-flex items-center gap-1 rounded-md px-1 text-ui-sm text-foreground-subtle hover:text-foreground">
          <ChevronDownIcon className={cn('size-3.5 transition-transform', full && 'rotate-180')} />
          {full ? t('Show less', '收起') : t('Show the whole plan', '显示全部计划')}
        </button>
      )}
      {!interactive && call.status === 'ok' && modeLabel(interaction?.mode) && (
        <p className="mt-3 text-ui-sm text-foreground-subtle">{modeLabel(interaction?.mode)}</p>
      )}
      {interactive && (
        <div className="mt-3 border-t border-border/60 pt-3">
          <div role="radiogroup" aria-label={t('Permission mode after approval', '批准后的权限模式')} className="flex flex-col gap-1">
            {modes.map(option => {
              const selected = option.value === mode;
              return (
                <button key={option.value} type="button" role="radio" aria-checked={selected} disabled={busy}
                  onClick={() => update(d => ({ ...d, mode: option.value }))}
                  className={cn('flex w-full items-start gap-2.5 rounded-lg border px-2.5 py-2 text-left transition-colors hover:bg-surface-hover',
                    mobile && 'min-h-11', selected ? 'border-border-hover bg-selected' : 'border-transparent')}>
                  <span aria-hidden className={cn('mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border',
                    selected ? 'border-foreground bg-foreground' : 'border-border-hover bg-background')}>
                    {selected && <span className="size-1.5 rounded-full bg-background" />}
                  </span>
                  <span className="min-w-0">
                    <span className="block text-ui-base text-foreground">{option.label}</span>
                    <span className="block text-ui-sm text-foreground-subtle">{option.description}</span>
                  </span>
                </button>
              );
            })}
          </div>
          <Textarea
            className={cn('mt-2 min-h-14', mobile && 'text-mobile-input-safe')}
            rows={2}
            aria-label={t('Plan feedback', '计划修改意见')}
            placeholder={t('To change the plan, write your feedback, then choose "Keep planning"', '需要调整时写下修改意见，再点「继续规划」')}
            value={draft.feedback ?? ''}
            disabled={busy}
            onChange={e => { const feedback = e.target.value; update(d => ({ ...d, feedback })); }}
          />
        </div>
      )}
    </RequestShell>
  );
});

// "Allow Bash?" — a tool permission request, with a preview of what would run.
import { memo } from 'react';
import { ShieldQuestionIcon } from 'lucide-react';
import type { PendingRequest, Session } from '@/backend/types';
import { Button } from '@/components/ui/button';
import { t } from '@/lib/i18n';
import { RequestShell } from './RequestShell';
import { useRequest } from './drafts';

function Preview({ request }: { request: PendingRequest }) {
  const input = request.input ?? {};
  if (typeof input.command === 'string') {
    return (
      <pre className="max-h-60 overflow-auto rounded-lg border border-border/60 bg-surface px-3 py-2 font-mono text-[12.5px] leading-relaxed whitespace-pre-wrap break-all text-foreground">
        <span className="text-foreground-subtlest select-none">$ </span>{input.command}
      </pre>
    );
  }
  const path = typeof input.file_path === 'string' ? input.file_path : typeof input.path === 'string' ? input.path : '';
  if (path) return <p className="truncate rounded-lg border border-border/60 bg-surface px-3 py-2 font-mono text-[12.5px] text-foreground" title={path}>{path}</p>;
  const json = JSON.stringify(input, null, 2);
  if (!json || json === '{}') return null;
  return <pre className="max-h-60 overflow-auto rounded-lg border border-border/60 bg-surface px-3 py-2 font-mono text-[12px] leading-relaxed text-foreground-subtle">{json}</pre>;
}

export const ApprovalCard = memo(function ApprovalCard({ request, session }: { request: PendingRequest; session: Session }) {
  const { busy, settled, error, answer } = useRequest(request.id);
  const description = typeof request.input?.description === 'string' ? request.input.description : '';
  return (
    <RequestShell
      icon={<ShieldQuestionIcon />}
      eyebrow={t(`Allow ${request.toolName}?`, `允许运行 ${request.toolName}？`)}
      state={settled ? t('Submitted', '已提交') : t('Waiting for approval', '等待批准')}
      tone={settled ? 'done' : 'pending'}
      error={error}
      actions={
        <>
          {session.mode === 'terminal' && (
            <Button variant="ghost" size="lg" className="mr-auto text-foreground-subtle" disabled={busy} onClick={() => void answer({ decision: 'terminal' })}
              title={t('Withdraw the request from the web and answer it in the terminal', '撤回网页上的请求，改在终端回答')}>
              {t('Answer in terminal', '在终端回答')}
            </Button>
          )}
          <Button variant="outline" size="lg" disabled={busy} onClick={() => void answer({ decision: 'deny' })}>{t('Deny', '拒绝')}</Button>
          <Button size="lg" disabled={busy} onClick={() => void answer({ decision: 'allow' })}>{settled ? t('Submitted', '已提交') : t('Allow once', '允许一次')}</Button>
        </>
      }
    >
      {description && <p className="mb-2 text-ui-base text-foreground">{description}</p>}
      <Preview request={request} />
    </RequestShell>
  );
});

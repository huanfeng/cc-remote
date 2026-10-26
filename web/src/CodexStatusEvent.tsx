import { parseCodexStatus, readCodexUsage, type CodexEvent, type PlanStep, type TokenCounts } from './codexStatus';
import { locale, t } from './i18n';
import './codex-status.css';

const stateLabel = (status: PlanStep['status']) => ({
  pending: t('Pending', '待处理'), inProgress: t('In progress', '进行中'),
  completed: t('Completed', '已完成'), unknown: t('Status unknown', '状态未知'),
})[status];
const MAX_DIFF_LINES = 2000;

/** Read-only progress, distinct from plan approval / interaction cards. */
export function CodexStatusEvent({ event }: { event: CodexEvent }) {
  const status = parseCodexStatus(event);
  if (!status || status.type === 'usage') return null;
  if (status.type === 'unavailable') return <p className="codex-status-note">{status.subject === 'plan' ? t('Plan details unavailable', '计划详情暂不可用') : t('Change details unavailable', '变更详情暂不可用')}</p>;
  const truncated = event.truncated && <p className="codex-status-note">{t('This update was truncated by the server; details may be incomplete.', '此更新已被服务端截断，详情可能不完整。')}</p>;
  if (status.type === 'plan') {
    const completed = status.steps.filter(step => step.status === 'completed').length;
    return <details className="codex-status-card">
      <summary>{t('Plan', '执行计划')} <span className="codex-status-count">{completed}/{status.steps.length} {t('completed', '已完成')}</span></summary>
      <div className="codex-status-body">
        {status.explanation && <p className="codex-plan-explanation">{status.explanation}</p>}
        {status.steps.length ? <ol className="codex-plan-steps">{status.steps.map((step, index) => <li key={index}>
          <span className={`codex-step-state codex-step-${step.status}`}>{stateLabel(step.status)}</span><span>{step.text}</span>
        </li>)}</ol> : <p>{t('No steps reported', '尚未报告计划步骤')}</p>}
        {truncated}
      </div>
    </details>;
  }
  return <details className="codex-status-card">
    <summary>{t('Changes', '代码变更')} <span className="codex-status-count">+{status.added} · −{status.removed}</span></summary>
    <div className="codex-status-body">
      {status.lines.length ? <pre className="codex-diff" tabIndex={0} aria-label={t('Code diff', '代码差异')}><code>{status.lines.slice(0, MAX_DIFF_LINES).map((line, index) => <span key={index} className={line.startsWith('+') ? 'codex-diff-added' : line.startsWith('-') ? 'codex-diff-removed' : line.startsWith('@@') ? 'codex-diff-hunk' : undefined}>{line}{'\n'}</span>)}</code></pre> : <p>{t('No changes reported', '尚未报告代码变更')}</p>}
      {status.lines.length > MAX_DIFF_LINES && <p className="codex-status-note">{t(`${status.lines.length - MAX_DIFF_LINES} more lines omitted from this preview.`, `预览省略了其余 ${status.lines.length - MAX_DIFF_LINES} 行。`)}</p>}
      {truncated}
    </div>
  </details>;
}

/** Mount once in the active Codex session's summary; missing/old-server readings render nothing. */
export function CodexSessionUsage({ events, threadId }: { events: readonly CodexEvent[]; threadId?: string }) {
  const usage = readCodexUsage(events, threadId);
  if (!usage) return null;
  const row = (label: string, value: number | undefined) => value !== undefined && <div className="codex-usage-row" key={label}><dt>{label}</dt><dd>{value.toLocaleString(locale)}</dd></div>;
  const breakdown = (counts: TokenCounts | undefined, last = false) => [
    row(last ? t('Latest model call tokens', '最近一次调用 Token') : t('Session tokens', '会话累计 Token'), counts?.totalTokens),
    row(last ? t('Latest model call input tokens', '最近一次调用输入 Token') : t('Input tokens', '输入 Token'), counts?.inputTokens),
    row(last ? t('Latest model call cached input tokens', '最近一次调用缓存输入 Token') : t('Cached input tokens', '缓存输入 Token'), counts?.cachedInputTokens),
    row(last ? t('Latest model call output tokens', '最近一次调用输出 Token') : t('Output tokens', '输出 Token'), counts?.outputTokens),
    row(last ? t('Latest model call reasoning tokens', '最近一次调用推理 Token') : t('Reasoning tokens', '推理 Token'), counts?.reasoningOutputTokens),
  ];
  return <details className="codex-status-card codex-session-usage" aria-label={t('Codex session usage', 'Codex 会话用量')}>
    <summary>{t('Session usage', '会话用量')}</summary>
    <div className="codex-status-body">
    <dl>{breakdown(usage.total)}{breakdown(usage.last, true)}{row(t('Context window', '上下文窗口容量'), usage.modelContextWindow)}</dl>
    </div>
  </details>;
}

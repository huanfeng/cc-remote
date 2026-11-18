import type { Session } from '../backend/types.ts';

/** Ignore stale live metadata once the turn is no longer running. */
export function sessionRetry(session: Pick<Session, 'status' | 'live'>, language = 'en') {
  const retry = session.status === 'running' ? session.live?.retry : undefined;
  const message = retry?.message?.trim();
  if (!message) return undefined;
  const positive = (value?: number) => typeof value === 'number' && Number.isInteger(value) && value > 0;
  const attempt = positive(retry?.attempt) ? retry!.attempt : undefined;
  const max = positive(retry?.maxAttempts) ? retry!.maxAttempts : undefined;
  const progress = attempt === undefined ? '' : ` · ${attempt}${max === undefined ? '' : `/${max}`}`;
  return {
    label: `${language.startsWith('zh') ? '连接重试中' : 'Retrying connection'}${progress}`,
    message,
  };
}

import type { Session, SessionStatus } from '@/backend/types';
import { lang, t } from '../../lib/i18n.ts';
import { sessionRetry } from '../../domain/sessionRetry.ts';
import { providerName, providerOf } from '../../domain/providers.ts';

export type Tone = 'running' | 'retrying' | 'waiting' | 'failed' | 'done' | 'idle' | 'terminal-idle' | 'interrupted';
type StatusSession = Pick<Session, 'status' | 'activeTasks' | 'live' | 'mode' | 'online'>;

function hasIdleTerminal(s: StatusSession): boolean {
  return (s.status ?? 'idle') === 'idle' && !(s.activeTasks ?? 0) && s.mode === 'terminal' && s.online === true;
}

export function statusTone(s: StatusSession): Tone {
  if (sessionRetry(s)) return 'retrying';
  switch (s.status) {
    case 'running': return 'running';
    case 'waiting': return 'waiting';
    case 'failed': case 'error': return 'failed';
    case 'interrupted': case 'stopped': return 'interrupted';
    case 'completed': return (s.activeTasks ?? 0) > 0 ? 'running' : 'done';
    default: return (s.activeTasks ?? 0) > 0 ? 'running' : hasIdleTerminal(s) ? 'terminal-idle' : 'idle';
  }
}

export function sessionStatusLabel(s: StatusSession): string {
  if (hasIdleTerminal(s)) return t('Idle, terminal still running', '空闲，终端仍在运行');
  return sessionRetry(s, lang)?.label ?? statusLabel(s.status, s.activeTasks);
}

export function statusLabel(status: SessionStatus | undefined, activeTasks = 0): string {
  const base = {
    running: t('Running', '运行中'),
    waiting: t('Waiting for you', '等待你'),
    idle: t('Idle', '空闲'),
    completed: t('Done', '已完成'),
    interrupted: t('Interrupted', '已中断'),
    failed: t('Failed', '失败'),
    stopped: t('Stopped', '已停止'),
    error: t('Error', '出错'),
  }[status ?? 'idle'];
  if (activeTasks > 0 && status !== 'running') return t(`${activeTasks} in background`, `${activeTasks} 个后台任务`);
  return base;
}

/** Tailwind classes for the small status dot. */
export const toneDot: Record<Tone, string> = {
  running: 'bg-icon-blue animate-pulse',
  retrying: 'bg-warning',
  waiting: 'bg-warning',
  failed: 'bg-destructive',
  interrupted: 'bg-foreground-subtlest',
  done: 'bg-success',
  idle: 'bg-foreground-subtlest/50',
  'terminal-idle': 'border-[1.5px] border-violet-600 dark:border-violet-400',
};

/** Classes for a status pill (badge). */
export const tonePill: Record<Tone, string> = {
  running: 'bg-progress-surface text-progress-surface-foreground',
  retrying: 'bg-warning-surface text-warning-surface-foreground',
  waiting: 'bg-warning-surface text-warning-surface-foreground',
  failed: 'bg-destructive-surface text-destructive-surface-foreground',
  interrupted: 'bg-surface text-foreground-subtle',
  done: 'bg-success-surface text-success-surface-foreground',
  idle: 'bg-surface text-foreground-subtle',
  'terminal-idle': 'bg-surface text-violet-700 dark:text-violet-300',
};

export function modeLabel(s: Session): string {
  return s.mode === 'terminal' ? t('Terminal', '终端') : t('Managed', '托管');
}

export function sessionMeta(s: Session): string {
  return [providerName(providerOf(s)), modeLabel(s)].join(' · ');
}

export function folderName(cwd: string): string {
  return cwd.split(/[\\/]/).filter(Boolean).pop() || cwd;
}

/** Shortens a path for display: the last two segments, with ~ for the home directory. */
export function shortPath(cwd: string): string {
  const norm = cwd.replace(/\\/g, '/').replace(/^[A-Za-z]:\/Users\/[^/]+/i, '~').replace(/^\/(home|Users)\/[^/]+/, '~');
  const parts = norm.split('/').filter(Boolean);
  return parts.length > 3 ? `…/${parts.slice(-2).join('/')}` : norm;
}

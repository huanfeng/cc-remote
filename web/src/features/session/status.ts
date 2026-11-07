import type { Session, SessionStatus } from '@/backend/types';
import { t } from '@/lib/i18n';
import { providerName, providerOf } from '@/domain/providers';

export type Tone = 'running' | 'waiting' | 'failed' | 'done' | 'idle' | 'interrupted';

export function statusTone(s: Pick<Session, 'status' | 'activeTasks'>): Tone {
  switch (s.status) {
    case 'running': return 'running';
    case 'waiting': return 'waiting';
    case 'failed': case 'error': return 'failed';
    case 'interrupted': case 'stopped': return 'interrupted';
    case 'completed': return (s.activeTasks ?? 0) > 0 ? 'running' : 'done';
    default: return (s.activeTasks ?? 0) > 0 ? 'running' : 'idle';
  }
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
  waiting: 'bg-warning',
  failed: 'bg-destructive',
  interrupted: 'bg-foreground-subtlest',
  done: 'bg-success',
  idle: 'bg-foreground-subtlest/50',
};

/** Classes for a status pill (badge). */
export const tonePill: Record<Tone, string> = {
  running: 'bg-progress-surface text-progress-surface-foreground',
  waiting: 'bg-warning-surface text-warning-surface-foreground',
  failed: 'bg-destructive-surface text-destructive-surface-foreground',
  interrupted: 'bg-surface text-foreground-subtle',
  done: 'bg-success-surface text-success-surface-foreground',
  idle: 'bg-surface text-foreground-subtle',
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

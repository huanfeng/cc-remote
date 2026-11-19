import type { TaskInfo } from '@/backend/types';

export type TaskKind = 'agent' | 'command' | 'teammate';
export const taskKind = (task: TaskInfo): TaskKind =>
  task.taskType === 'local_bash' ? 'command' : task.taskType === 'in_process_teammate' ? 'teammate' : 'agent';

export const taskRunning = (status: string) => status === 'running' || status === 'pending' || status === 'in_progress';

/** Active work comes first across task types; each task appears exactly once. */
export function taskGroups(tasks: TaskInfo[]): { kind: 'running' | TaskKind; tasks: TaskInfo[] }[] {
  const active = tasks.filter(task => taskRunning(task.status));
  const finished = tasks.filter(task => !taskRunning(task.status));
  return [
    { kind: 'running' as const, tasks: active },
    ...(['agent', 'command', 'teammate'] as const).map(kind => ({ kind, tasks: finished.filter(task => taskKind(task) === kind) })),
  ].filter(group => group.tasks.length > 0);
}

import type { ProjectShell } from '../../backend/types.ts';

// Path spelling is a lookup hint. The host owns project identity and returns aliases.
export function shellPathKey(cwd: string) {
  if (/^(?:[a-z]:[\\/]|\\\\)/i.test(cwd)) return cwd.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
  return cwd.replace(/\/+$/, '') || '/';
}

export function matchesShellPath(shell: ProjectShell, cwd: string): boolean {
  const key = shellPathKey(cwd);
  return [shell.cwd, ...(shell.aliases ?? [])].some(path => shellPathKey(path) === key);
}

export function isRunningProjectShell(shell: ProjectShell): boolean {
  return shell.state === 'running' && Boolean(shell.runId);
}

export function isOpenProjectShell(shell: ProjectShell): boolean {
  return shell.state === 'interrupted' || isRunningProjectShell(shell);
}

/** Count terminal processes and restorable panes, including splits in the same tab. */
export function projectShellCounts(shells: ProjectShell[], cwd: string) {
  if (!cwd) return { total: 0, running: 0 };
  const matchingProjects = new Set(shells.filter(shell => matchesShellPath(shell, cwd)).map(shell => shell.projectKey));
  let total = 0;
  let running = 0;
  for (const shell of shells) {
    if (!matchingProjects.has(shell.projectKey) || !isOpenProjectShell(shell)) continue;
    total++;
    if (isRunningProjectShell(shell)) running++;
  }
  return { total, running };
}

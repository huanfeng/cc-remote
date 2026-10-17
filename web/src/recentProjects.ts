export type ProjectSource = { cwd: string; updatedAt: string };
export type RecentProject = { path: string; name: string; updatedAt: string };

// Windows paths compare case-insensitively and with either slash.
const isWindows = (path: string) => /^[a-z]:[\\/]/i.test(path) || path.startsWith('\\\\');
const trimEnd = (path: string) => path.length > 1 && /[\\/]$/.test(path) && !/^[a-z]:[\\/]$/i.test(path) ? path.slice(0, -1) : path;
export const projectKey = (path: string) => { const p = trimEnd(path.trim()); return isWindows(p) ? p.replace(/\//g, '\\').toLowerCase() : p; };
export const projectName = (path: string) => trimEnd(path).split(/[\\/]/).filter(Boolean).pop() || path;

/**
 * The project directories of recent sessions (CC Remote's and Claude Code's own
 * history), most recently used first, each directory once.
 */
export function recentProjects(sources: ProjectSource[], limit = 8): RecentProject[] {
  const seen = new Map<string, RecentProject>();
  for (const { cwd, updatedAt } of sources) {
    if (!cwd?.trim()) continue;
    const key = projectKey(cwd), old = seen.get(key);
    if (!old || Date.parse(updatedAt) > Date.parse(old.updatedAt)) seen.set(key, { path: trimEnd(cwd.trim()), name: projectName(cwd.trim()), updatedAt });
  }
  return [...seen.values()].sort((a, b) => (Date.parse(b.updatedAt) || 0) - (Date.parse(a.updatedAt) || 0)).slice(0, limit);
}

/** Recent projects matching what is typed (name or path, case-insensitive); all of them while it is empty or picked. */
export function matchProjects(list: RecentProject[], typed: string): RecentProject[] {
  const query = typed.trim().toLowerCase();
  if (!query || list.some(item => projectKey(item.path) === projectKey(typed))) return list;
  return list.filter(item => item.path.toLowerCase().includes(query));
}

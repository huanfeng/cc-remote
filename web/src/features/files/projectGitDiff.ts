export type GitDiffRow = {
  kind: 'meta' | 'hunk' | 'context' | 'added' | 'removed';
  text: string;
  before?: number;
  after?: number;
};

const hunk = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/;

export function parseGitDiff(diff: string, limit = 4000): { rows: GitDiffRow[]; truncated: boolean } {
  const lines = diff ? diff.split('\n', limit + 2) : [];
  if (lines.at(-1) === '') lines.pop();
  const rows: GitDiffRow[] = [];
  let before = 0, after = 0, beforeLeft = 0, afterLeft = 0;
  for (const raw of lines.slice(0, limit)) {
    const line = raw.replace(/\r$/, '');
    const header = hunk.exec(line);
    if (header) {
      before = Number(header[1]);
      beforeLeft = Number(header[2] ?? 1);
      after = Number(header[3]);
      afterLeft = Number(header[4] ?? 1);
      rows.push({ kind: 'hunk', text: line });
    } else if (beforeLeft && afterLeft && line.startsWith(' ')) {
      rows.push({ kind: 'context', text: line.slice(1), before: before++, after: after++ });
      beforeLeft--;
      afterLeft--;
    } else if (afterLeft && line.startsWith('+')) {
      rows.push({ kind: 'added', text: line.slice(1), after: after++ });
      afterLeft--;
    } else if (beforeLeft && line.startsWith('-')) {
      rows.push({ kind: 'removed', text: line.slice(1), before: before++ });
      beforeLeft--;
    } else {
      rows.push({ kind: 'meta', text: line });
    }
  }
  return { rows, truncated: lines.length > limit };
}

export function gitDiffReference(rows: GitDiffRow[], from: number, to: number, side: 'after' | 'before' = 'after') {
  const current = rows.slice(Math.min(from, to), Math.max(from, to) + 1).filter(row => row[side] !== undefined);
  const startLine = current[0]?.[side];
  const endLine = current.at(-1)?.[side];
  // An unchanged gap between hunks is absent from the diff text.
  if (startLine === undefined || endLine === undefined || endLine - startLine + 1 !== current.length) return;
  return { startLine, endLine, text: current.map(row => row.text).join('\n') };
}

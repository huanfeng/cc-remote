import { test } from 'node:test';
import assert from 'node:assert/strict';
import { gitDiffReference, parseGitDiff } from '../src/features/files/projectGitDiff.ts';

test('unified diff maps old and new line numbers across multiple hunks', () => {
  const { rows, truncated } = parseGitDiff([
    '--- a/example', '+++ b/example', '@@ -5,3 +7,4 @@ title',
    ' shared', '-old', '+new', '++++ begins with pluses', ' ',
    '@@ -20 +23 @@', '-last', '+replacement', '\\ No newline at end of file', '',
  ].join('\n'));
  assert.equal(truncated, false);
  assert.deepEqual(rows.filter(row => ['context', 'added', 'removed'].includes(row.kind)), [
    { kind: 'context', text: 'shared', before: 5, after: 7 },
    { kind: 'removed', text: 'old', before: 6 },
    { kind: 'added', text: 'new', after: 8 },
    { kind: 'added', text: '+++ begins with pluses', after: 9 },
    { kind: 'context', text: '', before: 7, after: 10 },
    { kind: 'removed', text: 'last', before: 20 },
    { kind: 'added', text: 'replacement', after: 23 },
  ]);
  assert.equal(rows.at(-1)?.kind, 'meta');
});

test('new and deleted files use the existing side, while metadata stays unnumbered', () => {
  assert.deepEqual(parseGitDiff('@@ -0,0 +1 @@\n+new\n').rows[1], { kind: 'added', text: 'new', after: 1 });
  assert.deepEqual(parseGitDiff('@@ -1 +0,0 @@\n-old\n').rows[1], { kind: 'removed', text: 'old', before: 1 });
  assert.deepEqual(parseGitDiff('Binary files differ\n').rows, [{ kind: 'meta', text: 'Binary files differ' }]);
});

test('rendering limits report when some diff rows are hidden', () => {
  assert.deepEqual(parseGitDiff('', 1), { rows: [], truncated: false });
  assert.deepEqual(parseGitDiff('diff --git a/a b/a\nrename from b\n', 1), {
    rows: [{ kind: 'meta', text: 'diff --git a/a b/a' }], truncated: true,
  });
});

test('a selected diff range cites contiguous new lines and skips removed lines', () => {
  const { rows } = parseGitDiff('@@ -3,3 +7,3 @@\n shared\n-old\n+new\n end\n@@ -20 +24 @@\n-x\n+y\n');
  assert.deepEqual(gitDiffReference(rows, 4, 1), { startLine: 7, endLine: 9, text: 'shared\nnew\nend' });
  assert.equal(gitDiffReference(rows, 2, 2), undefined);
  assert.equal(gitDiffReference(rows, 2, 7), undefined);
  assert.deepEqual(gitDiffReference(rows, 2, 2, 'before'), { startLine: 4, endLine: 4, text: 'old' });
  assert.deepEqual(gitDiffReference(rows, 4, 1, 'before'), { startLine: 3, endLine: 5, text: 'shared\nold\nend' });
});

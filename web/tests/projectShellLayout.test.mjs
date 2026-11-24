import assert from 'node:assert/strict';
import test from 'node:test';
import { clampSplitRatio, projectShellLayout } from '../src/features/projectShell/projectShellLayout.ts';

const panes = [
  { id: 'first', panePath: 'L' },
  { id: 'second', panePath: 'RU' },
  { id: 'third', panePath: 'RD' },
];

test('nested splits fill their own branch and expand when the opposite branch closes', () => {
  const original = projectShellLayout(panes);
  assert.equal(original.direction, 'columns');
  assert.deepEqual(original.rects.get('first'), { x: 0, y: 0, width: 50, height: 100 });
  assert.deepEqual(original.rects.get('third'), { x: 50, y: 50, width: 50, height: 50 });

  const collapsed = projectShellLayout(panes.slice(1));
  assert.equal(collapsed.direction, 'rows');
  assert.deepEqual(collapsed.rects.get('second'), { x: 0, y: 0, width: 100, height: 50 });
  assert.deepEqual(collapsed.rects.get('third'), { x: 0, y: 50, width: 100, height: 50 });
});

test('two-pane layout falls back to the old host direction when paths are absent', () => {
  const legacy = [{ id: 'first' }, { id: 'second' }];
  const { direction, rects, dividers } = projectShellLayout(legacy, 'rows');
  assert.equal(direction, 'rows');
  assert.deepEqual(rects.get('second'), { x: 0, y: 50, width: 100, height: 50 });
  assert.equal(dividers.length, 1);
  assert.deepEqual(projectShellLayout(legacy, 'rows', { [dividers[0].id]: 0.75 }).rects.get('second'),
    { x: 0, y: 75, width: 100, height: 25 });
});

test('each nested divider sizes its own branch and preserves pane membership', () => {
  const [root, nested] = projectShellLayout(panes).dividers;
  const { rects, dividers } = projectShellLayout(panes, 'columns', { [root.id]: 0.65, [nested.id]: 0.25 });
  assert.deepEqual(rects.get('first'), { x: 0, y: 0, width: 65, height: 100 });
  assert.deepEqual(rects.get('second'), { x: 65, y: 0, width: 35, height: 25 });
  assert.deepEqual(rects.get('third'), { x: 65, y: 25, width: 35, height: 75 });
  assert.deepEqual(dividers[1], { id: nested.id, direction: 'rows', rect: { x: 65, y: 0, width: 35, height: 100 }, ratio: 0.25 });
});

test('divider ratio survives compaction and splitting the first pane again', () => {
  const [, nested] = projectShellLayout(panes).dividers;
  const compacted = [{ id: 'second', panePath: 'U' }, { id: 'third', panePath: 'D' }];
  const ratios = { [nested.id]: 0.4 };
  assert.equal(projectShellLayout(compacted, 'columns', ratios).dividers[0].ratio, 0.4);

  const extended = [{ id: 'third', panePath: 'D' }, { id: 'fourth', panePath: 'UR' }, { id: 'second', panePath: 'UL' }];
  assert.equal(projectShellLayout(extended, 'columns', ratios).dividers[0].ratio, 0.4);
});

test('split ratio keeps both sides visible and handles invalid measurements', () => {
  assert.equal(clampSplitRatio(-1), 0.15);
  assert.equal(clampSplitRatio(2), 0.85);
  assert.equal(clampSplitRatio(NaN), 0.5);
});

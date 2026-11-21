import assert from 'node:assert/strict';
import test from 'node:test';
import { projectShellLayout } from '../src/features/projectShell/projectShellLayout.ts';

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
  const { direction, rects } = projectShellLayout([{ id: 'first' }, { id: 'second' }], 'rows');
  assert.equal(direction, 'rows');
  assert.deepEqual(rects.get('second'), { x: 0, y: 50, width: 100, height: 50 });
});

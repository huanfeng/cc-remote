import assert from 'node:assert/strict';
import test from 'node:test';
import { adjustShellFrame, fitShellFrame } from '../src/features/projectShell/projectShellFrame.ts';

test('the default terminal fits and can move away from the bottom-right corner', () => {
  const bounds = { width: 1280, height: 800 };
  const initial = fitShellFrame(null, bounds);
  assert.equal(initial.x + initial.width, 1268);
  assert.equal(initial.y + initial.height, 788);
  assert.deepEqual(adjustShellFrame(initial, 'move', -2000, -2000, bounds), { ...initial, x: 0, y: 0 });
});

test('resizing from opposite corners preserves the stationary corner and respects workspace bounds', () => {
  const bounds = { width: 1280, height: 800 };
  const initial = { x: 150, y: 200, width: 540, height: 360 };
  assert.deepEqual(adjustShellFrame(initial, 'nw', -200, -450, bounds), { x: 0, y: 0, width: 690, height: 560 });
  assert.deepEqual(adjustShellFrame(initial, 'se', -400, -480, bounds), { x: 150, y: 200, width: 340, height: 180 });
  assert.deepEqual(adjustShellFrame(initial, 'ne', 900, 900, bounds), { x: 150, y: 380, width: 1130, height: 180 });
});

test('a saved panel stays reachable after the surrounding workspace shrinks', () => {
  const saved = { x: 800, y: 500, width: 520, height: 400 };
  assert.deepEqual(fitShellFrame(saved, { width: 350, height: 255 }), { x: 0, y: 0, width: 350, height: 255 });
  assert.deepEqual(fitShellFrame(saved, { width: 200, height: 140 }), { x: 0, y: 0, width: 200, height: 140 });
  assert.deepEqual(fitShellFrame(saved, { width: 1600, height: 1000 }), saved);
});

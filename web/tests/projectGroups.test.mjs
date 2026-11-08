import { test } from 'node:test';
import assert from 'node:assert/strict';
import { FOLD_KEEP, projectGroups } from '../src/features/sidebar/projectGroups.ts';

const NOW = Date.UTC(2026, 10, 8);
const day = 86_400_000;
const session = (id, cwd, daysAgo, extra = {}) => ({ id, cwd, title: id, updatedAt: '', lastConversationAt: new Date(NOW - daysAgo * day).toISOString(), ...extra });
const ids = list => list.map(s => s.id);

test('groups keep their order and fold old sessions past the first few', () => {
  const busy = Array.from({ length: 12 }, (_, i) => session(`a${i}`, 'D:\\work\\big', 10 + i));
  const groups = projectGroups([session('b0', '/other', 1), ...busy], NOW);
  assert.deepEqual(groups.map(g => g.key), ['/other', 'd:/work/big']);
  assert.equal(groups[1].shown.length, FOLD_KEEP);
  assert.deepEqual(ids(groups[1].shown), ['a0', 'a1', 'a2', 'a3', 'a4']);
  assert.equal(groups[1].folded.length, 7);
});

test('recent, running, waiting and the open session stay shown', () => {
  const list = [
    ...Array.from({ length: 8 }, (_, i) => session(`r${i}`, '/p', i * 0.5)), // all within 7 days
    session('old', '/p', 30),
    session('run', '/p', 40, { status: 'running' }),
    session('wait', '/p', 41, { status: 'waiting' }),
    session('open', '/p', 50),
    session('older', '/p', 60),
  ];
  const [g] = projectGroups(list, NOW, s => s.id === 'open');
  assert.deepEqual(ids(g.shown), ['r0', 'r1', 'r2', 'r3', 'r4', 'r5', 'r6', 'r7', 'run', 'wait', 'open']);
  assert.deepEqual(ids(g.folded), ['old', 'older']);
});

test('a session with no conversation falls back to its creation time', () => {
  const list = Array.from({ length: 6 }, (_, i) => session(`o${i}`, '/p', 20));
  list.push({ id: 'new', cwd: '/p', title: 'new', updatedAt: '', createdAt: new Date(NOW - day).toISOString() });
  const [g] = projectGroups(list, NOW);
  assert.ok(ids(g.shown).includes('new'));
  assert.deepEqual(ids(g.folded), ['o5']);
});

test('a search shows every match', () => {
  const list = Array.from({ length: 9 }, (_, i) => session(`s${i}`, '/p', 30 + i));
  const [g] = projectGroups(list, NOW, undefined, false);
  assert.equal(g.shown.length, 9);
  assert.equal(g.folded.length, 0);
});

test('folders differing only in case and slashes are one project', () => {
  const groups = projectGroups([session('x', 'C:\\Repo', 1), session('y', 'c:/repo', 2)], NOW);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].cwd, 'C:\\Repo');
});

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { FOLD_KEEP, busy, idleSessions, inProgressSessions, projectGroups } from '../src/features/sidebar/projectGroups.ts';
import { isCompletionPending } from '../src/domain/completionRead.ts';

const NOW = Date.UTC(2026, 10, 8);
const day = 86_400_000;
const session = (id, cwd, daysAgo, extra = {}) => ({ id, cwd, title: id, updatedAt: '', lastConversationAt: new Date(NOW - daysAgo * day).toISOString(), ...extra });
const ids = list => list.map(s => s.id);

test('bulk idle archive preserves unread results and active work, then accepts viewed results', () => {
  const done = session('done', '/p', 1, { status: 'completed', completedAt: 'turn-1' });
  const list = [done, session('idle', '/p', 2, { status: 'idle' }), session('run', '/p', 3, { status: 'running' }),
    session('background', '/p', 4, { status: 'completed', activeTasks: 1 }), session('archived', '/p', 5, { archived: true })];
  assert.deepEqual(ids(idleSessions(list, s => isCompletionPending(s, { completedAt: 'turn-1', viewedAt: '' }))), ['idle']);
  assert.deepEqual(ids(idleSessions(list, s => isCompletionPending(s, { completedAt: 'turn-1', viewedAt: 'turn-1' }))), ['done', 'idle']);
});

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

test('in-progress shortcuts retain order and project membership for running, waiting and background work', () => {
  const list = [
    session('idle', '/one', 0, { status: 'idle' }),
    session('waiting', '/one', 1, { status: 'waiting' }),
    session('running', '/two', 2, { status: 'running' }),
    session('background', '/one', 30, { status: 'completed', activeTasks: 2 }),
    session('done', '/two', 31, { status: 'completed', activeTasks: 0 }),
    session('failed', '/two', 32, { status: 'failed' }),
  ];
  assert.deepEqual(ids(inProgressSessions(list)), ['waiting', 'running', 'background']);
  assert.equal(inProgressSessions(list)[0], list[1], 'shortcuts reuse the original sessions');
  const grouped = projectGroups(list, NOW).flatMap(g => [...g.shown, ...g.folded]);
  assert.deepEqual(new Set(ids(grouped)), new Set(ids(list)), 'virtual rows do not move or remove project rows');
  assert.equal(busy(list[3]), true, 'background work must not be archived as idle');
});

test('in-progress shortcuts exclude archived sessions even when search makes them eligible', () => {
  const archived = session('archived match', '/p', 1, { status: 'running', archived: true });
  const match = session('matching work', '/p', 2, { status: 'waiting' });
  const other = session('other work', '/other', 3, { status: 'running' });
  const matched = [archived, match]; // The caller has applied its title/path search.
  assert.deepEqual(ids(inProgressSessions(matched)), ['matching work']);
  assert.deepEqual(ids(projectGroups(matched, NOW, undefined, false)[0].shown), ['archived match', 'matching work']);
  assert.deepEqual(ids(inProgressSessions([other])), ['other work'], 'shortcuts follow the supplied search scope');
});

test('in-progress shortcuts find work beyond the project page and keep older background work unfolded', () => {
  const list = Array.from({ length: 205 }, (_, i) => session(`idle-${i}`, '/p', 10 + i));
  const older = session('older work', '/p', 300, { status: 'completed', activeTasks: 1 });
  list.push(older);
  assert.deepEqual(ids(inProgressSessions(list)), ['older work']);
  const [project] = projectGroups(list, NOW);
  assert.ok(project.shown.includes(older), 'active background work stays visible in its project too');
  assert.equal(list.length, 206, 'building shortcuts does not mutate the source');
});

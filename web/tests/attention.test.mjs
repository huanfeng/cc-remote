import { test } from 'node:test';
import assert from 'node:assert/strict';
import { attentionCounts, attentionTitle, attentionIcon, stripAttentionTitle } from '../src/domain/attention.ts';

const completion = '2026-11-20T01:00:00Z';
const record = { completedAt: completion, viewedAt: '' };
const session = (id, extra = {}) => ({ id, status: 'completed', completedAt: completion, ...extra });

test('attention counts waiting and unread completed work, excluding archived and active background work', () => {
  const list = [session('done'), session('wait', { status: 'waiting' }), session('wait', { status: 'waiting' }),
    session('old'), session('archived', { archived: true }), session('archived-wait', { status: 'waiting', archived: true }),
    session('background', { activeTasks: 1 }), session('failed', { status: 'failed' }), session('running', { status: 'running' })];
  assert.deepEqual(attentionCounts(list, s => s.id === 'old' ? { ...record, viewedAt: completion } : record), { waiting: 1, done: 1, total: 2 });
});

test('historic baseline has no unread badge and reading clears it without changing waiting', () => {
  const list = [session('done'), session('waiting', { status: 'waiting' })];
  assert.equal(attentionCounts(list, () => undefined).done, 0);
  assert.equal(attentionCounts(list, () => ({ ...record, viewedAt: completion })).done, 0);
  assert.equal(attentionCounts(list, () => record).total, 2);
  assert.deepEqual(attentionCounts([], () => record), { waiting: 0, done: 0, total: 0 });
});

test('titles explain both actionable categories and return to the original identity when clear', () => {
  assert.equal(attentionTitle('Host · CC Remote', { waiting: 2, done: 3, total: 5 }, 'zh'), '(2 待处理 · 3 待查看) Host · CC Remote');
  assert.equal(attentionTitle('Host', { waiting: 0, done: 1, total: 1 }), '(1 unread) Host');
  assert.equal(attentionTitle('Host', { waiting: 0, done: 0, total: 0 }), 'Host');
  assert.equal(stripAttentionTitle('(2 待处理 · 3 待查看) My alias · CC Remote'), 'My alias · CC Remote');
  assert.equal(stripAttentionTitle('(1 unread) My alias'), 'My alias');
  assert.equal(stripAttentionTitle('(2) Legacy host'), 'Legacy host');
  assert.equal(stripAttentionTitle('(Production) Host'), '(Production) Host');
});

test('favicon preserves the host icon, prioritizes waiting, and restores the original when cleared', () => {
  const original = '<svg><path id="host-glyph"/></svg>';
  const waiting = attentionIcon(original, { waiting: 1, done: 3, total: 4 });
  assert.match(waiting, /host-glyph/);
  assert.match(waiting, /#d97706/);
  assert.match(attentionIcon(original, { waiting: 0, done: 1, total: 1 }), /#16a34a/);
  assert.equal(attentionIcon(original, { waiting: 0, done: 0, total: 0 }), original);
});

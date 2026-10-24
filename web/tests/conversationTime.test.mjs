import test from 'node:test';
import assert from 'node:assert/strict';
import { conversationTime, conversationOrder } from '../src/conversationTime.ts';

const now = new Date(2026, 9, 24, 14, 30).getTime();
const iso = (offset) => new Date(now + offset).toISOString();

test('conversation time distinguishes recent, calendar dates, missing and estimated values', () => {
  assert.equal(conversationTime({ id: 'a', lastConversationAt: iso(-30_000) }, now, 'zh').label, '刚刚');
  assert.equal(conversationTime({ id: 'a', lastConversationAt: iso(-12 * 60_000) }, now, 'zh').label, '12 分钟前');
  assert.equal(conversationTime({ id: 'a', lastConversationAt: iso(-2 * 3600_000) }, now, 'zh').label, '12:30');
  const yesterday = new Date(now); yesterday.setDate(yesterday.getDate() - 1);
  assert.equal(conversationTime({ id: 'a', lastConversationAt: yesterday.toISOString() }, now).label, 'Yesterday 14:30');
  assert.equal(conversationTime({ id: 'a', lastConversationAt: new Date(2026, 9, 21, 9, 15).toISOString() }, now).label, '10-21 09:15');
  assert.equal(conversationTime({ id: 'a', lastConversationAt: new Date(2025, 11, 31).toISOString() }, now).label, '2025-12-31');
  assert.equal(conversationTime({ id: 'a', createdAt: iso(0) }, now).label, 'Time unknown');
  assert.equal(conversationTime({ id: 'a', updatedAt: iso(0), lastConversationAt: 'invalid' }, now).label, 'Time unknown');
  const estimated = conversationTime({ id: 'a', lastConversationAt: iso(60_000), conversationTimeEstimated: true }, now);
  assert.equal(estimated.label, '≈ 14:31');
  assert.match(estimated.title, /estimated/);
  assert.equal(estimated.iso, iso(60_000));
});

test('sorting ignores administrative updatedAt and keeps active turns stable', () => {
  const active = new Map();
  const old = { id: 'old', status: 'running', lastConversationAt: iso(-120_000), updatedAt: iso(0) };
  const recent = { id: 'recent', lastConversationAt: iso(-60_000) };
  assert.deepEqual(conversationOrder([old, recent], active).map(x => x.id), ['recent', 'old']);
  const streaming = { ...old, lastConversationAt: iso(0) };
  assert.deepEqual(conversationOrder([streaming, recent], active).map(x => x.id), ['recent', 'old']);
  assert.deepEqual(conversationOrder([{ ...streaming, status: 'completed' }, recent], active).map(x => x.id), ['old', 'recent']);
  assert.equal(active.size, 0);
  assert.deepEqual(conversationOrder([{ id: 'b' }, { id: 'a' }]).map(x => x.id), ['a', 'b']);
});

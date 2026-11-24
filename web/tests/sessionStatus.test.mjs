import assert from 'node:assert/strict';
import test from 'node:test';
import { displaySession, observeCompletion } from '../src/domain/completionRead.ts';
import { sessionStatusLabel, statusTone } from '../src/features/session/status.ts';

const online = { mode: 'terminal', online: true };

test('idle sessions distinguish a running terminal from offline and managed sessions', () => {
  assert.equal(statusTone({ ...online, status: 'idle' }), 'terminal-idle');
  assert.equal(sessionStatusLabel({ ...online, status: 'idle' }), 'Idle, terminal still running');
  assert.equal(statusTone({ mode: 'terminal', online: false, status: 'idle' }), 'idle');
  assert.equal(statusTone({ mode: 'terminal', status: 'idle' }), 'idle');
  assert.equal(statusTone({ mode: 'managed', online: true, status: 'idle' }), 'idle');
});

test('activity and unread completion take precedence over an online terminal', () => {
  for (const [status, tone] of [['running', 'running'], ['waiting', 'waiting'], ['failed', 'failed'], ['interrupted', 'interrupted']]) {
    assert.equal(statusTone({ ...online, status }), tone);
  }
  assert.equal(statusTone({ ...online, status: 'idle', activeTasks: 1 }), 'running');
  const completed = { ...online, status: 'completed', completedAt: '2026-11-21T01:00:00Z' };
  const unread = { completedAt: completed.completedAt, viewedAt: '' };
  assert.equal(statusTone(displaySession(completed, unread)), 'done');
  assert.equal(statusTone(displaySession(completed, observeCompletion(completed))), 'terminal-idle');
});

test('terminal compaction labels only the running session', () => {
  const live = { compacting: true };
  assert.equal(sessionStatusLabel({ ...online, status: 'running', live }), 'Compacting context…');
  assert.equal(sessionStatusLabel({ ...online, status: 'interrupted', live }), 'Interrupted');
});

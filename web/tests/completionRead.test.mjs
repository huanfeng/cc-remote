import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mergeCompletion, observeCompletion, isCompletionPending, displaySession, viewCompletion, canViewCompletion } from '../src/domain/completionRead.ts';

const session = (patch = {}) => ({ id: 'a', title: 'Task', cwd: '/p', updatedAt: '', status: 'completed', completedAt: 'turn-1', completionEventSeq: 42, ...patch });
const unseen = { completedAt: 'turn-1', viewedAt: '' };
const visible = { loaded: true, lastSeq: 42, atBottom: true, visible: true, focused: true, obscured: false };

test('historical completion establishes a read baseline without flooding Done', () => {
  const s = session();
  const r = observeCompletion(s);
  assert.equal(isCompletionPending(s, r), false);
  assert.equal(displaySession(s, r).status, 'idle');
  assert.equal(s.status, 'completed', 'execution result is preserved');
});

test('a shared unread completion is pending on a new device and clears when the host reports it read', () => {
  const s = session({ completionReadAt: '' });
  const oldLocalBaseline = { completedAt: s.completedAt, viewedAt: s.completedAt };
  assert.equal(isCompletionPending(s, observeCompletion(s)), true);
  assert.equal(isCompletionPending(s, observeCompletion(s, oldLocalBaseline)), true);
  const otherDeviceRead = { ...s, completionReadAt: s.completedAt };
  assert.equal(displaySession(otherDeviceRead, oldLocalBaseline).status, 'idle');
  const localView = viewCompletion(s, observeCompletion(s));
  assert.equal(localView.confirmedAt, s.completedAt);
  assert.equal(isCompletionPending(s, localView), false);
  assert.equal(isCompletionPending(session({ completedAt: 'turn-2', completionReadAt: '' }), localView), true);
});

test('a running task completes unread, survives reload and becomes idle only when viewed', () => {
  const running = session({ status: 'running', completedAt: undefined });
  const baseline = observeCompletion(running);
  const s = session();
  const pending = observeCompletion(s, baseline);
  const reloaded = observeCompletion(s, JSON.parse(JSON.stringify(pending)));
  assert.equal(isCompletionPending(s, reloaded), true);
  assert.equal(displaySession(s, reloaded).status, 'completed');
  const read = viewCompletion(s, reloaded);
  assert.equal(isCompletionPending(s, read), false);
  assert.equal(displaySession(s, read).status, 'idle');
});

test('completion while disconnected is detected from its saved baseline', () => {
  const previous = observeCompletion(session());
  const s = session({ completedAt: 'turn-2' });
  assert.equal(isCompletionPending(s, observeCompletion(s, previous)), true);
  assert.equal(isCompletionPending(session({ updatedAt: 'new-settings' }), previous), false);
});

test('background tasks remain in progress until they finish', () => {
  const s = session({ activeTasks: 1 });
  assert.equal(isCompletionPending(s, unseen), false);
  assert.equal(displaySession(s, unseen), s);
  assert.equal(isCompletionPending({ ...s, activeTasks: 0 }, unseen), true);
});

test('a stale acknowledgment cannot consume a newer completion', () => {
  const newer = { ...unseen, completedAt: 'turn-2' };
  assert.equal(viewCompletion(session(), newer), newer);
  assert.equal(isCompletionPending(session({ completedAt: 'turn-2' }), newer), true);
});

test('reading requires latest loaded output, foreground focus, bottom and no overlay', () => {
  assert.equal(canViewCompletion(session(), visible), true);
  for (const patch of [{ loaded: false }, { lastSeq: 41 }, { atBottom: false }, { visible: false }, { focused: false }, { obscured: true }]) {
    assert.equal(canViewCompletion(session(), { ...visible, ...patch }), false, JSON.stringify(patch));
  }
  assert.equal(canViewCompletion(session({ completionEventSeq: undefined }), visible), false);
  assert.equal(canViewCompletion(session({ activeTasks: 1 }), visible), false);
  assert.equal(canViewCompletion(session({ completionEventSeq: 0 }), { ...visible, lastSeq: 0 }), true);
});

test('stale snapshots and stale views never rewind completion or read identities', () => {
  const old = '2026-11-18T01:02:03.1Z';
  const newer = '2026-11-18T01:02:03.120000001Z';
  const latest = { completedAt: newer, viewedAt: newer };
  const stale = { completedAt: old, viewedAt: old };
  assert.equal(mergeCompletion(latest, stale), latest);
  assert.equal(observeCompletion(session({ completedAt: old }), latest), latest);
  assert.equal(isCompletionPending(session({ completedAt: old }), latest), false);
  assert.equal(viewCompletion(session({ completedAt: old }), latest), latest);
  assert.deepEqual(mergeCompletion(stale, latest), latest);
  const nano = '2026-11-18T01:02:03.120000002Z';
  assert.equal(isCompletionPending(session({ completedAt: nano }), latest), true);
});

test('failures, interrupted and waiting states retain their actual meaning', () => {
  for (const status of ['failed', 'error', 'interrupted', 'stopped', 'waiting']) {
    const s = session({ status });
    assert.equal(displaySession(s, unseen), s);
    assert.equal(isCompletionPending(s, unseen), false);
    assert.equal(canViewCompletion(s, visible), false);
  }
});

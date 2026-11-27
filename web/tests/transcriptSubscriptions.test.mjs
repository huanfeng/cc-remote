import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

const hooks = registerHooks({ resolve(specifier, context, next) {
  const mocks = {
    '@/backend/backend': 'export const useBackend = () => {};',
    '@/features/composer/drafts': 'export const useDrafts = { getState: () => ({clear() {}}) };',
    '@/features/requests/drafts': 'export const useRequestDrafts = { getState: () => ({prune() {}}) };',
  };
  if (mocks[specifier]) return { url: 'data:text/javascript,' + encodeURIComponent(mocks[specifier]), shortCircuit: true };
  return next(specifier, context);
} });
const { acquireTranscript, useTranscripts, releaseSession, releaseTranscriptWatches } = await import('../src/store/transcripts.ts');
hooks.deregister();

function fixture() {
  const calls = [];
  const backend = { watchTranscript(id, update, options) {
    const call = { id, update, options, stopped: false };
    calls.push(call);
    return () => { call.stopped = true; };
  } };
  return { calls, backend };
}
const update = (status, seq, requests = []) => ({ session: { id: 'a', status }, events: [{ seq, kind: 'assistant', text: `event-${seq}` }], requests, hasMore: false });

test('A to B to A preserves one live subscription, final content, requests and expanded history', () => {
  const { backend, calls } = fixture();
  const leaveA = acquireTranscript(backend, 'session:a', 'a');
  assert.equal(calls[0].options.isViewing(), true);
  calls[0].update({ ...update('running', 1), reset: true });
  leaveA();
  const leaveB = acquireTranscript(backend, 'session:b', 'b');
  assert.equal(calls[0].stopped, false);
  assert.equal(calls[0].options.isViewing(), false);
  calls[0].update(update('waiting', 2, [{ id: 'approval' }]));
  assert.equal(useTranscripts.getState().byKey['session:a'].requests[0].id, 'approval');
  calls[0].update(update('completed', 3));
  const leaveAgain = acquireTranscript(backend, 'session:a', 'a');
  assert.equal(calls.length, 2, 'returning to A reuses its existing stream');
  const cached = useTranscripts.getState().byKey['session:a'];
  assert.deepEqual(cached.events.map(e => e.seq), [1, 2, 3]);
  assert.equal(cached.session.status, 'completed');
  assert.deepEqual(cached.requests, []);
  assert.equal(calls[0].options.isViewing(), true);
  leaveB(); leaveAgain();
  releaseSession('a');
  assert.equal(calls[0].stopped, true);
  calls[0].update(update('running', 4));
  assert.equal(useTranscripts.getState().byKey['session:a'], undefined, 'late replies cannot revive an evicted cache');
  releaseTranscriptWatches(backend);
  assert.equal(calls[1].stopped, true);
  useTranscripts.setState({ byKey: {} });
});

test('sign-out releases background subscriptions; a new backend never inherits their streams', () => {
  const first = fixture(), second = fixture();
  acquireTranscript(first.backend, 'session:a', 'a')();
  useTranscripts.setState({ byKey: {} });
  assert.equal(first.calls[0].stopped, true);
  acquireTranscript(second.backend, 'session:a', 'a');
  first.calls[0].update(update('completed', 99));
  assert.equal(useTranscripts.getState().byKey['session:a'].loaded, false);
  second.calls[0].update(update('running', 1));
  assert.equal(useTranscripts.getState().byKey['session:a'].session.status, 'running');
  releaseTranscriptWatches(first.backend);
  releaseTranscriptWatches(second.backend);
  useTranscripts.setState({ byKey: {} });
});

test('native history unsubscribes on leaving the view', () => {
  const { backend, calls } = fixture();
  acquireTranscript(backend, 'native:a', 'a', 'codex')();
  assert.equal(calls[0].stopped, true);
  releaseTranscriptWatches(backend);
  useTranscripts.setState({ byKey: {} });
});

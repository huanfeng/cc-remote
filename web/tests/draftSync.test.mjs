import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DRAFT_SYNC_TTL_MS, mergeSynced, pruneTombstones } from '../src/lib/draftSync.ts';

test('mergeSynced: a later edit elsewhere wins; ours stays when newer', () => {
  const local = { drafts: { a: { text: 'mine', at: 10 }, b: { text: 'b', at: 50 } }, removed: {} };
  const r = mergeSynced(local, { drafts: { a: { text: 'theirs', at: 20 }, b: { text: 'old', at: 40 } }, removed: {} });
  assert.equal(r.changed, true);
  assert.equal(r.drafts.a.text, 'theirs');
  assert.equal(r.drafts.b.text, 'b');
});

test('mergeSynced: a removal elsewhere deletes our older copy, not a newer one', () => {
  const local = { drafts: { a: { at: 10 }, b: { at: 30 } }, removed: {} };
  const r = mergeSynced(local, { drafts: {}, removed: { a: 20, b: 20 } });
  assert.equal(r.changed, true);
  assert.deepEqual(Object.keys(r.drafts), ['b']);
  assert.equal(r.removed.a, 20);
  assert.equal(r.removed.b, undefined);
});

test('mergeSynced: an entry older than our tombstone does not come back; no change, no echo', () => {
  const local = { drafts: {}, removed: { a: 30 } };
  const r = mergeSynced(local, { drafts: { a: { at: 20 } }, removed: {} });
  assert.equal(r.changed, false);
  assert.deepEqual(r.drafts, {});
  const same = mergeSynced({ drafts: { a: { at: 5 } }, removed: {} }, { drafts: { a: { at: 5 } }, removed: {} });
  assert.equal(same.changed, false);
});

test('pruneTombstones drops ones past 7 days', () => {
  const now = 1_000_000_000_000;
  assert.deepEqual(pruneTombstones({ a: now - 1000, b: now - DRAFT_SYNC_TTL_MS - 1 }, now), { a: now - 1000 });
  assert.deepEqual(pruneTombstones(undefined, now), {});
});

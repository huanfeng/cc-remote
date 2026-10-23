import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mentionAt } from '../src/mentions.ts';

test('the @word the caret is in', () => {
  assert.deepEqual(mentionAt('@', 1), { start: 0, end: 1, query: '' });
  assert.deepEqual(mentionAt('look at @src/ap', 15), { start: 8, end: 15, query: 'src/ap' });
  assert.deepEqual(mentionAt('look at @src/ap now', 15), { start: 8, end: 15, query: 'src/ap' });
  assert.deepEqual(mentionAt('a\n@doc', 6), { start: 2, end: 6, query: 'doc' });
});

test('no menu outside an @word', () => {
  assert.equal(mentionAt('mail me@example.com', 19), undefined);
  assert.equal(mentionAt('@src done', 9), undefined);
  assert.equal(mentionAt('', 0), undefined);
  assert.equal(mentionAt('@"quoted', 8), undefined);
});

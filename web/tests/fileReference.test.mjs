import assert from 'node:assert/strict';
import test from 'node:test';
import { appendFileReference, fileLineReference } from '../src/features/files/fileReference.ts';

test('a file reference appends to the current draft and keeps literal filename punctuation', () => {
  assert.equal(appendFileReference('Inspect this draft\n', { path: 'docs/a `quoted` name.md' }),
    'Inspect this draft\n\n``docs/a `quoted` name.md``\n');
  assert.equal(appendFileReference('', { path: 'src/main.ts', startLine: 2 }), '`src/main.ts:2`\n');
});

test('selected code is quoted with its range and a fence longer than any in its content', () => {
  const reference = fileLineReference('docs/guide.md', ['heading', '```ts', '  let value', '```', 'footer'], 4, 2);
  assert.deepEqual(reference, { path: 'docs/guide.md', startLine: 2, endLine: 4, text: '```ts\n  let value\n```' });
  assert.equal(appendFileReference('Please explain this', reference), 'Please explain this\n\n`docs/guide.md:2-4`\n~~~\n```ts\n  let value\n```\n~~~\n');
  assert.match(appendFileReference('', { ...reference, context: 'Git 改动前' }), /`docs\/guide\.md:2-4` \(Git 改动前\)/);
});

test('large selections retain their location while bounding the excerpt in the draft', () => {
  const reference = fileLineReference('data.txt', Array(300).fill('x'.repeat(500)), 1, 250);
  const draft = appendFileReference('', reference);
  assert.match(draft, /`data\.txt:1-250`/);
  assert.ok(draft.length < 8100);
  assert.match(draft, /\n…\n```\n$/);
});

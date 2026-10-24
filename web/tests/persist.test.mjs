import { test, before } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

let P;
before(async () => {
  const { build } = await import('esbuild');
  const web = join(dirname(fileURLToPath(import.meta.url)), '..');
  const outdir = join(web, 'node_modules', '.cache', 'persist-test');
  mkdirSync(outdir, { recursive: true });
  const outfile = join(outdir, 'persist.mjs');
  await build({ entryPoints: [join(web, 'src', 'persist.ts')], outfile, bundle: true, format: 'esm', platform: 'node', packages: 'external', logLevel: 'silent' });
  P = await import(pathToFileURL(outfile).href);
});

const DAY = 24 * 60 * 60 * 1000;
const NOW = Date.UTC(2026, 9, 15);
const att = id => ({ id, kind: 'file', name: `${id}.txt`, mime: 'text/plain', size: 3, url: `/api/sessions/s1/uploads/${id}` });
const draft = (text, updatedAt = NOW, extra = {}) => ({ draft: text, attachments: [], questionDrafts: {}, answers: {}, updatedAt, ...extra });

test('Codex history routes, tabs and views survive refresh without colliding with Claude', () => {
  const codex = { id: 'same', native: true, provider: 'codex' };
  assert.deepEqual(P.parseHash(P.formatHash('same', true, 'codex')), codex);
  assert.deepEqual(P.parseKey('native-codex:same'), codex);
  const state = { ...P.emptyUI(), open: ['native:same', 'native-codex:same'], selected: 'native-codex:same' };
  assert.deepEqual(P.deserialize(P.serialize(state, NOW), NOW), state);
  assert.deepEqual(P.forget(state, codex).open, ['native:same']);
  assert.equal(P.forget(state, codex).selected, '');
  const view = { scrollTop: 42, nearBottom: false, expandedTools: [], expandedGroups: {} };
  assert.deepEqual(P.parseViews(P.serializeViews({ 'native-codex:same': view })), { 'native-codex:same': view });
});

test('native thread IDs do not delete or overwrite managed drafts with the same ID', () => {
  const state = { ...P.emptyUI(), open: ['session:same', 'native:same', 'native-codex:same'], selected: 'session:same', drafts: { same: draft('unsent managed message') } };
  const next = P.forget(state, { id: 'same', native: true, provider: 'codex' });
  const restored = P.deserialize(P.serialize(next, NOW), NOW);
  assert.equal(restored.drafts.same.draft, 'unsent managed message');
  assert.equal(restored.selected, 'session:same');
  assert.deepEqual(restored.open, ['session:same', 'native:same']);
});

test('hash routes round-trip and reject malformed input', () => {
  assert.equal(P.formatHash('abc', false), '#/s/abc');
  assert.equal(P.formatHash('x y/中', true), '#/native/x%20y%2F%E4%B8%AD');
  assert.equal(P.formatHash('', false), '');
  assert.deepEqual(P.parseHash('#/s/abc'), { id: 'abc', native: false });
  assert.deepEqual(P.parseHash(P.formatHash('a b', true)), { id: 'a b', native: true });
  for (const bad of ['', '#', '#/', '#/s/', '#/x/abc', '#/s/a/b', '#/s/%E0%A4%A', 's/abc']) assert.equal(P.parseHash(bad), undefined, bad);
  assert.deepEqual(P.parseKey('session:a:b'), { id: 'a:b', native: false });
  assert.deepEqual(P.parseKey('native:n1'), { id: 'n1', native: true });
  assert.equal(P.parseKey(''), undefined);
  assert.equal(P.parseKey('other:x'), undefined);
});

test('deserialize tolerates missing, corrupt and unknown-version data', () => {
  for (const raw of [null, undefined, '', '{', '[]', '"x"', JSON.stringify({ v: 2, open: ['session:a'] }), JSON.stringify({ open: ['session:a'] })]) assert.deepEqual(P.deserialize(raw, NOW), P.emptyUI(), String(raw));
});

test('migrate keeps valid fields and strips invalid ones', () => {
  const state = P.migrate({
    v: 1,
    open: ['session:a', 'bogus', 'session:a', 'native:n', 7],
    selected: 'native:n', sidebarTab: 'native',
    drafts: {
      a: { draft: 'hi', attachments: [att('u1'), { id: 'x', name: 'n', url: 'https://evil/x' }, 'nope'], questionDrafts: { r1: { feedback: 'f' }, r2: 5 }, answers: { r3: { Q: 'A', bad: 1 }, r4: 'x' }, updatedAt: NOW },
      empty: { draft: '', attachments: [], updatedAt: NOW },
      noTime: { draft: 'x' },
    },
  });
  assert.deepEqual(state.open, ['session:a', 'native:n']);
  assert.equal(state.selected, 'native:n');
  assert.equal(state.sidebarTab, 'native');
  assert.deepEqual(Object.keys(state.drafts), ['a']);
  assert.deepEqual(state.drafts.a.attachments, [{ id: 'u1', name: 'u1.txt', url: '/api/sessions/s1/uploads/u1', mime: 'text/plain', size: 3, kind: 'file' }]);
  assert.deepEqual(state.drafts.a.questionDrafts, { r1: { feedback: 'f' } });
  assert.deepEqual(state.drafts.a.answers, { r3: { Q: 'A' } });
});

test('drafts expire after 7 days', () => {
  const state = { ...P.emptyUI(), drafts: { old: draft('old', NOW - 8 * DAY), edge: draft('edge', NOW - 7 * DAY), fresh: draft('fresh', NOW - DAY) } };
  assert.deepEqual(Object.keys(P.expire(state, NOW).drafts).sort(), ['edge', 'fresh']);
  assert.deepEqual(Object.keys(P.deserialize(JSON.stringify(state), NOW).drafts).sort(), ['edge', 'fresh']);
});

test('cap drops the oldest drafts until the state fits', () => {
  const big = 'x'.repeat(400);
  const state = { ...P.emptyUI(), drafts: { c: draft(big, NOW - 1), a: draft(big, NOW - 3), b: draft(big, NOW - 2) } };
  const max = JSON.stringify(state).length - 300;
  const capped = P.cap(state, max);
  assert.deepEqual(Object.keys(capped.drafts).sort(), ['b', 'c']);
  assert.ok(JSON.stringify(capped).length <= max);
  assert.equal(P.cap(state, 10).drafts.c, undefined, 'everything goes when even one draft is too big');
  const out = JSON.parse(P.serialize({ ...state, drafts: { ...state.drafts, stale: draft('s', NOW - 9 * DAY) } }, NOW, max));
  assert.deepEqual(Object.keys(out.drafts).sort(), ['b', 'c']);
});

test('mergeUI writes only changed drafts and replaces the layout', () => {
  const stored = { ...P.emptyUI(), open: ['session:x'], drafts: { other: draft('from another tab'), mine: draft('old'), gone: draft('bye') } };
  const merged = P.mergeUI(stored, { open: ['session:mine'], selected: 'session:mine', sidebarTab: 'session' }, { mine: draft('new'), gone: null, blank: draft('') });
  assert.deepEqual(merged.open, ['session:mine']);
  assert.equal(merged.selected, 'session:mine');
  assert.equal(merged.drafts.other.draft, 'from another tab');
  assert.equal(merged.drafts.mine.draft, 'new');
  assert.equal(merged.drafts.gone, undefined);
  assert.equal(merged.drafts.blank, undefined);
  assert.deepEqual(P.mergeUI(stored, undefined, {}).open, ['session:x'], 'no layout keeps the stored one');
});

test('forget removes a session from the open list, selection and drafts', () => {
  const state = { ...P.emptyUI(), open: ['session:a', 'native:a', 'session:b'], selected: 'session:a', drafts: { a: draft('x'), b: draft('y') } };
  const out = P.forget(state, { id: 'a', native: false });
  assert.deepEqual(out.open, ['native:a', 'session:b']);
  assert.equal(out.selected, '');
  assert.deepEqual(Object.keys(out.drafts), ['b']);
  const nativeOut = P.forget(state, { id: 'a', native: true });
  assert.deepEqual(nativeOut.open, ['session:a', 'session:b']);
  assert.equal(nativeOut.selected, 'session:a');
  assert.deepEqual(Object.keys(nativeOut.drafts), ['a', 'b']);
});

test('draftEmpty treats any text, attachment or pending answer as content', () => {
  const base = { draft: '', attachments: [], questionDrafts: {}, answers: {} };
  assert.equal(P.draftEmpty(base), true);
  assert.equal(P.draftEmpty({ ...base, draft: ' ' }), false);
  assert.equal(P.draftEmpty({ ...base, attachments: [att('u')] }), false);
  assert.equal(P.draftEmpty({ ...base, questionDrafts: { r: {} } }), false);
  assert.equal(P.draftEmpty({ ...base, answers: { r: { q: 'a' } } }), false);
});

test('view state parses defensively and keeps the most recent entries', () => {
  const raw = JSON.stringify({ 'session:a': { scrollTop: 120.5, nearBottom: false, expandedTools: [3, 'x', 4.5, 9], expandedGroups: { 7: true, 8: false, x: true, 9: 'yes' } }, 'native:n': { scrollTop: -5 }, bogus: { scrollTop: 1 }, 'session:b': 'nope' });
  assert.deepEqual(P.parseViews(raw), {
    'session:a': { scrollTop: 120.5, nearBottom: false, expandedTools: [3, 9], expandedGroups: { 7: true, 8: false } },
    'native:n': { scrollTop: 0, nearBottom: true, expandedTools: [], expandedGroups: {} },
  });
  assert.deepEqual(P.parseViews('{oops'), {});
  assert.deepEqual(P.parseViews(null), {});
  const views = Object.fromEntries(Array.from({ length: 5 }, (_, i) => [`session:${i}`, { scrollTop: i, nearBottom: true, expandedTools: [], expandedGroups: {} }]));
  assert.deepEqual(Object.keys(JSON.parse(P.serializeViews(views, 2))), ['session:3', 'session:4']);
});

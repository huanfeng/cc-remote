import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  SendIds, admitFiles, canSteer, commandName, filterCommands, isSendKey, modelArgument, needsSteerConfirmation,
  pruneDrafts, quickSwitchCommand, sendContent, slashQuery, DRAFT_TTL_MS, MAX_ATTACHMENTS, MAX_ATTACHMENT_BYTES,
} from '../src/features/composer/composerLogic.ts';
import { groupModels, modelEfforts, permissionOptions, shortModel } from '../src/features/pickers/options.ts';

test('slash query only at the start and before any whitespace', () => {
  assert.equal(slashQuery('/'), '');
  assert.equal(slashQuery('/com'), 'com');
  assert.equal(slashQuery('/compact now'), undefined);
  assert.equal(slashQuery('hello /x'), undefined);
  assert.equal(slashQuery('/a\nb'), undefined);
  assert.equal(slashQuery(''), undefined);
});

test('commands filter by prefix first, then substring; names with or without slash', () => {
  const cmds = [{ name: '/compact' }, { name: 'clear' }, { name: '/review' }, { name: '/resume' }, { name: '/init' }];
  assert.deepEqual(filterCommands(cmds, '').map(c => c.name), cmds.map(c => c.name));
  assert.deepEqual(filterCommands(cmds, 'c').map(c => commandName(c.name)), ['compact', 'clear']);
  assert.deepEqual(filterCommands(cmds, 'e').map(c => commandName(c.name)), ['clear', 'review', 'resume']);
  assert.deepEqual(filterCommands(cmds, 'RE').map(c => commandName(c.name)), ['review', 'resume']);
});

test('/model and /effort without argument open the quick switch', () => {
  assert.equal(quickSwitchCommand('/model'), 'model');
  assert.equal(quickSwitchCommand(' /effort '), 'effort');
  assert.equal(quickSwitchCommand('/model opus'), undefined);
  assert.equal(quickSwitchCommand('/models'), undefined);
  assert.equal(modelArgument('/model claude-opus-5-5'), 'claude-opus-5-5');
  assert.equal(modelArgument('/model'), undefined);
});

test('a send ID is kept for the same content until cleared', () => {
  let n = 0;
  const ids = new SendIds(() => `id${++n}`);
  assert.equal(ids.idFor('s1', 'hello'), 'id1');
  assert.equal(ids.idFor('s1', 'hello'), 'id1', 'retry after a lost reply reuses the ID');
  assert.equal(ids.idFor('s2', 'hello'), 'id2', 'sessions are independent');
  assert.equal(ids.idFor('s1', 'hello!'), 'id3', 'new content gets a new ID');
  ids.clear('s1');
  assert.equal(ids.get('s1'), undefined);
  assert.equal(ids.idFor('s1', 'hello!'), 'id4', 'after a successful send the next message is new');
  assert.equal(sendContent('hi', ['a', 'b']), 'hi\na\nb');
});

test('random send IDs are 32 hex characters', () => {
  const id = new SendIds().idFor('k', 'x');
  assert.match(id, /^[0-9a-f]{32}$/);
});

test('steer: only for Codex managed running sessions with nothing queued and the host feature', () => {
  const base = { provider: 'codex', mode: 'managed', status: 'running', queued: [] };
  assert.equal(canSteer(['codexSteer'], base), true);
  assert.equal(canSteer([], base), false);
  assert.equal(canSteer(['codexSteer'], { ...base, provider: 'claude' }), false);
  assert.equal(canSteer(['codexSteer'], { ...base, mode: 'terminal' }), false);
  assert.equal(canSteer(['codexSteer'], { ...base, status: 'idle' }), false);
  assert.equal(canSteer(['codexSteer'], { ...base, queued: [{ text: 'x' }] }), false);
  assert.equal(needsSteerConfirmation({ content: 'go on' }, ' go on '), true);
  assert.equal(needsSteerConfirmation({ content: 'go on' }, 'other'), false);
  assert.equal(needsSteerConfirmation(undefined, 'go on'), false);
});

test('attachment limits: 20 MiB each and 8 per message', () => {
  const f = (name, size = 10) => ({ name, size });
  const r = admitFiles([f('a'), f('big', MAX_ATTACHMENT_BYTES + 1), f('b'), f('c')], MAX_ATTACHMENTS - 2);
  assert.deepEqual(r.accepted.map(x => x.name), ['a', 'b']);
  assert.deepEqual(r.tooLarge.map(x => x.name), ['big']);
  assert.deepEqual(r.overLimit.map(x => x.name), ['c']);
  assert.equal(admitFiles([f('x', MAX_ATTACHMENT_BYTES)], 0).accepted.length, 1);
});

test('send key: Enter or Ctrl/⌘+Enter; Shift/Alt and IME never send', () => {
  const k = (o = {}) => ({ key: 'Enter', shiftKey: false, altKey: false, ctrlKey: false, metaKey: false, ...o });
  assert.equal(isSendKey(k(), 'enter'), true);
  assert.equal(isSendKey(k({ shiftKey: true }), 'enter'), false);
  assert.equal(isSendKey(k({ altKey: true }), 'enter'), false);
  assert.equal(isSendKey(k({ ctrlKey: true }), 'enter'), false);
  assert.equal(isSendKey(k(), 'mod-enter'), false);
  assert.equal(isSendKey(k({ ctrlKey: true }), 'mod-enter'), true);
  assert.equal(isSendKey(k({ metaKey: true }), 'mod-enter'), true);
  assert.equal(isSendKey(k({ isComposing: true }), 'enter'), false);
  assert.equal(isSendKey(k({ keyCode: 229 }), 'enter'), false);
  assert.equal(isSendKey(k(), 'enter', true), false);
  assert.equal(isSendKey(k({ key: 'a' }), 'enter'), false);
});

test('drafts expire after 7 days and empty drafts are dropped', () => {
  const now = 1_000_000_000_000;
  const kept = pruneDrafts({
    fresh: { text: 'hi', attachments: [], at: now - 1000 },
    files: { text: '', attachments: [{ id: 'a' }], at: now },
    old: { text: 'stale', attachments: [], at: now - DRAFT_TTL_MS - 1 },
    empty: { text: '', attachments: [], at: now },
  }, now);
  assert.deepEqual(Object.keys(kept).sort(), ['files', 'fresh']);
});

test('models group by source and filter by query', () => {
  const models = [
    { id: 'opus', label: 'Opus', source: 'alias' },
    { id: 'claude-opus-5-5', label: 'Opus 5.5', source: 'api' },
    { id: 'claude-fable-5-1', label: 'Fable 5.1', source: 'recent' },
    { id: 'my-model', label: 'my-model', source: 'settings' },
  ];
  assert.deepEqual(groupModels(models).map(g => g.key), ['alias', 'recent', 'api', 'settings']);
  assert.deepEqual(groupModels(models, 'opus').map(g => [g.key, g.items.length]), [['alias', 1], ['api', 1]]);
});

test('efforts: Codex models advertise their own, Claude uses the provider list', () => {
  const models = [{ id: 'gpt', label: 'gpt', source: 'codex', efforts: ['low', 'high'], isDefault: true }, { id: 'x', label: 'x', source: 'codex' }];
  assert.deepEqual(modelEfforts(models, 'gpt', 'codex'), ['low', 'high']);
  assert.deepEqual(modelEfforts(models, '', 'codex'), ['low', 'high'], 'no model: the default one');
  assert.ok(modelEfforts(models, 'x', 'codex').includes('minimal'));
  assert.deepEqual(modelEfforts(undefined, '', 'claude'), ['low', 'medium', 'high', 'xhigh', 'max']);
});

test('permission options are provider specific and flag dangerous modes', () => {
  const codex = permissionOptions('codex');
  assert.deepEqual(codex.map(o => o.value), ['read-only', 'workspace-write', 'danger-full-access']);
  assert.equal(codex.find(o => o.value === 'danger-full-access').tone, 'danger');
  const claude = permissionOptions('claude', 'customMode');
  assert.equal(claude.find(o => o.value === 'bypassPermissions').tone, 'danger');
  assert.equal(claude.find(o => o.value === 'auto').tone, 'warn');
  assert.equal(claude.find(o => o.value === 'default').tone, undefined);
  assert.equal(claude.at(-1).value, 'customMode', 'an unknown saved mode stays selectable');
});

test('short model names for chips', () => {
  assert.equal(shortModel('claude-opus-5-5'), 'Opus 5.5');
  assert.equal(shortModel('sonnet'), 'Sonnet');
  assert.equal(shortModel('gpt-5.5-codex'), 'gpt-5.5-codex');
  assert.equal(shortModel(undefined), '');
});

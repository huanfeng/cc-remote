import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  buildItems, clipOutput, containsCall, diffLines, editDiff, extractTurns, foldEvents, interactionsByCall, pickActiveTurn,
  placeRequests, slashCommand, turnOpeners,
} from '../src/features/conversation/fold.ts';

let seq = 0;
const at = s => new Date(Date.UTC(2026, 0, 1, 0, 0, s)).toISOString();
const ev = (kind, fields = {}) => ({ seq: ++seq, kind, createdAt: at(seq), ...fields });
const use = (id, name, input = {}, parent) => ev('tool', { payload: { type: 'tool_use', id, name, input, ...(parent ? { parent_tool_use_id: parent } : {}) } });
const res = (id, content = 'ok', isError = false) => ev('tool_result', { payload: { tool_use_id: id, content, is_error: isError } });
const kinds = events => events.map(e => e.kind);

test('assistant deltas merge into one message keyed by the first delta', () => {
  const first = ev('assistant_delta', { text: 'Hel' });
  const { events, streamingSeq } = foldEvents([ev('user', { text: 'hi' }), first, ev('assistant_delta', { text: 'lo' })], true);
  assert.deepEqual(kinds(events), ['user', 'assistant']);
  assert.equal(events[1].text, 'Hello');
  assert.equal(events[1].seq, first.seq);
  assert.equal(streamingSeq, first.seq);
});

test('the final assistant event replaces the streamed text and ends streaming', () => {
  const d = ev('assistant_delta', { text: 'Hel' });
  const { events, streamingSeq } = foldEvents([d, ev('assistant', { text: 'Hello!' })], true);
  assert.equal(events.length, 1);
  assert.equal(events[0].text, 'Hello!');
  assert.equal(events[0].seq, d.seq);
  assert.equal(streamingSeq, undefined);
});

test('a stream is only streaming as the tail of a running session', () => {
  const raw = [ev('assistant_delta', { text: 'a' }), use('t', 'Bash', { command: 'ls' })];
  assert.equal(foldEvents(raw, true).streamingSeq, undefined, 'a tool after it');
  assert.equal(foldEvents([ev('assistant_delta', { text: 'a' })], false).streamingSeq, undefined, 'idle session');
});

test('the source events are never mutated', () => {
  const d = ev('assistant_delta', { text: 'x' });
  foldEvents([d, ev('assistant_delta', { text: 'y' })], true);
  assert.equal(d.text, 'x');
  assert.equal(d.kind, 'assistant_delta');
});

test('task notifications are hidden and old teammate turns are split', () => {
  const { events } = foldEvents([
    ev('user', { text: '  <task-notification>done</task-notification>' }),
    ev('user', { text: 'Another Claude session sent a message:\n<teammate-message teammate_id="bob" color="blue" summary="hi">Hello lead</teammate-message><teammate-message teammate_id="bob">{"type":"idle"}</teammate-message>' }),
  ], false);
  assert.deepEqual(kinds(events), ['teammate']);
  assert.deepEqual(events[0].payload, { name: 'bob', color: 'blue', summary: 'hi', part: 0 });
  assert.equal(events[0].text, 'Hello lead');
});

test('a recap is replaced by the next user turn or recap', () => {
  assert.deepEqual(kinds(foldEvents([ev('recap', { text: 'a' }), ev('recap', { text: 'b' })], false).events), ['recap']);
  assert.deepEqual(kinds(foldEvents([ev('recap', { text: 'a' }), ev('user', { text: 'u' })], false).events), ['user']);
  assert.deepEqual(kinds(foldEvents([ev('user', { text: 'u' }), ev('recap', { text: 'a' })], false).events), ['user', 'recap']);
});

test('status, result and interaction events are not rendered; Codex usage stays out', () => {
  const { events } = foldEvents([
    ev('status', { payload: { subtype: 'status', status: 'compacting' } }), ev('result'), ev('interaction', { payload: {} }),
    ev('status', { payload: { tokenUsage: { total: { totalTokens: 3 } } } }),
  ], false);
  assert.equal(events.length, 0);
});

test('Codex plan and diff snapshots with the same turnId replace earlier ones in place', () => {
  const plan1 = ev('status', { payload: { turnId: 'a', plan: [{ step: 'one', status: 'pending' }] } });
  const diff1 = ev('status', { payload: { turnId: 'a', diff: '+x' } });
  const plan2 = ev('status', { payload: { turnId: 'a', plan: [{ step: 'one', status: 'completed' }] } });
  const plan3 = ev('status', { payload: { turnId: 'b', plan: [{ step: 'two', status: 'pending' }] } });
  const { events } = foldEvents([plan1, diff1, plan2, plan3], false);
  assert.deepEqual(events.map(e => e.seq), [plan2.seq, diff1.seq, plan3.seq]);
});

test('Codex activity deltas fold into one card per item', () => {
  const { events } = foldEvents([
    ev('status', { payload: { subtype: 'item/commandExecution/outputDelta', itemId: 'c', turnId: 't', delta: 'a' } }),
    ev('status', { payload: { subtype: 'item/commandExecution/outputDelta', itemId: 'c', turnId: 't', delta: 'b' } }),
  ], false);
  assert.equal(events.length, 1);
  assert.equal(events[0].payload.content, 'ab');
});

test('subagent events stay out of the main stream and nest under their Agent call', () => {
  const { items } = buildItems([
    ev('assistant_delta', { text: 'main' }), use('a1', 'Agent', { description: 'x' }),
    use('c1', 'Read', { file_path: '/a' }, 'a1'), ev('tool_result', { payload: { tool_use_id: 'c1', content: 'ok', parent_tool_use_id: 'a1' } }),
    ev('assistant', { text: 'sub says', payload: { parent_tool_use_id: 'a1' } }),
  ], { running: true, live: true });
  assert.deepEqual(items.map(i => i.type), ['event', 'agent']);
  assert.equal(items[1].call.status, 'running');
  assert.deepEqual(items[1].items.map(i => i.type), ['tools', 'event']);
});

test('buildItems keeps identities across renders when nothing changed', () => {
  const raw = [ev('user', { text: 'u' }), use('r', 'Read', { file_path: '/a' }), res('r')];
  const first = buildItems(raw, { running: false, live: false }).items;
  const second = buildItems([...raw], { running: false, live: false, prev: first }).items;
  assert.equal(second[1], first[1]);
});

test('turn openers are the first assistant message after each user turn', () => {
  const { items } = buildItems([
    ev('user', { text: 'a' }), ev('assistant', { text: '1' }), use('x', 'Bash', { command: 'ls' }), res('x'), ev('assistant', { text: '2' }),
    ev('user', { text: 'b' }), ev('assistant', { text: '3' }),
  ], { running: false, live: false });
  const openers = turnOpeners(items);
  const texts = items.filter(i => i.type === 'event' && openers.has(i.key)).map(i => i.event.text);
  assert.deepEqual(texts, ['1', '3']);
});

test('turns list user messages with a summary and attachment fallback', () => {
  const { items } = buildItems([ev('user', { text: 'x'.repeat(80) }), ev('user', { text: '', payload: { attachments: [{}, {}] } })], { running: false, live: false });
  const turns = extractTurns(items);
  assert.equal(turns.length, 2);
  assert.equal(turns[0].summary.length, 57);
  assert.match(turns[1].summary, /2/);
});

test('active turn follows the scroll position and snaps to the last at the bottom', () => {
  const offsets = [{ seq: 1, top: 0 }, { seq: 5, top: 600 }, { seq: 9, top: 1500 }];
  assert.equal(pickActiveTurn(offsets, 0, 500, 3000), 1);
  assert.equal(pickActiveTurn(offsets, 500, 500, 3000), 5);
  assert.equal(pickActiveTurn(offsets, 2500, 500, 3000), 9);
});

test('slash commands are recognised at the start of a user turn', () => {
  assert.deepEqual(slashCommand('/plan 前端重做'), { name: 'plan', rest: '前端重做' });
  assert.deepEqual(slashCommand('/compact'), { name: 'compact', rest: '' });
  assert.equal(slashCommand('a /plan'), undefined);
  assert.equal(slashCommand('/usr/bin/ls'), undefined);
});

test('approvals attach to the running call of the same tool; others go to the bottom', () => {
  const { items } = buildItems([ev('user', { text: 'u' }), use('b1', 'Bash', { command: 'rm -rf x' })], { running: false, live: true });
  const approval = { id: 'r1', kind: 'approval', toolName: 'Bash', status: 'pending', input: { command: 'rm -rf x' } };
  const other = { id: 'r2', kind: 'approval', toolName: 'Write', status: 'pending', input: {} };
  const done = { id: 'r3', kind: 'approval', toolName: 'Bash', status: 'resolved', input: {} };
  const placed = placeRequests(items, [approval, other, done]);
  assert.equal(placed.byCall.get(items[1].calls[0].seq)?.id, 'r1');
  assert.deepEqual(placed.loose.map(r => r.id), ['r2']);
});

test('questions attach to their running AskUserQuestion call', () => {
  const input = { questions: [{ question: 'Q?', options: [] }] };
  const { items } = buildItems([use('q', 'AskUserQuestion', input)], { running: false, live: true });
  const placed = placeRequests(items, [{ id: 'r', kind: 'question', toolName: 'AskUserQuestion', status: 'pending', input }]);
  assert.equal(placed.byCall.get(items[0].call.seq)?.id, 'r');
  assert.equal(placed.loose.length, 0);
});

test('interaction outcomes belong to the latest matching call before them', () => {
  const raw = [use('p', 'ExitPlanMode', { plan: 'x' }), res('p', 'approved'), ev('interaction', { payload: { toolName: 'ExitPlanMode', outcome: 'web', decision: 'allow', mode: 'acceptEdits' } })];
  const { items } = buildItems(raw, { running: false, live: false });
  const map = interactionsByCall(items, raw);
  assert.deepEqual(map.get(items[0].call.seq), { outcome: 'web', decision: 'allow', mode: 'acceptEdits' });
});

test('long output is clipped by lines and characters', () => {
  const lines = Array.from({ length: 100 }, (_, i) => `line ${i}`).join('\n');
  const a = clipOutput(lines);
  assert.equal(a.clipped, true);
  assert.equal(a.text.split('\n').length, 60);
  assert.equal(a.hiddenLines, 40);
  const b = clipOutput('x'.repeat(7000));
  assert.equal(b.text.length, 6000);
  assert.equal(clipOutput('short').clipped, false);
});

test('edit diffs keep shared context and mark the changed middle', () => {
  const out = editDiff('a\nb\nc\nd', 'a\nB\nc\nd', 1);
  assert.deepEqual(out, [{ type: 'ctx', text: 'a' }, { type: 'del', text: 'b' }, { type: 'add', text: 'B' }, { type: 'ctx', text: 'c' }]);
});

test('unified diff lines are classified with headers outside hunks', () => {
  const out = diffLines(['diff --git a/x b/x', '--- a/x', '+++ b/x', '@@ -1 +1 @@', ' same', '-old', '+new', '+++not a header']);
  assert.deepEqual(out.map(l => l.type), ['meta', 'meta', 'meta', 'hunk', 'ctx', 'del', 'add', 'add']);
});

test('containsCall finds calls in groups and nested subagent transcripts', () => {
  const { items } = buildItems([
    use('top', 'Read', { file_path: '/a' }), res('top'), use('ag', 'Agent', { description: 'x' }),
    use('deep', 'Grep', { pattern: 'p' }, 'ag'),
  ], { running: false, live: false });
  assert.equal(containsCall(items, 'top'), true);
  assert.equal(containsCall(items, 'ag'), true);
  assert.equal(containsCall(items, 'deep'), true);
  assert.equal(containsCall(items, 'nope'), false);
});

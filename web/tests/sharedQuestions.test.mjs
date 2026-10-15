import { test } from 'node:test';
import assert from 'node:assert/strict';
import { groupTranscript, linkOutcomes, linkRequests, questionKey, requestCall, requestForCall, requestSettled } from '../src/toolGroups.ts';

let seq = 0;
const at = new Date(Date.UTC(2026, 0, 1)).toISOString();
const ask = text => ({ questions: [{ question: text, header: 'H', options: [{ label: 'A' }, { label: 'B' }] }] });
const use = (id, name, input = {}) => ({ seq: ++seq, kind: 'tool', payload: { type: 'tool_use', id, name, input }, createdAt: at });
const res = id => ({ seq: ++seq, kind: 'tool_result', text: '"Q1"="A"', payload: { type: 'tool_result', tool_use_id: id, content: '"Q1"="A"' }, createdAt: at });
const outcome = (toolName, value, input) => ({ seq: ++seq, kind: 'interaction', text: value, payload: { toolName, outcome: value, input }, createdAt: at });
const pending = (id, toolName, input) => ({ id, kind: toolName === 'ExitPlanMode' ? 'plan' : 'question', toolName, input, status: 'pending' });
const specials = items => items.filter(item => item.type === 'special').map(item => item.call);

test('question key ignores option details and key order', () => {
  assert.equal(questionKey({ questions: [{ options: [], question: 'Q1', multiSelect: false }] }), questionKey(ask('Q1')));
  assert.notEqual(questionKey(ask('Q1')), questionKey(ask('Q2')));
  assert.equal(questionKey(undefined), '[]');
});

test('a pending interaction links to its running call only', () => {
  const [call] = specials(groupTranscript([use('a', 'AskUserQuestion', ask('Q1'))], true));
  assert.equal(call.status, 'running');
  assert.equal(requestForCall(call, [pending('r2', 'AskUserQuestion', ask('Q2')), pending('r1', 'AskUserQuestion', ask('Q1'))])?.id, 'r1');
  assert.equal(requestForCall(call, [pending('p', 'ExitPlanMode', { plan: 'x' })]), undefined);
  const [offline] = specials(groupTranscript([use('b', 'AskUserQuestion', ask('Q1'))], false));
  assert.equal(requestForCall(offline, [pending('r1', 'AskUserQuestion', ask('Q1'))]), undefined, 'missing calls are not interactive');
  // ExitPlanMode's transcript input is empty; it matches by tool name.
  const [plan] = specials(groupTranscript([use('c', 'ExitPlanMode', {})], true));
  assert.equal(requestForCall(plan, [pending('p', 'ExitPlanMode', { plan: 'x' })])?.id, 'p');
});

test('requestCall renders an interaction not yet in the transcript', () => {
  const call = requestCall(pending('r', 'ExitPlanMode', { plan: 'do' }));
  assert.equal(call.status, 'running');
  assert.equal(call.input.plan, 'do');
  assert.equal(call.name, 'ExitPlanMode');
});

test('outcomes belong to the latest matching call before them', () => {
  const events = [
    use('a', 'AskUserQuestion', ask('Q1')), res('a'), outcome('AskUserQuestion', 'terminal', ask('Q1')),
    use('p', 'ExitPlanMode'), res('p'), outcome('ExitPlanMode', 'web'),
    use('b', 'AskUserQuestion', ask('Q1')), outcome('AskUserQuestion', 'handoff', ask('Q1')),
    use('c', 'AskUserQuestion', ask('Q2')),
  ];
  const items = groupTranscript(events.filter(event => event.kind !== 'interaction'), true);
  const [a, p, b, c] = specials(items);
  const linked = linkOutcomes(items, events);
  assert.equal(linked.get(a.seq), 'terminal');
  assert.equal(linked.get(p.seq), 'web');
  assert.equal(linked.get(b.seq), 'handoff');
  assert.equal(linked.get(c.seq), undefined, 'a different question gets no outcome');
});

test('each pending interaction links to one running call', () => {
  const items = groupTranscript([use('a', 'AskUserQuestion', ask('Q1')), use('b', 'AskUserQuestion', ask('Q1'))], true);
  const [a, b] = specials(items);
  // Only the last group runs; one request is linked once.
  const linked = linkRequests(items, [pending('r1', 'AskUserQuestion', ask('Q1'))]);
  assert.equal(linked.size, 1);
  assert.equal(linked.get(b.seq)?.id, 'r1');
  assert.equal(linked.get(a.seq), undefined);
});

test('a request whose call already has a later result is settled', () => {
  const before = new Date(Date.UTC(2025, 11, 31)).toISOString();
  const after = new Date(Date.UTC(2026, 0, 2)).toISOString();
  const items = groupTranscript([use('a', 'AskUserQuestion', ask('Q1')), res('a')], true);
  assert.equal(requestSettled({ ...pending('r', 'AskUserQuestion', ask('Q1')), createdAt: before }, items), true, 'answered in the terminal after the request');
  assert.equal(requestSettled({ ...pending('r', 'AskUserQuestion', ask('Q1')), createdAt: after }, items), false, 'an earlier identical answer does not hide a new request');
  assert.equal(requestSettled({ ...pending('r', 'AskUserQuestion', ask('Q2')), createdAt: before }, items), false, 'a different question');
  assert.equal(requestSettled(pending('r', 'AskUserQuestion', ask('Q1')), items), false, 'no creation time');
});

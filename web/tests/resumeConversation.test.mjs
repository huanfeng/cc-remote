import { test } from 'node:test';
import assert from 'node:assert/strict';
import { groupTranscript, requestForCall, linkOutcomes } from '../src/domain/toolGroups.ts';
import { buildItems, placeRequests, interactionsByCall } from '../src/features/conversation/fold.ts';
import { resumeConversationView } from '../src/domain/resumeConversation.ts';

const at = '2026-11-19T00:00:00Z';
const input = { title: 'Resume this conversation?', message: 'This conversation used 94% of its context. Resuming may incur additional usage.', options: ['resume', 'new_conversation', 'compact'].map(id => ({ id, label: 'terminal text' })) };
const tool = { seq: 1, kind: 'tool', createdAt: at, payload: { id: 'r1', name: 'ResumeConversation', input } };
const request = { id: 'r1', kind: 'question', toolName: 'ResumeConversation', input, status: 'pending', createdAt: at };

test('a resume choice is a standalone request bound to its exact call identity', () => {
  const { items } = buildItems([tool], { running: false, live: true });
  assert.equal(items[0].type, 'special');
  const placement = placeRequests(items, [request]);
  assert.equal(placement.byCall.get(1), request);
  assert.deepEqual(placement.loose, []);
  assert.equal(requestForCall(items[0].call, [{ ...request, id: 'other' }]), undefined);
});

test('history preserves the original explanation, valid choices and recorded terminal answer', () => {
  for (const option of ['resume', 'new_conversation', 'compact', '']) {
    const outcome = option ? 'terminal' : 'expired';
    const result = { seq: 2, kind: 'tool_result', createdAt: at, payload: { tool_use_id: 'r1', content: JSON.stringify({ outcome, option }) } };
    const items = groupTranscript([tool, result], false);
    const view = resumeConversationView(items[0].call);
    assert.equal(view.message, input.message);
    assert.deepEqual(view.options.map(option => option.id), ['resume', 'new_conversation', 'compact']);
    assert.equal(view.selected, option);
    assert.equal(view.outcome, outcome);
    assert.equal(view.expired, !option);
    assert.deepEqual(placeRequests(items, [request]).loose, []);
  }
});

test('repeated identical resume prompts never consume another prompt’s request or outcome', () => {
  const second = { ...tool, seq: 3, payload: { ...tool.payload, id: 'r2' } };
  const event = (id, seq, outcome) => ({ seq, kind: 'interaction', createdAt: at, payload: { id, toolName: 'ResumeConversation', input, outcome, answers: { option: 'compact' } } });
  const items = groupTranscript([tool, second], true);
  const events = [event('r1', 4, 'web'), event('r2', 5, 'terminal'), event('unknown', 6, 'expired')];
  const interactions = interactionsByCall(items, events);
  assert.equal(interactions.get(1)?.outcome, 'web');
  assert.equal(interactions.get(3)?.outcome, 'terminal');
  assert.deepEqual([...linkOutcomes(items, events)], [[1, 'web'], [3, 'terminal']]);
  assert.equal(resumeConversationView(items[0].call, interactions.get(1)).selected, 'compact');
  const result = { seq: 2, kind: 'tool_result', createdAt: at, payload: { tool_use_id: 'r1', content: JSON.stringify({ outcome: 'web', option: 'resume' }) } };
  const current = { ...request, id: 'r2' };
  const placement = placeRequests(groupTranscript([tool, result, second], true), [current]);
  assert.equal(placement.byCall.get(3), current);
  assert.deepEqual(placement.loose, []);
});

test('malformed or missing choices do not create executable actions', () => {
  const call = { name: 'ResumeConversation', input: { options: [null, {}, { id: '__proto__' }, { id: 'unknown' }] }, resultText: 'null' };
  assert.deepEqual(resumeConversationView(call).options, []);
  assert.equal(resumeConversationView(call).message, '');
});

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { findPlan, isPlanFile } from '../src/domain/planSource.ts';

const write = (seq, file_path, content, truncated) => ({ seq, kind: 'tool', truncated, payload: { type: 'tool_use', id: `w${seq}`, name: 'Write', input: { file_path, content } } });
const exit = seq => ({ seq, kind: 'tool', payload: { type: 'tool_use', id: `x${seq}`, name: 'ExitPlanMode', input: {} } });

test('plan files under .claude/plans, either separator', () => {
  assert.ok(isPlanFile('/home/u/.claude/plans/fix-usage.md'));
  assert.ok(isPlanFile(['C:', 'Users', 'u', '.claude', 'plans', 'fix.md'].join(String.fromCharCode(92))));
  assert.ok(!isPlanFile('/w/src/plans/a.md'));
  assert.ok(!isPlanFile(undefined));
});

test('own input first, then the request, keeping planFilePath', () => {
  assert.deepEqual(findPlan([], 5, { plan: 'mine' }), { plan: 'mine', path: undefined });
  assert.deepEqual(findPlan([], 5, {}, { plan: 'asked', planFilePath: '/h/.claude/plans/p.md' }), { plan: 'asked', path: '/h/.claude/plans/p.md' });
});

test('else the latest Write to a plan file before the call, marked when cut', () => {
  const events = [
    write(1, '/h/.claude/plans/old.md', 'old plan'),
    exit(2),
    write(3, '/w/notes.md', 'not a plan'),
    write(4, '/h/.claude/plans/new.md', 'draft'),
    write(5, '/h/.claude/plans/new.md', 'final plan', true),
    exit(6),
    write(7, '/h/.claude/plans/later.md', 'after'),
  ];
  assert.deepEqual(findPlan(events, 6, {}), { plan: 'final plan', path: '/h/.claude/plans/new.md', seq: 5, truncated: true });
});

test('an earlier ExitPlanMode closes the search; nothing found is empty', () => {
  const events = [write(1, '/h/.claude/plans/old.md', 'old plan'), exit(2), exit(3)];
  assert.deepEqual(findPlan(events, 3, {}), { plan: '', path: undefined });
  assert.deepEqual(findPlan(events, 2, {}).plan, 'old plan');
});

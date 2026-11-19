import { test } from 'node:test';
import assert from 'node:assert/strict';
import { taskGroups } from '../src/domain/taskGroups.ts';

const task = (id, taskType, status) => ({ id, taskType, status });
const ids = groups => groups.map(group => [group.kind, group.tasks.map(task => task.id)]);

test('running agents and shell commands stay ahead of completed type categories', () => {
  const tasks = [
    ...Array.from({ length: 30 }, (_, i) => task(`done-${i}`, 'local_agent', 'completed')),
    task('shell', 'local_bash', 'running'), task('agent', 'local_agent', 'running'),
    task('old-shell', 'local_bash', 'completed'), task('team', 'in_process_teammate', 'running'),
  ];
  const groups = taskGroups(tasks);
  assert.deepEqual(ids(groups)[0], ['running', ['shell', 'agent', 'team']]);
  assert.equal(groups[1].kind, 'agent');
  assert.deepEqual(ids(groups)[2], ['command', ['old-shell']]);
  assert.equal(groups.flatMap(group => group.tasks).length, tasks.length);
  assert.equal(new Set(groups.flatMap(group => group.tasks.map(task => task.id))).size, tasks.length);
  assert.equal(tasks[0].id, 'done-0', 'input order is untouched');
});

test('pending work stays visible while failures and idle teammates are inactive', () => {
  assert.deepEqual(ids(taskGroups([
    task('pending', 'local_bash', 'pending'), task('progress', 'local_agent', 'in_progress'),
    task('error', 'local_agent', 'failed'), task('interrupted', 'local_bash', 'interrupted'),
    task('idle', 'in_process_teammate', 'idle'),
  ])), [['running', ['pending', 'progress']], ['agent', ['error']], ['command', ['interrupted']], ['teammate', ['idle']]]);
});

test('completed work returns to its type and an empty running group disappears', () => {
  const work = task('agent', 'local_agent', 'running');
  assert.deepEqual(ids(taskGroups([work])), [['running', ['agent']]]);
  assert.deepEqual(ids(taskGroups([{ ...work, status: 'completed' }])), [['agent', ['agent']]]);
  assert.deepEqual(ids(taskGroups([{ ...work, status: 'running' }])), [['running', ['agent']]]);
  assert.deepEqual(taskGroups([]), []);
});

test('unknown task types retain the existing agent fallback', () => {
  assert.deepEqual(ids(taskGroups([task('old', undefined, 'completed'), task('new', 'new_agent_type', 'running')])) , [['running', ['new']], ['agent', ['old']]]);
});

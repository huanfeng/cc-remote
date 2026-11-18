import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sessionRetry } from '../src/domain/sessionRetry.ts';

const message = 'Connection dropped (ECONNRESET) · retrying… · attempt 6/10';
const session = { status: 'running', live: { retry: { message, attempt: 6, maxAttempts: 10 } } };

test('connection retries retain error details and expose progress instead of normal work', () => {
  assert.deepEqual(sessionRetry(session, 'zh-CN'), { label: '连接重试中 · 6/10', message });
  assert.equal(sessionRetry(session).label, 'Retrying connection · 6/10');
  assert.equal(session.status, 'running', 'retry remains stoppable ongoing work');
});

test('resuming normal output and ending a turn remove retry presentation', () => {
  assert.equal(sessionRetry({ status: 'running', live: { activity: 'Thinking…' } }), undefined);
  for (const status of ['completed', 'waiting', 'idle', 'failed', 'error', 'interrupted', 'stopped']) {
    assert.equal(sessionRetry({ ...session, status }), undefined, status);
  }
});

test('last retry is not mistaken for exhausted or failed', () => {
  assert.equal(sessionRetry({ ...session, live: { retry: { message, attempt: 10, maxAttempts: 10 } } }).label, 'Retrying connection · 10/10');
});

test('missing counts and malformed metadata do not render misleading progress', () => {
  assert.equal(sessionRetry({ status: 'running', live: { retry: { message } } }).label, 'Retrying connection');
  assert.equal(sessionRetry({ status: 'running', live: { retry: { message, attempt: 2 } } }).label, 'Retrying connection · 2');
  assert.equal(sessionRetry({ status: 'running', live: { retry: { message, attempt: NaN, maxAttempts: -1 } } }).label, 'Retrying connection');
  assert.equal(sessionRetry({ status: 'running', live: { retry: { message: '  ' } } }), undefined);
});

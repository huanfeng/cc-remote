import { test } from 'node:test';
import assert from 'node:assert/strict';
import { setTimeout as tick } from 'node:timers/promises';
import xterm from '@xterm/xterm';
import { projectShellOutput } from '../src/features/projectShell/projectShellOutput.ts';

test('project shell batches a hot stream through xterm and keeps earlier lines available', async () => {
  const terminal = new xterm.Terminal({ allowProposedApi: true, scrollback: 5000 });
  const applied = [];
  let writes = 0;
  const output = projectShellOutput((bytes, parsed) => {
    writes++;
    terminal.write(bytes, parsed);
  }, seq => applied.push(seq));
  try {
    const encoder = new TextEncoder();
    for (let i = 0; i < 3500; i++) {
      const line = encoder.encode(`log ${i} ${'中'.repeat(27)}\r\n`);
      // Split inside the first UTF-8 character to exercise decoder state.
      const split = line.indexOf(0xe4) + 1;
      output.data(line.subarray(0, split), i * 2 + 1);
      output.data(line.subarray(split), i * 2 + 2);
    }
    await new Promise(resolve => output.barrier(resolve));
    assert.ok(writes < 40, `${writes} calls to xterm.write for 7000 frames`);
    assert.equal(applied.length, writes);
    assert.equal(applied.at(-1), 7000);
    assert.ok(applied.every((seq, i) => i === 0 || seq > applied[i - 1]));
    const buffer = terminal.buffer.active;
    assert.equal(buffer.getLine(0).translateToString(true), `log 0 ${'中'.repeat(27)}`);
    assert.equal(buffer.getLine(3499).translateToString(true), `log 3499 ${'中'.repeat(27)}`);
  } finally {
    output.dispose();
    terminal.dispose();
  }
});

test('resize, reset and reconnect barriers wait for earlier parsed data', async () => {
  const seen = [];
  const pending = [];
  const encode = text => new TextEncoder().encode(text);
  const output = projectShellOutput((bytes, parsed) => {
    seen.push(new TextDecoder().decode(bytes));
    pending.push(parsed);
  }, seq => seen.push(seq));
  try {
    output.data(encode('one'), 1);
    output.data(encode('two'), 2);
    output.barrier(() => seen.push('resize'));
    output.data(encode('three'), 3);
    output.barrier(() => seen.push('connect'));
    await tick(5);
    assert.deepEqual(seen, ['onetwo']);
    pending.shift()();
    await tick(5);
    assert.deepEqual(seen, ['onetwo', 2, 'resize', 'three']);
    pending.shift()();
    await tick(5);
    assert.deepEqual(seen, ['onetwo', 2, 'resize', 'three', 3, 'connect']);
  } finally {
    output.dispose();
  }
});

test('xterm output stays bounded and disposed streams stop acknowledging data', async () => {
  const pending = [];
  const seen = [];
  const output = projectShellOutput((_bytes, parsed) => pending.push(parsed), seq => seen.push(seq));
  const bytes = new Uint8Array(32 << 10);
  for (let seq = 1; seq <= 10; seq++) output.data(bytes, seq);
  await tick(5);
  assert.equal(pending.length, 4);
  pending.shift()();
  await tick(5);
  assert.equal(pending.length, 4);
  output.dispose();
  for (const parsed of pending) parsed();
  await tick(5);
  assert.deepEqual(seen, [1]);
});

test('a large snapshot acknowledges its cursor only after its last parser slice', async () => {
  const pending = [];
  const seen = [];
  const output = projectShellOutput((bytes, parsed) => pending.push({ bytes, parsed }), seq => seen.push(seq));
  try {
    output.data(new Uint8Array(100 << 10), 7);
    await tick(5);
    assert.deepEqual(pending.map(item => item.bytes.length), [32768, 32768, 32768, 4096]);
    for (const item of pending.slice(0, -1)) item.parsed();
    assert.deepEqual(seen, []);
    pending.at(-1).parsed();
    assert.deepEqual(seen, [7]);
  } finally {
    output.dispose();
  }
});

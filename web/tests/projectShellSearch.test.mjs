import assert from 'node:assert/strict';
import { test } from 'node:test';
import xterm from '@xterm/xterm';
import unicode11 from '@xterm/addon-unicode11';
import { findProjectShellMatch } from '../src/features/projectShell/findProjectShellMatch.ts';

async function writeTerminal(t, text, cols = 24) {
  const terminal = new xterm.Terminal({ cols, rows: 3, allowProposedApi: true });
  terminal.loadAddon(new unicode11.Unicode11Addon());
  terminal.unicode.activeVersion = '11';
  t.after(() => terminal.dispose());
  await new Promise(resolve => terminal.write(text, resolve));
  return (query, direction, anchor) => findProjectShellMatch(terminal.buffer.active, cols, query, direction, anchor);
}

test('project Shell searches retained history literally and wraps in both directions', async t => {
  const find = await writeTerminal(t, '[ERR] oldest\r\nskip\r\n[err] middle\r\n[ErR] newest');
  const newest = find('[Err]', 'previous');
  const middle = find('[Err]', 'previous', newest);
  const oldest = find('[Err]', 'previous', middle);
  assert.deepEqual(newest, { x: 0, y: 3, length: 5 });
  assert.deepEqual(middle, { x: 0, y: 2, length: 5 });
  assert.deepEqual(oldest, { x: 0, y: 0, length: 5 });
  assert.deepEqual(find('[Err]', 'previous', oldest), newest);
  assert.deepEqual(find('[Err]', 'next', newest), oldest);
  assert.equal(find('not found', 'next'), undefined);
});

test('project Shell maps wrapped matches and supplementary Unicode to xterm cell coordinates', async t => {
  const find = await writeTerminal(t, 'abcde中文😀x\r\nhard\r\nbreak', 6);
  assert.deepEqual(find('e中', 'previous'), { x: 4, y: 0, length: 4 });
  assert.deepEqual(find('中文', 'next'), { x: 0, y: 1, length: 4 });
  assert.deepEqual(find('😀', 'next'), { x: 4, y: 1, length: 2 });
  assert.deepEqual(find('x', 'next'), { x: 0, y: 2, length: 1 });
  assert.equal(find('hardbreak', 'next'), undefined);
});

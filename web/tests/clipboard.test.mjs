import { test } from 'node:test';
import assert from 'node:assert/strict';
import { copyText } from '../src/clipboard.ts';

async function environment(options, run) {
  const original = new Map(['window', 'navigator', 'document', 'HTMLElement'].map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
  const state = { nodes: [], calls: 0, focused: null, selected: '', restored: [] };
  class Element {
    isConnected = true;
    style = {};
    focus() { state.focused = this; }
    select() { state.selected = this.value; }
    setSelectionRange(start, end) { assert.equal(start, 0); assert.equal(end, this.value.length); }
    remove() { state.nodes.splice(state.nodes.indexOf(this), 1); }
  }
  const previous = new Element();
  const range = { cloneRange() { return this; } };
  const window = { isSecureContext: options.secure, getSelection: () => ({ rangeCount: 1, getRangeAt: () => range, removeAllRanges() {}, addRange(r) { state.restored.push(r); } }) };
  const document = { activeElement: previous, createElement: () => new Element(), body: { appendChild(node) { state.nodes.push(node); } }, execCommand(command) { assert.equal(command, 'copy'); state.calls++; return options.legacy !== false; } };
  for (const [key, value] of Object.entries({ window, document, navigator: { clipboard: options.clipboard }, HTMLElement: Element })) Object.defineProperty(globalThis, key, { configurable: true, value });
  try { await run(state, previous, range); }
  finally { for (const [key, descriptor] of original) { if (descriptor) Object.defineProperty(globalThis, key, descriptor); else delete globalThis[key]; } }
}

test('secure clipboard writes the exact command without creating DOM nodes', async () => {
  let received;
  await environment({ secure: true, clipboard: { async writeText(text) { received = text; } } }, async state => {
    await copyText('cc-remote -- claude');
    assert.equal(received, 'cc-remote -- claude');
    assert.equal(state.calls, 0);
    assert.equal(state.nodes.length, 0);
  });
});

test('HTTP without Clipboard API copies Unicode and multiline text, then restores focus and selection', async () => {
  await environment({ secure: false }, async (state, previous, range) => {
    const command = "cc-remote --data-dir 'D:\\项目 数据' -- claude\n第二行";
    await copyText(command);
    assert.equal(state.selected, command);
    assert.equal(state.calls, 1);
    assert.equal(state.nodes.length, 0);
    assert.equal(state.focused, previous);
    assert.deepEqual(state.restored, [range]);
  });
});

test('a denied modern clipboard falls back to selection-based copying', async () => {
  await environment({ secure: true, clipboard: { async writeText() { throw new Error('denied'); } } }, async state => {
    await copyText('fallback command');
    assert.equal(state.selected, 'fallback command');
    assert.equal(state.calls, 1);
    assert.equal(state.nodes.length, 0);
  });
});

test('a blocked legacy copy reports failure and still removes temporary input', async () => {
  await environment({ secure: false, legacy: false }, async (state, previous) => {
    await assert.rejects(copyText('manual command'), /Automatic copy is unavailable/);
    assert.equal(state.nodes.length, 0);
    assert.equal(state.focused, previous);
  });
});

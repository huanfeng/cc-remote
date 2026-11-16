import { test } from 'node:test';
import assert from 'node:assert/strict';
import { installDesktopTitlebar } from '../src/platform/desktopTitlebar.ts';

function setup(platform = 'windows') {
  const original = { window: globalThis.window, Element: globalThis.Element };
  const commands = [];
  class Element extends EventTarget {
    constructor(blockedBy) { super(); this.blockedBy = blockedBy; }
    closest(selector) {
      if (!this.blockedBy) return null;
      assert(selector.includes(this.blockedBy), `interactive selector includes ${this.blockedBy}`);
      return this;
    }
  }
  globalThis.Element = Element;
  globalThis.window = { __TAURI_INTERNALS__: {
    metadata: { currentWindow: { label: 'main' } },
    invoke: async (command, args) => { commands.push([command, args]); },
  } };
  const header = new Element();
  const stop = installDesktopTitlebar(header, platform);
  const mouse = (type, { target = header, button = 0, detail = 1, x = 100, y = 20 } = {}) => {
    const event = new Event(type, { cancelable: true });
    for (const [key, value] of Object.entries({ target, button, detail, clientX: x, clientY: y })) Object.defineProperty(event, key, { value });
    header.dispatchEvent(event);
    return event;
  };
  return { commands, Element, mouse, stop, cleanup() {
    stop();
    for (const [key, value] of Object.entries(original)) {
      if (value === undefined) delete globalThis[key]; else globalThis[key] = value;
    }
  } };
}

test('title and blank areas drag on the first press, maximize and restore on the second press', () => {
  const h = setup();
  try {
    assert(h.mouse('mousedown', { target: new h.Element() }).defaultPrevented);
    h.mouse('mousedown', { detail: 2 });
    h.mouse('mouseup', { detail: 2 });
    h.mouse('mousedown', { detail: 2 });
    assert.deepEqual(h.commands, [
      ['plugin:window|start_dragging', { label: 'main' }],
      ['plugin:window|toggle_maximize', { label: 'main' }],
      ['plugin:window|toggle_maximize', { label: 'main' }],
    ]);
    h.stop();
    h.mouse('mousedown');
    assert.equal(h.commands.length, 3, 'unmount removes the handlers');
  } finally { h.cleanup(); }
});

test('controls, nested icons, tab reordering and other mouse buttons keep their own behavior', () => {
  const h = setup();
  try {
    for (const selector of ['button', 'a', '[role="tab"]', '[draggable="true"]', '[contenteditable]']) {
      const target = new h.Element(selector);
      for (const detail of [1, 2]) assert(!h.mouse('mousedown', { target, detail }).defaultPrevented);
    }
    for (const button of [1, 2]) h.mouse('mousedown', { button });
    h.mouse('mousedown', { detail: 3 });
    assert.deepEqual(h.commands, []);
  } finally { h.cleanup(); }
});

test('macOS maximizes on release and cancels when the second press moves', () => {
  const h = setup('mac');
  try {
    h.mouse('mousedown', { detail: 2 });
    assert.deepEqual(h.commands, []);
    h.mouse('mouseup', { detail: 2, x: 101 });
    assert.deepEqual(h.commands, []);
    h.mouse('mousedown', { detail: 2 });
    h.mouse('mouseup', { detail: 2 });
    h.mouse('mouseup', { detail: 2 });
    assert.deepEqual(h.commands, [['plugin:window|toggle_maximize', { label: 'main' }]]);
  } finally { h.cleanup(); }
});

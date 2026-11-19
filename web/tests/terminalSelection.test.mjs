import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

test('terminal copies completed selections, preserves them on right-click and reports clipboard failures', async () => {
  class Target {
    listeners = new Map();
    addEventListener(type, fn) { const list = this.listeners.get(type) ?? []; list.push(fn); this.listeners.set(type, list); }
    removeEventListener(type, fn) { this.listeners.set(type, (this.listeners.get(type) ?? []).filter(x => x !== fn)); }
    fire(type, extra = {}) {
      const event = { type, button: 0, preventDefault() { this.defaultPrevented = true; }, stopImmediatePropagation() { this.stopped = true; }, ...extra };
      for (const fn of this.listeners.get(type) ?? []) { fn(event); if (event.stopped) break; }
      return event;
    }
    toggleAttribute() {}
  }
  class Terminal {
    options = {}; unicode = {}; modes = {}; selection = ''; buffer = { active: { viewportY: 0, baseY: 0 } };
    constructor() { globalThis.__selectionTerm = this; }
    loadAddon() {} registerLinkProvider() {} open() {} onData() {} focus() {} reset() {} dispose() {}
    onSelectionChange(fn) { this.selectionChanged = fn; }
    attachCustomKeyEventHandler(fn) { this.key = fn; }
    hasSelection() { return !!this.selection; }
    getSelection() { return this.selection; }
    clearSelection() { this.selection = ''; this.selectionChanged(); }
    select(value) { this.selection = value; this.selectionChanged(); }
  }
  const copied = [], patches = [];
  let failCopy = false;
  globalThis.__selectionTerminalClass = Terminal;
  globalThis.__selectionCopy = async text => { if (failCopy) throw new Error('copy denied'); copied.push(text); };
  globalThis.MutationObserver = globalThis.ResizeObserver = class { observe() {} disconnect() {} };
  globalThis.getComputedStyle = () => ({ getPropertyValue: () => '' });
  globalThis.requestAnimationFrame = () => 1;
  globalThis.cancelAnimationFrame = () => {};
  globalThis.window = { setTimeout };
  globalThis.document = Object.assign(new Target(), { hidden: false, documentElement: {} });
  const data = text => 'data:text/javascript,' + encodeURIComponent(text);
  const mocks = {
    '@xterm/xterm': data('export const Terminal=globalThis.__selectionTerminalClass;'),
    '@xterm/addon-fit': data('export class FitAddon { proposeDimensions() { return null; } }'),
    '@xterm/addon-unicode11': data('export class Unicode11Addon {}'),
    '@xterm/addon-web-links': data('export class WebLinksAddon {}'),
    '@/domain/terminalPaths': data('export const filePathLinks=()=>({});'),
    '@/domain/clipboard': data('export const copyText=text=>globalThis.__selectionCopy(text);'),
    '@/terminal/terminalTheme': data('export const mergeTerminalTheme=()=>({});'),
    '@/lib/i18n': data('export const t=en=>en;'),
  };
  const hook = registerHooks({ resolve(specifier, context, next) {
    if (mocks[specifier]) return { url: mocks[specifier], shortCircuit: true };
    if (specifier === './terminalSize') return { url: new URL('../src/features/terminal/terminalSize.ts', import.meta.url).href, shortCircuit: true };
    return next(specifier, context);
  } });
  let engine;
  const flush = () => new Promise(resolve => setImmediate(resolve));
  try {
    const { createTerminalEngine } = await import('../src/features/terminal/terminalEngine.ts');
    const screen = new Target();
    engine = createTerminalEngine({ backend: { openTerminal: () => ({ close() {} }) }, sessionId: 's', mobile: false,
      box: { clientWidth: 0, clientHeight: 0 }, screen, zoom: () => 1, fitDevice: () => false, openFile() {}, onUi: patch => patches.push(patch) });
    const term = globalThis.__selectionTerm;
    screen.fire('mousedown'); term.select('partial'); term.select('whole selection\n第二行');
    await flush(); assert.deepEqual(copied, [], 'dragging must not copy intermediate text');
    document.fire('mouseup'); await flush();
    assert.deepEqual(copied, ['whole selection\n第二行']);
    assert.equal(term.hasSelection(), true, 'automatic copy keeps selection');
    assert.equal(screen.fire('mousedown', { button: 2 }).stopped, true, 'right-click must not reach Claude mouse reporting');
    const context = screen.fire('contextmenu', { button: 2 }); await flush();
    assert.equal(context.defaultPrevented, true); assert.equal(context.stopped, true);
    assert.equal(term.getSelection(), 'whole selection\n第二行');
    assert.equal(copied.at(-1), 'whole selection\n第二行');
    const copies = copied.length;
    document.fire('mouseup'); await flush();
    assert.equal(copied.length, copies, 'unrelated mouseup does not copy again');
    screen.fire('touchstart'); term.select('mobile selection');
    document.fire('touchend'); await flush();
    assert.equal(copied.at(-1), 'mobile selection');
    const shift = { type: 'keydown', key: 'Shift' };
    term.key(shift); term.select('keyboard selection'); term.key({ ...shift, type: 'keyup' });
    await flush(); assert.equal(copied.at(-1), 'keyboard selection');
    const keyboard = { type: 'keydown', ctrlKey: true, key: 'c', preventDefault() { this.defaultPrevented = true; } };
    assert.equal(term.key(keyboard), false); await flush(); assert.equal(term.hasSelection(), true);
    failCopy = true;
    engine.copySelection(); await flush();
    assert.equal(term.hasSelection(), true, 'failed copy must keep recoverable text');
    assert.ok(patches.some(p => p.copyError), 'clipboard failure is visible');
    term.clearSelection();
    assert.equal(screen.fire('mousedown', { button: 2 }).stopped, undefined, 'without a selection preserve terminal right-click');
    assert.equal(screen.fire('contextmenu', { button: 2 }).stopped, undefined);
    engine.dispose(); engine = undefined;
    term.select('after disposal'); document.fire('mouseup'); await flush();
    assert.equal(copied.includes('after disposal'), false);
  } finally {
    engine?.dispose(); hook.deregister();
    for (const key of ['__selectionTerminalClass', '__selectionTerm', '__selectionCopy', 'MutationObserver', 'ResizeObserver', 'getComputedStyle', 'requestAnimationFrame', 'cancelAnimationFrame', 'window', 'document']) delete globalThis[key];
  }
});

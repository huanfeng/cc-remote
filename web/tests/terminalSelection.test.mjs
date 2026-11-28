import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

test('project and session terminals copy completed selections only when enabled and preserve manual copy', async () => {
  class Target {
    listeners = new Map();
    addEventListener(type, fn) { const list = this.listeners.get(type) ?? []; list.push(fn); this.listeners.set(type, list); }
    removeEventListener(type, fn) { this.listeners.set(type, (this.listeners.get(type) ?? []).filter(x => x !== fn)); }
    fire(type, extra = {}) {
      const event = { type, ...(type.startsWith('touch') ? {} : { button: 0 }), preventDefault() { this.defaultPrevented = true; }, stopImmediatePropagation() { this.stopped = true; }, ...extra };
      for (const fn of this.listeners.get(type) ?? []) { fn(event); if (event.stopped) break; }
      return event;
    }
    toggleAttribute() {}
  }
  class Terminal {
    options = {}; unicode = {}; modes = {}; selection = ''; buffer = { active: { viewportY: 0, baseY: 0 } };
    constructor() { globalThis.__selectionTerm = this; }
    loadAddon() {} registerLinkProvider() {} open() {} focus() {} reset() {} dispose() {}
    onData() { return { dispose() {} }; }
    onBinary() { return { dispose() {} }; }
    selectionListeners = new Set();
    onSelectionChange(fn) {
      this.selectionListeners.add(fn);
      return { dispose: () => this.selectionListeners.delete(fn) };
    }
    attachCustomKeyEventHandler(fn) { this.key = fn; }
    hasSelection() { return !!this.selection; }
    getSelection() { return this.selection; }
    clearSelection() { this.select(''); }
    select(value) { this.selection = value; for (const fn of this.selectionListeners) fn(); }
  }
  const copied = [], patches = [];
  let failCopy = false, enabled = true;
  globalThis.__selectionTerminalClass = Terminal;
  globalThis.__selectionCopy = async text => { if (failCopy) throw new Error('copy denied'); copied.push(text); };
  globalThis.MutationObserver = globalThis.ResizeObserver = class { observe() {} disconnect() {} };
  globalThis.getComputedStyle = () => ({ getPropertyValue: () => '' });
  globalThis.requestAnimationFrame = () => 1;
  globalThis.cancelAnimationFrame = () => {};
  globalThis.window = Object.assign(new Target(), { setTimeout });
  globalThis.document = Object.assign(new Target(), { hidden: false, documentElement: {} });
  const data = text => 'data:text/javascript,' + encodeURIComponent(text);
  const mocks = {
    '@xterm/xterm': data('export const Terminal=globalThis.__selectionTerminalClass;'),
    '@xterm/addon-fit': data('export class FitAddon { proposeDimensions() { return null; } }'),
    '@xterm/addon-unicode11': data('export class Unicode11Addon {}'),
    '@xterm/addon-web-links': data('export class WebLinksAddon {}'),
    '@/domain/terminalPaths': data('export const filePathLinks=()=>({});'),
    '@/domain/clipboard': data('export const copyText=text=>globalThis.__selectionCopy(text);'),
    '@/platform/desktop': data('export const desktopTerminalLinks=undefined;'),
    '@/terminal/terminalTheme': data('export const mergeTerminalTheme=()=>({});'),
    '@/lib/i18n': data('export const t=en=>en;'),
  };
  const hook = registerHooks({ resolve(specifier, context, next) {
    if (mocks[specifier]) return { url: mocks[specifier], shortCircuit: true };
    const path = {
      './terminalSize': 'terminal/terminalSize',
      '@/features/terminal/terminalSize': 'terminal/terminalSize',
      '@/features/terminal/terminalSelection': 'terminal/terminalSelection',
      './projectShellOutput': 'projectShell/projectShellOutput',
      './findProjectShellMatch': 'projectShell/findProjectShellMatch',
    }[specifier];
    if (path) return { url: new URL(`../src/features/${path}.ts`, import.meta.url).href, shortCircuit: true };
    return next(specifier, context);
  } });
  let engine;
  const flush = () => new Promise(resolve => setImmediate(resolve));
  try {
    const { createTerminalEngine } = await import('../src/features/terminal/terminalEngine.ts');
    const screen = new Target();
    engine = createTerminalEngine({ backend: { openTerminal: () => ({ close() {} }) }, sessionId: 's', mobile: false,
      box: { clientWidth: 0, clientHeight: 0 }, screen, zoom: () => 1, fitDevice: () => false,
      copyOnSelect: () => enabled, openFile() {}, onUi: patch => patches.push(patch) });
    const term = globalThis.__selectionTerm;
    screen.fire('mousedown'); term.select('partial'); term.select('whole selection\n第二行');
    await flush(); assert.deepEqual(copied, [], 'dragging must not copy intermediate text');
    window.fire('mouseup'); await flush();
    assert.deepEqual(copied, ['whole selection\n第二行']);
    assert.equal(term.hasSelection(), true, 'automatic copy keeps selection');
    assert.equal(screen.fire('mousedown', { button: 2 }).stopped, true, 'right-click must not reach Claude mouse reporting');
    const context = screen.fire('contextmenu', { button: 2 }); await flush();
    assert.equal(context.defaultPrevented, true); assert.equal(context.stopped, true);
    assert.equal(term.getSelection(), 'whole selection\n第二行');
    assert.equal(copied.at(-1), 'whole selection\n第二行');
    const copies = copied.length;
    window.fire('mouseup'); await flush();
    assert.equal(copied.length, copies, 'unrelated mouseup does not copy again');
    screen.fire('touchstart'); term.select('mobile selection');
    window.fire('touchend'); await flush();
    assert.equal(copied.at(-1), 'mobile selection');
    const shift = { type: 'keydown', key: 'Shift' };
    term.key(shift); term.select('keyboard selection'); term.key({ ...shift, type: 'keyup' });
    await flush(); assert.equal(copied.at(-1), 'keyboard selection');
    const beforeShiftDrag = copied.length;
    term.key(shift); screen.fire('mousedown'); term.select('shift drag'); window.fire('mouseup');
    term.key({ ...shift, type: 'keyup' });
    assert.deepEqual(copied.slice(beforeShiftDrag), ['shift drag']);
    enabled = false;
    screen.fire('mousedown'); term.select('manual only'); window.fire('mouseup');
    assert.equal(copied.at(-1), 'shift drag');
    const keyboard = { type: 'keydown', ctrlKey: true, key: 'c', preventDefault() { this.defaultPrevented = true; } };
    assert.equal(term.key(keyboard), false); await flush();
    assert.equal(copied.at(-1), 'manual only');
    assert.equal(term.hasSelection(), true);
    failCopy = true;
    engine.copySelection(); await flush();
    assert.equal(term.hasSelection(), true, 'failed copy must keep recoverable text');
    assert.ok(patches.some(p => p.copyError), 'clipboard failure is visible');
    term.clearSelection();
    assert.equal(screen.fire('mousedown', { button: 2 }).stopped, undefined, 'without a selection preserve terminal right-click');
    assert.equal(screen.fire('contextmenu', { button: 2 }).stopped, undefined);
    engine.dispose(); engine = undefined;
    term.select('after disposal'); window.fire('mouseup'); await flush();
    assert.equal(copied.includes('after disposal'), false);

    failCopy = false;
    const { createProjectShellTerminal } = await import('../src/features/projectShell/projectShellTerminal.ts');
    const element = new Target();
    engine = createProjectShellTerminal({
      backend: { openProjectShell: () => ({ close() {} }) }, shellId: 'shell', runId: 'run', element, mobile: false,
      copyOnSelect: () => enabled, onCopyError() {}, onState() {},
    });
    const projectTerm = globalThis.__selectionTerm;
    element.fire('mousedown'); projectTerm.select('project disabled'); window.fire('mouseup');
    assert.equal(copied.at(-1), 'manual only');
    enabled = true;
    projectTerm.select('programmatic selection'); window.fire('mouseup');
    assert.equal(copied.at(-1), 'manual only', 'search highlights do not auto-copy');
    element.fire('mousedown'); projectTerm.select('project final');
    window.fire('mouseup', { button: 2 });
    assert.equal(copied.at(-1), 'manual only');
    window.fire('mouseup');
    assert.equal(copied.at(-1), 'project final');
    assert.equal(engine.selection(), 'project final');
  } finally {
    engine?.dispose(); hook.deregister();
    for (const key of ['__selectionTerminalClass', '__selectionTerm', '__selectionCopy', 'MutationObserver', 'ResizeObserver', 'getComputedStyle', 'requestAnimationFrame', 'cancelAnimationFrame', 'window', 'document']) delete globalThis[key];
  }
});

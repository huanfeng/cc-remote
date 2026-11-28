import { test } from 'node:test';
import assert from 'node:assert/strict';
import { defaultProjectShellBindings, matchesProjectShellShortcut, projectShellActionForKey, projectShellBindingLabel, projectShellShortcutLabel } from '../src/features/projectShell/projectShellKeys.ts';
import { projectShellCounts } from '../src/features/projectShell/projectShellStatus.ts';

test('Web falls back from a saved Alt+F shortcut while desktop still accepts it', () => {
  const altF = { key: 'f', code: 'KeyF', altKey: true, ctrlKey: false };
  const ctrlBackquote = { key: 'Dead', code: 'Backquote', altKey: false, ctrlKey: true };
  assert.equal(projectShellShortcutLabel('alt-f', false), 'Ctrl+`');
  assert.equal(matchesProjectShellShortcut(altF, 'alt-f', false), false);
  assert.equal(matchesProjectShellShortcut(ctrlBackquote, 'alt-f', false), true);
  assert.equal(matchesProjectShellShortcut(ctrlBackquote, 'default', false), true);
  assert.equal(projectShellShortcutLabel('alt-f', true), 'Alt+F');
  assert.equal(matchesProjectShellShortcut(altF, 'alt-f', true), true);
  assert.equal(matchesProjectShellShortcut(ctrlBackquote, 'disabled', false), false);
});

test('the project badge counts open panes by project and distinguishes running PTYs from saved panes', () => {
  const project = { cwd: 'D:/Work/Repo', projectKey: 'project', state: 'running' };
  const shells = [
    { ...project, id: 'a', runId: 'run-a', aliases: ['D:\\Alias'], tabId: 'a' },
    { ...project, id: 'b', runId: 'run-b', tabId: 'a' },
    { ...project, id: 'c', state: 'interrupted' },
    { ...project, id: 'd', state: 'exited' },
    { ...project, id: 'e', state: 'stopped' },
    { ...project, id: 'f' },
    { ...project, id: 'g', cwd: 'D:/Work/Other', projectKey: 'other', runId: 'run-g' },
  ];
  assert.deepEqual(projectShellCounts(shells, 'd:/alias/'), { total: 3, running: 2 });
  assert.deepEqual(projectShellCounts(shells, 'D:/Work/Repo'), { total: 3, running: 2 });
  assert.deepEqual(projectShellCounts(shells, 'D:/Work/Other'), { total: 1, running: 1 });
  assert.deepEqual(projectShellCounts(shells, ''), { total: 0, running: 0 });
});

test('the terminal shortcuts match only their chord and allow rebinding or disabling an action', () => {
  const key = (value, modifiers) => ({
    key: value, ctrlKey: true, shiftKey: true, altKey: false, metaKey: false,
    getModifierState: () => false, ...modifiers,
  });
  const match = event => projectShellActionForKey(event, defaultProjectShellBindings);
  assert.equal(match(key('Enter')), 'newTab');
  assert.equal(match(key('ArrowRight')), 'splitColumns');
  assert.equal(match(key('ArrowDown')), 'splitRows');
  assert.equal(match(key('Enter', { shiftKey: false })), undefined);
  assert.equal(match(key('Enter', { defaultPrevented: true })), undefined);
  assert.equal(match(key('Enter', { isComposing: true })), undefined);
  assert.equal(match(key('Enter', { getModifierState: name => name === 'AltGraph' })), undefined);
  const bindings = { ...defaultProjectShellBindings, newTab: { key: 't', meta: true }, splitRows: null };
  assert.equal(projectShellActionForKey(key('Enter'), bindings), undefined);
  assert.equal(projectShellActionForKey(key('ArrowDown'), bindings), undefined);
  assert.equal(projectShellActionForKey(key('T', { ctrlKey: false, shiftKey: false, metaKey: true }), bindings), 'newTab');
  assert.equal(projectShellBindingLabel(defaultProjectShellBindings.splitColumns), 'Ctrl+Shift+→');
  assert.equal(projectShellBindingLabel(bindings.newTab, true), 'Meta+t');
});

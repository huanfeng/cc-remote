import { test } from 'node:test';
import assert from 'node:assert/strict';
import { emptyWorkspace, addWorkspaceTab, closeWorkspaceTab, moveWorkspaceTab, savedWorkspace, restoreWorkspace } from '../src/domain/desktopWorkspace.ts';

const tab = (id) => ({ label:`tab-main-${id}`, hostId:id, hash:`#/s/${id}`, title:id, canBack:false, canForward:false, waiting:0 });
test('server tabs activate an existing context and close to their nearest neighbor', () => {
  let state = ['a','b','c'].reduce((s,id)=>addWorkspaceTab(s,tab(id)), emptyWorkspace());
  state = addWorkspaceTab(state,tab('b'));
  assert.equal(state.tabs.length,3);
  assert.equal(state.active,'tab-main-b');
  state = closeWorkspaceTab(state,'tab-main-b');
  assert.equal(state.active,'tab-main-c');
  assert.deepEqual(state.closed,[{hostId:'b',hash:'#/s/b'}]);
  state = closeWorkspaceTab(state,'tab-main-a');
  assert.equal(state.active,'tab-main-c');
  state = closeWorkspaceTab(state,'tab-main-c');
  assert.equal(state.active,null);
  assert.deepEqual(state.tabs,[]);
});
test('reordering preserves the active tab and restore excludes removed or duplicate hosts', () => {
  let state = ['a','b','c'].reduce((s,id)=>addWorkspaceTab(s,tab(id)), emptyWorkspace());
  state = moveWorkspaceTab(state,'tab-main-c','tab-main-a');
  assert.deepEqual(state.tabs.map(x=>x.hostId),['c','a','b']);
  assert.equal(state.active,'tab-main-c');
  assert.deepEqual(restoreWorkspace(savedWorkspace(state),new Set(['a','c'])),{tabs:[{hostId:'c',hash:'#/s/c'},{hostId:'a',hash:'#/s/a'}],activeHost:'c'});
  assert.deepEqual(restoreWorkspace('{broken',new Set()),{tabs:[]});
  assert.deepEqual(restoreWorkspace(JSON.stringify({tabs:[{hostId:'a',hash:'https://evil.test'},{hostId:'a',hash:'#/s/other'},null]}),new Set(['a'])),{tabs:[{hostId:'a',hash:''}],activeHost:undefined});
});

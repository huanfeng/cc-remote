import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decodePreferences, mergePreferencePatch } from '../src/domain/preferences.ts';

const defaults = {
  theme:'system', palette:'graphite', terminalPalette:'dark', sendKey:'enter', sidebarCollapsed:false, sidebarWidth:272,
  collapsedProjects:{}, toolsExpanded:false, terminalView:{}, terminalZoom:1, terminalFit:false,
  newSessionMode:'terminal', hiddenRecaps:[], notify:{on:false,waiting:true,done:true,failed:true},
};

test('shared settings sync without changing each active view sidebar layout', () => {
  const local = {...defaults, sidebarWidth:400, sidebarCollapsed:true};
  const latest = {...defaults, theme:'dark', palette:'default', sidebarWidth:240};
  const synced = mergePreferencePatch(local, latest, {});
  assert.equal(synced.theme, 'dark'); assert.equal(synced.palette, 'default');
  assert.equal(synced.sidebarWidth, 400); assert.equal(synced.sidebarCollapsed, true);
  const layoutEdit = mergePreferencePatch(local, latest, {sidebarWidth:390});
  assert.equal(layoutEdit.theme, 'dark'); assert.equal(layoutEdit.sidebarWidth, 390);
});

test('stale map and notification patches preserve other views edits and apply local deletions', () => {
  const local = {...defaults, collapsedProjects:{'host-a/one':true}, terminalView:{a:true}};
  const latest = {...local, theme:'dark', collapsedProjects:{...local.collapsedProjects,'host-b/two':true}, terminalView:{a:true,b:true}, notify:{...defaults.notify,done:false}};
  const merged = mergePreferencePatch(local, latest, {
    collapsedProjects:{}, terminalView:{a:true,c:true}, notify:{...defaults.notify,on:true},
  });
  assert.deepEqual(merged.collapsedProjects, {'host-b/two':true});
  assert.deepEqual(merged.terminalView, {a:true,b:true,c:true});
  assert.deepEqual(merged.notify, {...defaults.notify,on:true,done:false});
  assert.equal(merged.theme, 'dark');
});

test('dismissed recaps merge; existing envelopes migrate and corrupted settings fall back', () => {
  const merged = mergePreferencePatch(defaults, {...defaults,hiddenRecaps:['other:1']}, {hiddenRecaps:['local:1']});
  assert.deepEqual(merged.hiddenRecaps, ['other:1','local:1']);
  assert.equal(decodePreferences('{broken', defaults), undefined);
  const migrated = decodePreferences(JSON.stringify({version:1,state:{theme:'dark',hiddenRecaps:['old:1']}}), defaults);
  assert.equal(migrated.theme,'dark'); assert.deepEqual(migrated.hiddenRecaps,[]);
  assert.equal(migrated.terminalPalette,'dark');
  const invalid = decodePreferences(JSON.stringify({version:2,state:{theme:'bad',terminalPalette:'bad',sidebarWidth:'huge',terminalView:null,notify:{on:true}}}), defaults);
  assert.equal(invalid.theme, 'system'); assert.equal(invalid.sidebarWidth,272);
  assert.equal(invalid.terminalPalette,'dark');
  assert.deepEqual(invalid.terminalView,{}); assert.deepEqual(invalid.notify,{...defaults.notify,on:true});
});

test('two stores synchronize without echo writes and stale layout edits cannot revert the theme', async () => {
  const original = {window:globalThis.window,localStorage:globalThis.localStorage};
  const values = new Map(), listeners = [];
  let writes = 0;
  try {
    globalThis.window = {addEventListener(type, listener) { if (type === 'storage') listeners.push(listener); }};
    globalThis.localStorage = {
      getItem: key => values.get(key) ?? null,
      setItem(key,value) { writes++; values.set(key,value); },
    };
    const {usePrefs:a} = await import('../src/lib/prefs.ts?view=a');
    const {usePrefs:b} = await import('../src/lib/prefs.ts?view=b');
    a.getState().set({theme:'dark',terminalPalette:'ocean',sidebarWidth:400});
    // B has not received the storage notification yet. Its layout write must
    // still preserve A's latest theme and its own independent layout.
    b.getState().set({sidebarWidth:300});
    assert.equal(b.getState().theme,'dark');
    assert.equal(b.getState().terminalPalette,'ocean');
    const count = writes;
    for (const listener of listeners) listener({key:'ccn-prefs'});
    assert.equal(writes,count);
    assert.equal(a.getState().sidebarWidth,400);
    assert.equal(b.getState().sidebarWidth,300);
    b.getState().set({palette:'default'});
    for (const listener of listeners) listener({key:'ccn-prefs'});
    assert.equal(a.getState().palette,'default');
    assert.equal(a.getState().sidebarWidth,400);
  } finally {
    for (const [key,value] of Object.entries(original)) {
      if (value === undefined) delete globalThis[key]; else globalThis[key]=value;
    }
  }
});

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

test('native tabs survive overlapping opens, wait for listeners, preserve focus and expose recovery', async () => {
  const values = new Map();
  const handlers = new Map(), storageHandlers = new Set(), native = new Set(['tab-main-stale']);
  const calls = [], events = [];
  let releaseFirst, failHost = '';
  const firstCreation = new Promise(resolve => { releaseFirst = resolve; });
  const updateListeners = new Set();
  let update = { aboutOpen:false };
  const hostList = { current:'a', hosts:[{id:'a'},{id:'b'},{id:'c'}] };
  const h = {
    invoke:async (command,args) => {
      calls.push([command,args]);
      if (command === 'desktop_tab_open') {
        if (args.hostId === failHost) throw new Error('test creation failure');
        if (args.hostId === 'a') await firstCreation;
        native.add(args.label);
      } else if (command === 'desktop_tab_close') native.delete(args.label);
      else if (command === 'desktop_tab_activate' && args.label && !native.has(args.label)) throw new Error('missing native view');
    },
    views:async()=>[...native].map(label=>({label,window:{label:'main'}})),
    listen:async (name,callback)=> { handlers.set(name,callback); return ()=>handlers.delete(name); },
    emit:async (target,event,payload)=> { events.push({target,event,payload}); },
    hosts:()=>hostList,
    update:{ getState:()=>update, setState:patch=> {update={...update,...patch};for(const callback of updateListeners)callback(update);}, subscribe:callback=>{updateListeners.add(callback);return()=>updateListeners.delete(callback);} },
  };
  globalThis.__desktopWorkspaceTest = h;
  globalThis.location = new URL('http://tauri.localhost/');
  globalThis.localStorage = { getItem:key=>values.get(key)??null, setItem:(key,value)=>values.set(key,value) };
  globalThis.window = { addEventListener:(_,fn)=>storageHandlers.add(fn), removeEventListener:(_,fn)=>storageHandlers.delete(fn) };
  const data = source=>'data:text/javascript,'+encodeURIComponent(source);
  const prelude = 'const h=globalThis.__desktopWorkspaceTest;';
  const mocks = {
    '@tauri-apps/api/core':data(prelude+'export const invoke=h.invoke;'),
    '@tauri-apps/api/window':data('export const getCurrentWindow=()=>({label:"main"});'),
    '@tauri-apps/api/webview':data(prelude+'export const getAllWebviews=h.views;'),
    '@tauri-apps/api/event':data(prelude+'export const listen=h.listen,emitTo=h.emit;'),
    './desktop':data('export const DESKTOP=true,DESKTOP_TAB=null;'),
    '@/lib/hosts':data(prelude+'export const loadHosts=h.hosts;'),
    './desktopUpdate':data(prelude+'export const useDesktopUpdate=h.update;export const openDesktopAbout=()=>h.update.setState({aboutOpen:true});'),
    './desktopTabs':data('export const DESKTOP_OPEN_HOST="desktop-open-host",DESKTOP_TAB_STATE="desktop-tab-state",DESKTOP_TAB_COMMAND="desktop-tab-command",DESKTOP_SHORTCUT="desktop-shortcut",DESKTOP_ABOUT="desktop-open-about";'),
  };
  const hook = registerHooks({ resolve(specifier,context,next) {
    if (mocks[specifier]) return {url:mocks[specifier],shortCircuit:true};
    if (specifier === '@/domain/desktopWorkspace') return {url:new URL('../src/domain/desktopWorkspace.ts',import.meta.url).href,shortCircuit:true};
    return next(specifier,context);
  }});
  const settle = async()=> {for(let i=0;i<6;i++)await new Promise(resolve=>setImmediate(resolve));};
  let stop;
  try {
    const workspace = await import('../src/platform/desktopWorkspace.ts');
    stop = workspace.subscribeDesktopWorkspace();
    await settle();
    assert(calls.some(([name,args])=>name==='desktop_tab_close'&&args.label==='tab-main-stale'));
    workspace.openDesktopHost({hostId:'b',hash:'#/s/b',reload:false});
    releaseFirst();
    await settle();
    let state=workspace.useDesktopWorkspace.getState();
    assert.equal(state.error,'');
    assert.equal(state.tabs.length,2);
    const b=state.tabs.find(tab=>tab.hostId==='b');
    assert.equal(state.active,b.label);
    assert.equal(calls.filter(([name])=>name==='desktop_tab_activate').at(-1)[1].label,b.label);

    await workspace.sendDesktopTabCommand({action:'settings'});
    assert.equal(events.filter(e=>e.payload?.action==='settings').length,0,'commands wait for the child JS listener');
    const activations=calls.filter(([name])=>name==='desktop_tab_activate').length;
    handlers.get('desktop-tab-state')({payload:{...b,title:'B · CC Remote',phase:'ready',error:'',waiting:2,done:3}});
    await settle();
    assert.equal(events.filter(e=>e.payload?.action==='settings').length,1);
    assert.equal(calls.filter(([name])=>name==='desktop_tab_activate').length,activations,'poll metadata never steals input focus');
    assert.equal(workspace.useDesktopWorkspace.getState().tabs.find(tab=>tab.label===b.label).done,3,'inactive host completion metadata is retained');

    h.update.setState({aboutOpen:true});await settle();
    assert.equal(calls.filter(([name])=>name==='desktop_tab_activate').at(-1)[1].label,null);
    h.update.setState({aboutOpen:false});await settle();
    assert.equal(calls.filter(([name])=>name==='desktop_tab_activate').at(-1)[1].label,b.label);

    failHost='c';workspace.openDesktopHost({hostId:'c',hash:'',reload:false});await settle();
    assert.match(workspace.useDesktopWorkspace.getState().error,/test creation failure/);
    assert.equal(calls.filter(([name])=>name==='desktop_tab_activate').at(-1)[1].label,null,'native content is hidden for the recovery panel');
    failHost='';workspace.useDesktopWorkspace.setState({error:''});workspace.reconcileDesktopTabs();await settle();
    assert.equal(workspace.useDesktopWorkspace.getState().error,'');

    hostList.hosts=hostList.hosts.filter(host=>host.id!=='b');
    for(const callback of storageHandlers)callback();await settle();
    assert(!native.has(b.label),'removing a saved host disposes its tab');
    const persisted=JSON.parse(values.get('cc-desktop-tabs:v1'));
    assert(persisted.tabs.every(tab=>Object.keys(tab).sort().join(',')==='hash,hostId'));
  } finally {
    stop?.();hook.deregister();
    delete globalThis.__desktopWorkspaceTest;delete globalThis.window;delete globalThis.location;delete globalThis.localStorage;
  }
});

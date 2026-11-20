import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

test('tab route traversal keeps forward history despite synchronous store subscribers', async () => {
  const events=[],listeners=new Map(),appListeners=new Set(),completionListeners=new Set(),connectionListeners=new Set(),storage=new Map();
  let command, appState={sessions:[],sessionsLoaded:true}, records={}, phase='ready';
  const url=new URL('http://tauri.localhost/index.html?desktop-tab=tab-main-a&host=a#/s/a');
  const h={
    emit:async(target,event,payload)=>events.push({target,event,payload}),
    listen:async(_,callback)=>{command=callback;return()=>{command=undefined;};},
    app:{getState:()=>({...appState,open:surface=>{appState={...appState,surface};for(const callback of appListeners)callback();}}),subscribe:callback=>{appListeners.add(callback);return()=>appListeners.delete(callback);}},
    connection:{getState:()=>({phase,error:'',offline:''}),subscribe:callback=>{connectionListeners.add(callback);return()=>connectionListeners.delete(callback);}},
    completion:{getState:()=>({records}),subscribe:callback=>{completionListeners.add(callback);return()=>completionListeners.delete(callback);}},
  };
  globalThis.__desktopNavigationTest=h;
  globalThis.location={get hash(){return url.hash;},set hash(value){url.hash=value;},get pathname(){return url.pathname;},get search(){return url.search;},replace:value=>{url.href=new URL(value,url).href;},reload(){}};
  globalThis.window={addEventListener:(event,callback)=>listeners.set(event,callback),removeEventListener:event=>listeners.delete(event)};
  window.history={state:null,pushState(state){this.state=state;},back(){this.state=null;listeners.get('popstate')?.({state:null});}};
  globalThis.document={head:{},title:'A · CC Remote'};
  globalThis.MutationObserver=class {observe(){}disconnect(){}};
  globalThis.sessionStorage={getItem:key=>storage.get(key)??null,setItem:(key,value)=>storage.set(key,value)};
  const data=source=>'data:text/javascript,'+encodeURIComponent(source);
  const prelude='const h=globalThis.__desktopNavigationTest;';
  const mocks={
    '@tauri-apps/api/event':data(prelude+'export const emitTo=h.emit,listen=h.listen;'),
    '@tauri-apps/api/window':data('export const getCurrentWindow=()=>({label:"main"});'),
    './desktop':data('export const DESKTOP=true,DESKTOP_TAB="tab-main-a";'),
    '@/store/app':data(prelude+'export const useApp=h.app;'),
    '@/features/auth/connection':data(prelude+'export const useConnection=h.connection;'),
    '@/store/completionRead':data(prelude+'export const useCompletionRead=h.completion;'),
    '@/lib/hosts':data('export const hostKey=id=>"a:"+id;'),
  };
  const hook=registerHooks({resolve(specifier,context,next){
    if(mocks[specifier])return{url:mocks[specifier],shortCircuit:true};
    if(specifier.startsWith('@/domain/'))return{url:new URL('../src/domain/'+specifier.slice('@/domain/'.length)+'.ts',import.meta.url).href,shortCircuit:true};
    if(specifier === '@/lib/overlayHistory')return{url:new URL('../src/lib/overlayHistory.ts',import.meta.url).href,shortCircuit:true};
    return next(specifier,context);
  }});
  let stop;
  try {
    const {installDesktopTabNavigation}=await import('../src/platform/desktopTabs.ts');
    stop=installDesktopTabNavigation();await new Promise(resolve=>setImmediate(resolve));
    appState={sessionsLoaded:true,sessions:[{id:'waiting',status:'waiting'},{id:'archived',status:'waiting',archived:true},{id:'done',status:'completed',completedAt:'2026-11-20T00:00:00Z'}]};
    records={'a:done':{completedAt:'2026-11-20T00:00:00Z',viewedAt:''}};
    for(const callback of appListeners)callback();
    assert.equal(events.at(-1).payload.waiting,1);
    assert.equal(events.at(-1).payload.done,1);
    phase='signed-out';
    for(const callback of connectionListeners)callback();
    assert.equal(events.at(-1).payload.waiting,0,'sign-out clears raw per-tab badges even while old sessions remain');
    assert.equal(events.at(-1).payload.done,0);
    phase='ready';
    for(const callback of connectionListeners)callback();
    records={'a:done':{completedAt:'2026-11-20T00:00:00Z',viewedAt:'2026-11-20T00:00:00Z'}};
    for(const callback of completionListeners)callback();
    assert.equal(events.at(-1).payload.done,0,'viewing completion reports clear without waiting for another server poll');
    const {overlayHistory}=await import('../src/lib/overlayHistory.ts');
    let remove,closed=0;
    remove=overlayHistory().add(()=>{closed++;remove();});
    assert.equal(events.at(-1).payload.canBack,true,'dialog enables titlebar Back even with empty route journal');
    const key={key:'w',ctrlKey:true,preventDefault(){this.prevented=true;},stopImmediatePropagation(){this.stopped=true;}};
    const before=events.length;
    listeners.get('keydown')(key);
    assert.equal(closed,1);assert.equal(key.prevented,true);assert.equal(key.stopped,true);
    assert.ok(events.slice(before).every(event=>event.payload.action!=='close'),'dialog Ctrl+W must not emit native tab-close');
    listeners.get('keydown')(key);
    assert.equal(events.at(-1).payload.action,'close','without dialog Ctrl+W still closes native tab');
    remove=overlayHistory().add(()=>{closed++;remove();});
    command({payload:{action:'back'}});
    assert.equal(closed,2);assert.equal(location.hash,'#/s/a','native Back dismisses dialog before route journal');
    location.hash='#/s/b';listeners.get('hashchange')();
    location.hash='#/s/c';listeners.get('hashchange')();
    command({payload:{action:'back'}});
    assert.equal(location.hash,'#/s/b');
    assert.equal(events.at(-1).payload.canForward,true);
    assert.equal(events.at(-1).payload.canBack,true);
    listeners.get('hashchange')();
    command({payload:{action:'forward'}});
    assert.equal(location.hash,'#/s/c');
    assert.equal(events.at(-1).payload.canForward,false);
    command({payload:{action:'back'}});
    command({payload:{action:'navigate',hash:'#/s/d'}});listeners.get('hashchange')();
    assert.equal(events.at(-1).payload.canForward,false,'a new route replaces the old forward branch');
    command({payload:{action:'back'}});
    assert.equal(location.hash,'#/s/b');
    assert(events.every(event=>event.target.kind==='Webview'&&event.target.label==='main'));
  } finally {
    stop?.();hook.deregister();
    for(const key of ['__desktopNavigationTest','location','window','document','MutationObserver','sessionStorage'])delete globalThis[key];
  }
});

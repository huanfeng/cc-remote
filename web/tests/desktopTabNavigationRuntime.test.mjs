import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

test('tab route traversal keeps forward history despite synchronous store subscribers', async () => {
  const events=[],listeners=new Map(),appListeners=new Set(),storage=new Map();
  let command, appState={sessions:[]};
  const url=new URL('http://tauri.localhost/index.html?desktop-tab=tab-main-a&host=a#/s/a');
  const h={
    emit:async(target,event,payload)=>events.push({target,event,payload}),
    listen:async(_,callback)=>{command=callback;return()=>{command=undefined;};},
    app:{getState:()=>({...appState,open:surface=>{appState={...appState,surface};for(const callback of appListeners)callback();}}),subscribe:callback=>{appListeners.add(callback);return()=>appListeners.delete(callback);}},
    connection:{getState:()=>({phase:'ready',error:'',offline:''}),subscribe:()=>()=>{}},
  };
  globalThis.__desktopNavigationTest=h;
  globalThis.location={get hash(){return url.hash;},set hash(value){url.hash=value;},get pathname(){return url.pathname;},get search(){return url.search;},replace:value=>{url.href=new URL(value,url).href;},reload(){}};
  globalThis.window={addEventListener:(event,callback)=>listeners.set(event,callback),removeEventListener:event=>listeners.delete(event)};
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
  };
  const hook=registerHooks({resolve(specifier,context,next){
    if(mocks[specifier])return{url:mocks[specifier],shortCircuit:true};
    if(specifier.startsWith('@/domain/'))return{url:new URL('../src/domain/'+specifier.slice('@/domain/'.length)+'.ts',import.meta.url).href,shortCircuit:true};
    return next(specifier,context);
  }});
  let stop;
  try {
    const {installDesktopTabNavigation}=await import('../src/platform/desktopTabs.ts');
    stop=installDesktopTabNavigation();await new Promise(resolve=>setImmediate(resolve));
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

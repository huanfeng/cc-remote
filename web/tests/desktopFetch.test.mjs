import { test } from 'node:test';
import assert from 'node:assert/strict';

test('desktop HTTP forwarding leaves Tauri IPC on the native fetch path', async () => {
  const native = [], commands = [];
  const original = {window:globalThis.window,document:globalThis.document,location:globalThis.location,isTauri:globalThis.isTauri};
  const encode = new TextEncoder();
  const metadata = encode.encode(JSON.stringify({status:200,statusText:'OK',headers:[['content-type','application/json']]}));
  const body = encode.encode('{"accounts":[]}');
  const packet = new Uint8Array(4+metadata.length+body.length);
  new DataView(packet.buffer).setUint32(0,metadata.length);packet.set(metadata,4);packet.set(body,4+metadata.length);
  try {
    globalThis.isTauri = true;
    globalThis.location = new URL('http://tauri.localhost/');
    globalThis.document = {documentElement:{classList:{add(){}}},addEventListener(){}};
    globalThis.window = {
      fetch:async input => {native.push(String(input));return new Response('{}');},
      __TAURI_INTERNALS__:{metadata:{currentWindow:{label:'main'}},invoke:async cmd => {
        commands.push(cmd);
        if(commands.length>2) throw new Error('IPC recursively forwarded to itself');
        // The real Tauri runtime implements invoke through a fetch to this URL.
        await window.fetch('http://ipc.localhost/'+cmd,{method:'POST'});
        return packet.buffer;
      }},
    };
    await import('../src/platform/desktop.ts');
    const response = await window.fetch('http://127.0.0.1:8790/api/usage/monitor');
    assert.deepEqual(await response.json(),{accounts:[]});
    assert.deepEqual(commands,['http_request']);
    assert.deepEqual(native,['http://ipc.localhost/http_request']);
    await window.fetch('http://tauri.localhost/assets/app.js');
    await window.fetch('https://ipc.localhost/plugin:event|listen');
    assert.equal(commands.length,1);
    assert.equal(native.length,3);
  } finally {
    for(const [key,value] of Object.entries(original)){if(value===undefined) delete globalThis[key];else globalThis[key]=value;}
  }
});

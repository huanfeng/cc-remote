import { test } from 'node:test';
import assert from 'node:assert/strict';
import { monitorRuns, windowKey, historyWindows } from '../src/features/status/monitorMath.ts';
import { HttpBackend } from '../src/backend/http/httpBackend.ts';

const window = { name: 'primary', percent: 42, windowMinutes: 300 };
const at = minutes => new Date(Date.UTC(2026,10,12,0,minutes)).toISOString();
const box = {width:600,height:200,padLeft:20,padRight:10,padTop:10,padBottom:20};

test('monitor charts preserve failure and sleep gaps and never fabricate zero readings', () => {
  const history = { hours:24, points:[
    {at:at(0),windows:[window]}, {at:at(10),windows:null,error:'offline'},
    {at:at(20),windows:[window]}, {at:at(80),windows:[window]},
  ]};
  const runs=monitorRuns(history,windowKey(window),Date.parse(at(0)),Date.parse(at(90)),box,10);
  assert.deepEqual(runs.map(r=>r.length),[1,1,1]);
  assert.ok(runs.every(r=>r[0][1]<180));
  assert.equal(historyWindows(history).length,1);
  assert.notEqual(windowKey(window),windowKey({...window,windowMinutes:10080}));
  const changedCadence = {hours:24,points:[0,30,60,70].map((m,i)=>({at:at(m),intervalMinutes:i<3?30:10,windows:[window]}))};
  assert.deepEqual(monitorRuns(changedCadence,windowKey(window),Date.parse(at(0)),Date.parse(at(90)),box,10).map(r=>r.length),[4]);
});

test('monitor HTTP mutations use authentication and history account ids are encoded', async () => {
  const log=[];
  const backend=new HttpBackend({baseUrl:'http://localhost:8790',token:'test-token',lang:'zh',fetch:async(url,init)=>{
    log.push({url,...init}); return new Response(JSON.stringify({intervalMinutes:30,accounts:[],points:[]}),{status:200});
  }});
  await backend.saveUsageMonitor(30);
  await backend.refreshUsageMonitor();
  const id='claude:account/with?reserved&chars';
  await backend.monitorHistory(id,168);
  assert.equal(log[0].method,'PUT');
  assert.deepEqual(JSON.parse(log[0].body),{intervalMinutes:30});
  assert.equal(log[1].method,'POST');
  assert.equal(new URL(log[2].url).searchParams.get('account'),id);
  assert.equal(new URL(log[2].url).searchParams.get('hours'),'168');
  assert.ok(log.every(r=>r.headers.Authorization==='Bearer test-token'));
  backend.dispose();
});

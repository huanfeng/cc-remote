import { test } from 'node:test';
import assert from 'node:assert/strict';
import { groupTranscript, requestForCall, linkOutcomes } from '../src/domain/toolGroups.ts';
import { buildItems, placeRequests, interactionsByCall } from '../src/features/conversation/fold.ts';
import { usageLimitView } from '../src/domain/usageLimit.ts';
const at = '2026-11-17T00:00:00Z';
const input = { title: 'Limit', reset: '5pm', options: ['1','2','3','4'].map(id => ({id,label:'terminal label'})) };
const tool = { seq:1,kind:'tool',createdAt:at,payload:{id:'r1',name:'UsageLimit',input} };
const request = {id:'r1',kind:'question',toolName:'UsageLimit',input,status:'pending',createdAt:at};
test('usage choice has one standalone pending card linked by request identity', () => {
 const {items} = buildItems([tool], {running:false,live:true});
 assert.equal(items[0].type,'special');
 const placement = placeRequests(items,[request]);
 assert.equal(placement.byCall.get(1),request);
 assert.deepEqual(placement.loose,[]);
 assert.equal(requestForCall(items[0].call,[{...request,id:'other'}]),undefined);
});
test('usage history restores all choices and expires without a duplicate pending card', () => {
 for (const option of ['1','2','3','4','']) {
  const outcome = option ? 'terminal' : 'expired';
  const result = {seq:2,kind:'tool_result',createdAt:at,payload:{tool_use_id:'r1',content:JSON.stringify({outcome,option})}};
  const items = groupTranscript([tool,result],false);
  const view = usageLimitView(items[0].call);
  assert.equal(view.selected,option);
  assert.equal(view.outcome,outcome);
  assert.equal(view.expired,!option);
  assert.equal(view.options.length,4);
  assert.equal(view.reset,'5pm');
  assert.deepEqual(placeRequests(items,[request]).loose,[]);
 }
});
test('interaction-only history retains the chosen option', () => {
 const items = groupTranscript([tool],false);
 const event = {seq:2,kind:'interaction',createdAt:at,payload:{id:'r1',toolName:'UsageLimit',input,outcome:'web',answers:{option:'2'}}};
 const interaction = interactionsByCall(items,[event]).get(1);
 assert.equal(usageLimitView(items[0].call,interaction).selected,'2');
 assert.equal(usageLimitView({...items[0].call,resultText:'null'}).selected,'');
});

test('repeated identical menus match interaction IDs even if outcomes arrive late', () => {
 const second = {...tool,seq:3,payload:{...tool.payload,id:'r2'}};
 const items = groupTranscript([tool,second],true);
 const event = (id, seq, outcome) => ({seq,kind:'interaction',createdAt:at,payload:{id,toolName:'UsageLimit',input,outcome,answers:{option:'2'}}});
 const events = [event('r1',4,'web'),event('r2',5,'terminal'),event('unknown',6,'expired')];
 const interactions = interactionsByCall(items,events);
 assert.equal(interactions.get(1)?.outcome,'web');
 assert.equal(interactions.get(3)?.outcome,'terminal');
 assert.deepEqual([...linkOutcomes(items,events)],[[1,'web'],[3,'terminal']]);
 const unknown = [event('unknown',6,'expired')];
 assert.equal(interactionsByCall(items,unknown).size,0);
 assert.equal(linkOutcomes(items,unknown).size,0);
 // A result from a previous identical menu must not hide the current request.
 const result = {seq:2,kind:'tool_result',createdAt:at,payload:{tool_use_id:'r1',content:JSON.stringify({outcome:'web',option:'1'})}};
 const currentItems = groupTranscript([tool,result,second],true);
 const currentRequest = {...request,id:'r2'};
 const placement = placeRequests(currentItems,[currentRequest]);
 assert.equal(placement.byCall.get(3),currentRequest);
 assert.deepEqual(placement.loose,[]);
});

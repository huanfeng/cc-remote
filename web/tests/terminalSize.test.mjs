import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  MAX_ATTEMPTS, OWN_ECHO_MS, PHONE_KEYS, SentSizes, backoffDelay, followFontSize, initialControl, keepsControl,
  pastePayload, stepZoom, throttleLatest, typingTakesControl, usableDims,
} from '../src/features/terminal/terminalSize.ts';

test('backoff doubles from 1 s and caps at 10 s', () => {
  assert.deepEqual([0, 1, 2, 3, 4, 5, 11].map(backoffDelay), [1000, 2000, 4000, 8000, 10000, 10000, 10000]);
  assert.equal(MAX_ATTEMPTS, 12);
});

test('computers control the size; phones only when fitting the device', () => {
  assert.equal(initialControl(false, false), true);
  assert.equal(initialControl(true, false), false);
  assert.equal(initialControl(true, true), true);
});

test('an echo of a size this view sent within 5 s is its own', () => {
  const s = new SentSizes();
  s.record(120, 40, 1000);
  assert.equal(s.isOwn(120, 40, 1000 + OWN_ECHO_MS - 1), true);
  assert.equal(s.isOwn(120, 40, 1000 + OWN_ECHO_MS), false);
  assert.equal(s.isOwn(100, 30, 1500), false);
  // An older size is still recognised after a newer one was sent.
  s.record(130, 40, 1200);
  assert.equal(s.isOwn(120, 40, 1300), true);
});

test('another front end resizing, or a held size, hands control away', () => {
  assert.equal(keepsControl({ held: false, firstSize: true, own: false }), true, 'the size from before connecting');
  assert.equal(keepsControl({ held: false, firstSize: false, own: true }), true, 'our own echo');
  assert.equal(keepsControl({ held: false, firstSize: false, own: false }), false, 'someone else');
  assert.equal(keepsControl({ held: true, firstSize: true, own: true }), false, 'a local ccr holds it');
});

test('typing takes control back on a computer, not on a following phone', () => {
  assert.equal(typingTakesControl({ mobile: false, fitDevice: false, held: false, controlling: false }), true);
  assert.equal(typingTakesControl({ mobile: true, fitDevice: false, held: false, controlling: false }), false);
  assert.equal(typingTakesControl({ mobile: true, fitDevice: true, held: false, controlling: false }), true);
  assert.equal(typingTakesControl({ mobile: false, fitDevice: false, held: true, controlling: false }), false);
  assert.equal(typingTakesControl({ mobile: false, fitDevice: false, held: false, controlling: true }), false);
});

test('fitted dimensions for a hidden or tiny box are ignored', () => {
  assert.equal(usableDims(undefined), false);
  assert.equal(usableDims({ cols: 10, rows: 30 }), false);
  assert.equal(usableDims({ cols: 80, rows: 3 }), false);
  assert.equal(usableDims({ cols: NaN, rows: 30 }), false);
  assert.equal(usableDims({ cols: 80, rows: 24 }), true);
});

test('following fits the whole screen on a computer and the width (zoomed) on a phone', () => {
  const base = { current: 13, width: 500, height: 200, drawnWidth: 1000, drawnHeight: 200 };
  assert.equal(followFontSize({ ...base, mobile: false, zoom: 1 }), 6.5);
  // Height limits a computer, never above 15px.
  assert.equal(followFontSize({ ...base, width: 2000, height: 100, mobile: false, zoom: 1 }), 6.5);
  assert.equal(followFontSize({ ...base, width: 5000, height: 5000, mobile: false, zoom: 1 }), 15);
  // A phone ignores height and applies zoom.
  assert.equal(followFontSize({ ...base, height: 10, mobile: true, zoom: 2 }), 13);
  assert.equal(followFontSize({ ...base, width: 1, mobile: true, zoom: 1 }), 4);
});

test('zoom steps by 0.25 within 0.5–3', () => {
  assert.equal(stepZoom(1, 1), 1.25);
  assert.equal(stepZoom(0.5, -1), 0.5);
  assert.equal(stepZoom(3, 1), 3);
  assert.equal(stepZoom(2.9, 1), 3);
});

test('resizes are throttled and always end on the latest size', () => {
  let now = 0;
  const timers = [];
  const clock = { now: () => now, set: (f, ms) => { const h = { f, at: now + ms }; timers.push(h); return h; }, clear: h => { timers.splice(timers.indexOf(h), 1); } };
  const sent = [];
  const th = throttleLatest(200, v => sent.push(v), clock);
  th.push('a');
  assert.deepEqual(sent, ['a']);
  now = 50; th.push('b');
  now = 100; th.push('c');
  assert.deepEqual(sent, ['a']);
  assert.equal(timers.length, 1);
  now = 200; timers.shift().f();
  assert.deepEqual(sent, ['a', 'c']);
  now = 500; th.push('d');
  assert.deepEqual(sent, ['a', 'c', 'd']);
  now = 550; th.push('e'); th.cancel();
  assert.equal(timers.length, 0);
});

test('phone arrows follow application cursor mode', () => {
  const up = PHONE_KEYS.find(k => k.title === '↑');
  assert.equal(up.data(false), '\x1b[A');
  assert.equal(up.data(true), '\x1bOA');
  assert.equal(PHONE_KEYS.find(k => k.title === 'Shift+Tab').data(false), '\x1b[Z');
});

test('the phone input pastes with bracketed paste when the TUI asked for it', () => {
  assert.equal(pastePayload('a\r\nb', true), '\x1b[200~a\nb\x1b[201~');
  assert.equal(pastePayload('a\rb', false), 'a\nb');
});

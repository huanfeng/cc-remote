import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  barLayout, compactNumber, fullest, ringLevel, countdown, durationText, historyTicks, lineRuns, meterVisible, niceMax, quotaWindows, usageLevel, validWindow,
} from '../src/features/status/usageMath.ts';

const NOW = Date.parse('2026-11-07T12:00:00Z');
const at = ms => new Date(NOW + ms).toISOString();

test('countdown: days, hours and minutes, rounded up; empty when unknown', () => {
  assert.equal(countdown(at(3 * 86_400_000 + 4 * 3600_000), NOW), '3d 4h');
  assert.equal(countdown(at(2 * 3600_000 + 15 * 60_000), NOW), '2h 15m');
  assert.equal(countdown(at(8 * 60_000 + 1), NOW), '9m');
  assert.equal(countdown(at(-60_000), NOW), '0m');
  assert.equal(countdown(undefined, NOW), '');
  assert.equal(countdown('not a date', NOW), '');
});

test('usage level: severity wins, else 75/90 thresholds', () => {
  assert.equal(usageLevel({ percent: 10, severity: 'warning' }), 'warn');
  assert.equal(usageLevel({ percent: 10, severity: 'critical' }), 'high');
  assert.equal(usageLevel({ percent: 74 }), 'ok');
  assert.equal(usageLevel({ percent: 75 }), 'warn');
  assert.equal(usageLevel({ percent: 90 }), 'high');
  assert.equal(validWindow({ percent: 101 }), false);
  assert.equal(validWindow({ percent: NaN }), false);
});

test('quota windows: Claude 5h/7d, Codex labels from windowMinutes', () => {
  assert.deepEqual(quotaWindows(undefined, 'claude').map(w => w.label), ['5h', '7d']);
  const codex = { available: true, usage: { windows: [{ percent: 1, windowMinutes: 300 }, { percent: 2, windowMinutes: 10080 }, { percent: 3, windowMinutes: 2880 }, { percent: 4 }] } };
  assert.deepEqual(quotaWindows(codex, 'codex').map(w => w.label), ['5h', '7d', '2d', 'Primary']);
  assert.deepEqual(quotaWindows({ available: true, usage: {} }, 'codex'), []);
});

test('meter visibility: hidden without a subscription or Codex windows', () => {
  assert.equal(meterVisible(undefined, 'claude'), false);
  assert.equal(meterVisible({ available: false, reason: 'api-key' }, 'claude'), false);
  assert.equal(meterVisible({ available: false, error: 'expired' }, 'claude'), true);
  assert.equal(meterVisible({ available: true, usage: { fiveHour: { percent: 3 } } }, 'codex'), false);
  assert.equal(meterVisible({ available: true, usage: { windows: [] } }, 'codex'), true);
});

test('line runs: scaled into the box, broken at long gaps, clamped', () => {
  const box = { width: 120, height: 60, padLeft: 10, padRight: 10, padTop: 5, padBottom: 5 };
  const start = NOW - 100 * 60_000, end = NOW;
  const runs = lineRuns([
    { at: at(-100 * 60_000), value: 0 },
    { at: at(-90 * 60_000), value: 100 },
    { at: at(-30 * 60_000), value: 150 },
    { at: at(-20 * 60_000) },
    { at: at(0), value: 50 },
  ], start, end, box);
  assert.equal(runs.length, 2);
  assert.deepEqual(runs[0], [[10, 55], [20, 5]]);
  assert.deepEqual(runs[1], [[80, 5], [110, 30]]);
});

test('nice max and bar layout', () => {
  assert.equal(niceMax(0), 1);
  assert.equal(niceMax(52), 100);
  assert.equal(niceMax(22), 25);
  assert.equal(niceMax(4), 5);
  assert.equal(niceMax(1.7), 2);
  const box = { width: 100, height: 50, padLeft: 0, padRight: 0, padTop: 0, padBottom: 0 };
  const { max, bars } = barLayout([0, 5, 10, 20], box, 0.5);
  assert.equal(max, 20);
  assert.deepEqual(bars.map(b => b.height), [0, 12.5, 25, 50]);
  assert.deepEqual(bars.map(b => b.x), [6.3, 31.3, 56.3, 81.3]);
  assert.equal(bars[3].y, 0);
});

test('ticks, durations and compact numbers', () => {
  assert.equal(historyTicks(24, NOW).length, 5);
  assert.equal(historyTicks(168, NOW).length, 8);
  assert.ok(historyTicks(720, NOW).length <= 9);
  assert.equal(durationText(7 * 3600 + 5 * 60), '7h 5m');
  assert.equal(durationText(2 * 86400 + 3600), '2d 1h');
  assert.equal(durationText(42), '42s');
  assert.equal(compactNumber(48_200_000), '48.2M');
  assert.equal(compactNumber(1234), '1.2K');
  assert.equal(compactNumber(999), '999');
});

test('ring level: 80/95 thresholds, host severity can raise it; fullest window', () => {
  assert.equal(ringLevel({ percent: 79 }), 'ok');
  assert.equal(ringLevel({ percent: 80 }), 'warn');
  assert.equal(ringLevel({ percent: 94 }), 'warn');
  assert.equal(ringLevel({ percent: 95 }), 'high');
  assert.equal(ringLevel({ percent: 10, severity: 'warning' }), 'warn');
  assert.equal(ringLevel({ percent: 96, severity: 'warning' }), 'high');
  assert.equal(fullest([{ percent: 37 }, undefined, { percent: 64 }, { percent: 200 }]).percent, 64);
  assert.equal(fullest([undefined]), undefined);
});

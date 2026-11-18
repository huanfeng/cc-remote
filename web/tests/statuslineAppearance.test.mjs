import { test } from 'node:test';
import assert from 'node:assert/strict';
import { defaultStatuslineOptions, colorThemes, statuslineExample, statuslineColor, statuslineSegments } from '../src/features/settings/statuslineAppearance.ts';

test('short labels reduce width while quota names and values remain intact', () => {
  assert.equal(statuslineExample('provider', true), 'P: Anthropic');
  assert.equal(statuslineExample('cost', true), '$0.1200');
  for (const key of ['fiveHour','sevenDay']) assert.equal(statuslineExample(key, true), statuslineExample(key));
  for (const key of statuslineSegments.filter(k => !['fiveHour','sevenDay'].includes(k))) assert.ok(statuslineExample(key, true).length < statuslineExample(key).length);
});
test('default colors distinguish segments and themes do not share mutable options', () => {
  const a = defaultStatuslineOptions();
  assert.ok(new Set(statuslineSegments.map(k => a.colors[k])).size >= 9);
  a.colors.model = '#000000';
  assert.notEqual(defaultStatuslineOptions().colors.model, '#000000');
  for (const theme of colorThemes) for (const key of statuslineSegments) assert.match(theme.colors[key], /^#[0-9a-f]{6}$/i);
});
test('custom segment and legacy normal colors take precedence over defaults', () => {
  assert.equal(statuslineColor('model', { model: '#123456', normal: '#654321' }), '#123456');
  assert.equal(statuslineColor('model', { normal: '#654321' }), '#654321');
  assert.equal(statuslineColor('model', {}), colorThemes[0].colors.model);
});

test('visibility changes retain full order and dragging includes disabled items', async () => {
  const { statuslineOrder, toggleStatusline, moveStatusline } = await import('../src/features/settings/statuslineAppearance.ts');
  const initial = defaultStatuslineOptions();
  const off = toggleStatusline(initial, 'model', false);
  assert.deepEqual(statuslineOrder(off), statuslineOrder(initial));
  assert.deepEqual(toggleStatusline(off, 'model', true).segments, initial.segments);
  const moved = moveStatusline(off, 'duration', 'provider');
  assert.equal(moved.order[0], 'duration');
  assert.equal(moved.segments.includes('duration'), false);
  assert.equal(toggleStatusline(moved, 'duration', true).segments[0], 'duration');
  assert.deepEqual(statuslineOrder(JSON.parse(JSON.stringify(moved))), moved.order);
});

import type { StatuslineOptions } from '../../backend/backend.ts';

export const statuslineSegments = ['provider', 'model', 'effort', 'context', 'input', 'output', 'cost', 'fiveHour', 'sevenDay', 'directory', 'duration'];
export const colorPresets = [
  { en: 'Cyan', zh: '青色', value: '#22d3ee' },
  { en: 'Blue', zh: '蓝色', value: '#60a5fa' },
  { en: 'Violet', zh: '紫色', value: '#a78bfa' },
  { en: 'Pink', zh: '粉色', value: '#f472b6' },
  { en: 'Green', zh: '绿色', value: '#4ade80' },
  { en: 'Teal', zh: '蓝绿', value: '#2dd4bf' },
  { en: 'Amber', zh: '琥珀', value: '#fbbf24' },
  { en: 'Orange', zh: '橙色', value: '#f59e0b' },
  { en: 'Red', zh: '红色', value: '#ef4444' },
  { en: 'Slate', zh: '灰色', value: '#94a3b8' },
  { en: 'White', zh: '白色', value: '#e2e8f0' },
  { en: 'Dark', zh: '深色', value: '#334155' },
];
const palette = (values: string[]): Record<string, string> => ({
  ...Object.fromEntries(statuslineSegments.map((key, i) => [key, values[i]])),
  normal: values[0], warning: '#f59e0b', critical: '#ef4444',
});
export const colorThemes = [
  { id: 'distinct', en: 'Distinct · default', zh: '清晰多彩 · 默认', colors: palette(['#22d3ee','#a78bfa','#f472b6','#4ade80','#38bdf8','#2dd4bf','#fbbf24','#60a5fa','#c084fc','#94a3b8','#cbd5e1']) },
  { id: 'soft', en: 'Soft', zh: '柔和', colors: palette(['#67e8f9','#c4b5fd','#f9a8d4','#86efac','#7dd3fc','#99f6e4','#fde68a','#93c5fd','#d8b4fe','#cbd5e1','#e2e8f0']) },
  { id: 'contrast', en: 'High contrast', zh: '鲜明', colors: palette(['#00d4ff','#c084fc','#ff80c8','#4ade80','#38bdf8','#2dd4bf','#fde047','#60a5fa','#e879f9','#f1f5f9','#ffffff']) },
];
export const defaultStatuslineOptions = (): StatuslineOptions => ({ segments: statuslineSegments.slice(0, 9), colors: { ...colorThemes[0].colors }, shortLabels: false, bars: false, warning: 75, critical: 90, separator: ' | ' });
const examples: Record<string, string> = { provider:'Provider: Anthropic', model:'Model: Opus', effort:'Effort: high', context:'Context: 32%', input:'Input: 64000 tokens', output:'Output: 1200 tokens', cost:'Cost: $0.1200', fiveHour:'5h: 18%', sevenDay:'7d: 24%', directory:'Dir: cc-remote', duration:'Time: 12m' };
const shortLabels: Record<string, string> = { provider:'P', model:'M', effort:'E', context:'C', input:'I', output:'O', directory:'D', duration:'T' };
export function statuslineExample(key: string, abbreviated = false): string {
  const text = examples[key] ?? '';
  if (!abbreviated) return text;
  if (key === 'cost') return text.replace('Cost: ', '');
  return shortLabels[key] ? text.replace(/^[^:]+:/, `${shortLabels[key]}:`) : text;
}
export function statuslineColor(key: string, colors: Record<string, string>): string {
  return colors[key] ?? colors.normal ?? colorThemes[0].colors[key] ?? '#22d3ee';
}

// Complete UI order is independent of visibility; disabled items keep their place.
export function statuslineOrder(options: StatuslineOptions): string[] {
  return [...new Set([...(options.order ?? options.segments), ...statuslineSegments])].filter(key => statuslineSegments.includes(key));
}
export function orderStatusline(options: StatuslineOptions, order: string[]): StatuslineOptions {
  return { ...options, order, segments: order.filter(key => options.segments.includes(key)) };
}
export function toggleStatusline(options: StatuslineOptions, key: string, enabled: boolean): StatuslineOptions {
  const order = statuslineOrder(options);
  return { ...options, order, segments: order.filter(item => item === key ? enabled : options.segments.includes(item)) };
}
export function moveStatusline(options: StatuslineOptions, key: string, target: string): StatuslineOptions {
  const order = statuslineOrder(options);
  const from = order.indexOf(key), to = order.indexOf(target);
  if (from < 0 || to < 0 || from === to) return options;
  order.splice(from, 1); order.splice(to, 0, key);
  return orderStatusline(options, order);
}

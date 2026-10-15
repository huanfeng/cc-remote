import { useEffect, useRef, useState } from 'react';
import { CheckIcon, ChevronDownIcon, RefreshIcon } from './icons';
import type { SelectOption } from './Select';

export type Api = <T>(path: string, options?: RequestInit) => Promise<T>;
export type ModelOption = { id: string; label: string; source: string; description?: string };
export type ModelList = { models: ModelOption[]; fetchedAt: string; restricted: boolean; warnings?: string[] };

export const PERMISSION_MODES: [string, string][] = [
  ['default', 'default · 每次审批'],
  ['plan', 'plan · 只规划不修改'],
  ['acceptEdits', 'acceptEdits · 自动接受文件修改'],
  ['dontAsk', 'dontAsk · 未预先允许的工具直接拒绝'],
  ['auto', 'auto · 自动模式（自动判断审批）'],
  ['bypassPermissions', 'bypassPermissions · 跳过所有审批'],
];
export const EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'];
export const dangerousMode = (mode: string) => ['bypassPermissions', 'dontAsk', 'auto'].includes(mode);
export const modeLabel = (mode: string) => PERMISSION_MODES.find(([value]) => value === mode)?.[1] || mode;
const EFFORT_NOTES: Record<string, string> = { low: '更快、更省', medium: '均衡', high: '更深入思考', xhigh: '高强度思考', max: '最大推理预算' };

/** Options for the custom Select; the empty value means "inherit". Unknown saved modes stay selectable. */
export function permissionOptions(inheritLabel: string, current = ''): SelectOption[] {
  const options: SelectOption[] = [{ value: '', label: inheritLabel }, ...PERMISSION_MODES.map(([mode, text]) => ({ value: mode, label: mode, description: text.split(' · ')[1],
    ...(mode === 'bypassPermissions' ? { tag: '危险', tone: 'danger' as const } : dangerousMode(mode) ? { tag: '谨慎', tone: 'warn' as const } : {}) }))];
  if (current && !options.some(option => option.value === current)) options.push({ value: current, label: current, description: '现有原生配置' });
  return options;
}
export const effortOptions = (inheritLabel: string): SelectOption[] => [{ value: '', label: inheritLabel }, ...EFFORTS.map(x => ({ value: x, label: x, description: EFFORT_NOTES[x] }))];

/** Asks before newly enabling a mode that skips or auto-answers approvals. */
export function confirmMode(mode: string, previous: string) {
  if (!dangerousMode(mode) || mode === previous) return true;
  const detail = mode === 'bypassPermissions' ? 'Claude 将不再请求任何工具审批，可直接修改文件、执行命令。' : mode === 'auto' ? 'Claude 将自动判断并批准部分工具调用。' : '未预先允许的工具会被直接拒绝，不再询问。';
  return window.confirm(`确认启用 ${mode}？\n${detail}\n仅在可信项目中使用。`);
}

const groups: [string, string[]][] = [['别名', ['alias']], ['配置 / 环境', ['settings', 'env']], ['最近使用', ['recent']], ['API', ['api']]];
const cache = new Map<string, Promise<ModelList>>();

export function loadModels(api: Api, sessionId = '', refresh = false) {
  const key = sessionId;
  if (refresh || !cache.has(key)) {
    const query = new URLSearchParams();
    if (sessionId) query.set('sessionId', sessionId);
    if (refresh) query.set('refresh', '1');
    const request = api<ModelList>(`/api/models${query.toString() ? `?${query}` : ''}`);
    request.catch(() => cache.delete(key));
    cache.set(key, request);
  }
  return cache.get(key)!;
}

/** Free-text model input with grouped suggestions; empty value follows the inherited default.
 * The "default" entry is Claude Code's model alias and is passed to --model as is. */
export function ModelPicker({ api, value, onChange, onCommit, sessionId, inheritLabel = '跟随默认', label = '模型' }: { api: Api; value: string; onChange: (value: string) => void; onCommit?: (value: string) => void; sessionId?: string; inheritLabel?: string; label?: string }) {
  const [list, setList] = useState<ModelList>();
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const root = useRef<HTMLDivElement>(null);
  async function load(refresh = false) {
    setLoading(true); setError('');
    try { setList(await loadModels(api, sessionId, refresh)); } catch (err) { setError((err as Error).message); } finally { setLoading(false); }
  }
  useEffect(() => { void load(); }, [api, sessionId]);
  const pick = (id: string) => { onChange(id); onCommit?.(id); setOpen(false); };
  const filter = value.trim().toLowerCase();
  const matches = (list?.models || []).filter(model => !filter || model.id.toLowerCase().includes(filter) || list?.models.some(item => item.id === value));
  return <div className="model-picker" ref={root} onBlur={event => { if (!root.current?.contains(event.relatedTarget as Node)) { setOpen(false); onCommit?.(value); } }}>
    <div className="model-input">
      <input aria-label={label} placeholder={inheritLabel} value={value} onFocus={() => setOpen(true)} onChange={event => { onChange(event.target.value); setOpen(true); }} onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); setOpen(false); onCommit?.(value); } if (event.key === 'Escape' && open) { event.stopPropagation(); setOpen(false); } }} />
      <button type="button" className="model-button" aria-label="展开模型列表" aria-expanded={open} onClick={() => setOpen(old => !old)}><ChevronDownIcon size={14} /></button>
      <button type="button" className="model-button" aria-label="刷新模型列表" title={list ? `获取于 ${new Date(list.fetchedAt).toLocaleTimeString()}` : '刷新'} disabled={loading} onClick={() => void load(true)}><span className={loading ? 'spinning' : ''}><RefreshIcon size={14} /></span></button>
    </div>
    {open && <div className="model-menu popover-menu" role="listbox" aria-label="模型建议" onMouseDown={event => event.preventDefault()}>
      <button type="button" role="option" className="popover-option" aria-selected={!value} onClick={() => pick('')}><span className="popover-check">{!value && <CheckIcon size={14} />}</span><span className="popover-text"><span className="popover-label">{inheritLabel}</span><small>留空 = 清除覆盖，不传 --model（与 Claude 的 default 别名不同）</small></span></button>
      {groups.map(([name, sources]) => { const items = matches.filter(model => sources.includes(model.source)); return items.length ? <div key={name}><span className="model-group">{name}</span>{items.map(model => <button type="button" role="option" className="popover-option" key={model.id} aria-selected={model.id === value} onClick={() => pick(model.id)}><span className="popover-check">{model.id === value && <CheckIcon size={14} />}</span><span className="popover-text"><span className="popover-label">{model.label}</span>{model.description && <small>{model.description}</small>}</span></button>)}</div> : null; })}
      {list?.restricted && <span className="model-group">组织/项目 availableModels 已限制可选模型</span>}
      {list?.warnings?.map(warning => <span key={warning} className="model-group">{warning}</span>)}
      {error && <span className="model-group">{error}</span>}
    </div>}
  </div>;
}

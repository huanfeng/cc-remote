import { useEffect, useRef, useState } from 'react';
import { CheckIcon, ChevronDownIcon, RefreshIcon } from './icons';
import type { SelectOption } from './Select';
import { locale, t } from './i18n';
import { codexPermissions, dangerousPermission, providerEfforts, providerName, providerQuery, type Provider } from './providers';

export type Api = <T>(path: string, options?: RequestInit) => Promise<T>;
export type ModelOption = { id: string; label: string; source: string; description?: string; efforts?: string[]; defaultEffort?: string; isDefault?: boolean };
export type ModelList = { models: ModelOption[]; fetchedAt: string; restricted: boolean; warnings?: string[] };

/** Prefer the selected model's advertised capabilities over a generic list. */
export function modelEfforts(list: ModelList | undefined, model: string, provider: Provider) {
  const selected = list?.models.find(item => item.id === model) || (!model ? list?.models.find(item => item.isDefault) : undefined);
  return provider === 'codex' && selected?.efforts?.length ? selected.efforts : providerEfforts(provider);
}

export const PERMISSION_MODES: [string, string][] = [
  ['default', t('default · ask for each approval', 'default · 每次审批')],
  ['plan', t('plan · plan only, no changes', 'plan · 只规划不修改')],
  ['acceptEdits', t('acceptEdits · auto-accept file edits', 'acceptEdits · 自动接受文件修改')],
  ['dontAsk', t('dontAsk · deny tools not allowed in advance', 'dontAsk · 未预先允许的工具直接拒绝')],
  ['auto', t('auto · auto mode (approvals decided automatically)', 'auto · 自动模式（自动判断审批）')],
  ['bypassPermissions', t('bypassPermissions · skip all approvals', 'bypassPermissions · 跳过所有审批')],
];
export const EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'];
export const dangerousMode = dangerousPermission;
export const modeLabel = (mode: string) => PERMISSION_MODES.find(([value]) => value === mode)?.[1] || mode;
export const EFFORT_NOTES: Record<string, string> = { none: t('No reasoning', '不启用推理'), minimal: t('Minimal reasoning', '最少推理'), low: t('Faster, cheaper', '更快、更省'), medium: t('Balanced', '均衡'), high: t('Thinks deeper', '更深入思考'), xhigh: t('Extra-high thinking', '高强度思考'), max: t('Maximum reasoning budget', '最大推理预算') };
/** Model list groups by the source of their entries. */
export const MODEL_GROUPS: [string, string[]][] = [[t('Aliases', '别名'), ['alias']], [t('Settings / environment', '配置 / 环境'), ['settings', 'env']], [t('Recently used', '最近使用'), ['recent']], ['API', ['api']], ['Codex', ['codex']]];

/** Options for the custom Select; the empty value means "inherit". Unknown saved modes stay selectable. */
export function permissionOptions(inheritLabel: string, current = '', provider: Provider = 'claude'): SelectOption[] {
  if (provider === 'codex') return [{ value: '', label: inheritLabel }, ...codexPermissions.map(value => ({ value, label: ({ 'read-only': t('Read only', '只读'), 'workspace-write': t('Workspace write', '工作区可写'), 'danger-full-access': t('Full Access', '完全访问') } as Record<string, string>)[value], ...(dangerousMode(value, provider) ? { tag: t('Dangerous', '危险'), tone: 'danger' as const } : {}) }))];
  const options: SelectOption[] = [{ value: '', label: inheritLabel }, ...PERMISSION_MODES.map(([mode, text]) => ({ value: mode, label: ({ default: t('Default', '默认审批'), plan: t('Plan', '规划模式'), acceptEdits: t('Accept edits', '自动接受编辑'), dontAsk: t('Do not ask', '不再询问'), auto: t('Automatic', '自动审批'), bypassPermissions: t('Bypass approvals', '跳过审批') } as Record<string, string>)[mode], description: text.split(' · ')[1],
    ...(mode === 'bypassPermissions' ? { tag: t('Dangerous', '危险'), tone: 'danger' as const } : dangerousMode(mode) ? { tag: t('Caution', '谨慎'), tone: 'warn' as const } : {}) }))];
  if (current && !options.some(option => option.value === current)) options.push({ value: current, label: current, description: t('Existing native setting', '现有原生配置') });
  return options;
}
export function permissionLabel(mode: string, provider: Provider = 'claude') {
  return permissionOptions('', mode, provider).find(option => option.value === mode)?.label || mode;
}
export const effortOptions = (inheritLabel: string, provider: Provider = 'claude'): SelectOption[] => [{ value: '', label: inheritLabel }, ...providerEfforts(provider).map(x => ({ value: x, label: x, description: EFFORT_NOTES[x] }))];

const caches = new WeakMap<Api, Map<string, Promise<ModelList>>>();

export function loadModels(api: Api, sessionId = '', refresh = false, provider: Provider = 'claude') {
  let cache = caches.get(api);
  if (!cache) { cache = new Map(); caches.set(api, cache); }
  const key = `${provider}:${sessionId}`;
  if (refresh || !cache.has(key)) {
    const query = new URLSearchParams();
    if (sessionId) query.set('sessionId', sessionId);
    if (refresh) query.set('refresh', '1');
    const request = api<ModelList>(providerQuery(`/api/models${query.toString() ? `?${query}` : ''}`, provider));
    request.catch(() => { if (cache.get(key) === request) cache.delete(key); });
    cache.set(key, request);
  }
  return cache.get(key)!;
}

/** Free-text model input with grouped suggestions; empty value follows the inherited default.
 * The "default" entry is Claude Code's model alias and is passed to --model as is. */
export function ModelPicker({ api, value, onChange, onCommit, sessionId, provider = 'claude', inheritLabel = t('Use default', '跟随默认'), label = t('Model', '模型') }: { api: Api; value: string; onChange: (value: string) => void; onCommit?: (value: string) => void; sessionId?: string; provider?: Provider; inheritLabel?: string; label?: string }) {
  const [loaded, setLoaded] = useState<{ api: Api; sessionId: string; provider: Provider; data: ModelList }>();
  const list = loaded?.api === api && loaded.sessionId === (sessionId || '') && loaded.provider === provider ? loaded.data : undefined;
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const root = useRef<HTMLDivElement>(null);
  const generation = useRef(0);
  async function load(refresh = false) {
    const requestGeneration = ++generation.current;
    setLoading(true); setError('');
    try { const data = await loadModels(api, sessionId, refresh, provider); if (generation.current === requestGeneration) setLoaded({ api, sessionId: sessionId || '', provider, data }); } catch (err) { if (generation.current === requestGeneration) setError((err as Error).message); } finally { if (generation.current === requestGeneration) setLoading(false); }
  }
  useEffect(() => { setLoaded(undefined); void load(); return () => { generation.current++; }; }, [api, sessionId, provider]);
  const pick = (id: string) => { onChange(id); onCommit?.(id); setOpen(false); };
  const filter = value.trim().toLowerCase();
  const matches = (list?.models || []).filter(model => !filter || model.id.toLowerCase().includes(filter) || list?.models.some(item => item.id === value));
  return <div className="model-picker" ref={root} onBlur={event => { if (!root.current?.contains(event.relatedTarget as Node)) { setOpen(false); onCommit?.(value); } }}>
    <div className="model-input">
      <input aria-label={label} placeholder={inheritLabel} value={value} onFocus={() => setOpen(true)} onChange={event => { onChange(event.target.value); setOpen(true); }} onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); setOpen(false); onCommit?.(value); } if (event.key === 'Escape' && open) { event.stopPropagation(); setOpen(false); } }} />
      <button type="button" className="model-button" aria-label={t('Show model list', '展开模型列表')} aria-expanded={open} onClick={() => setOpen(old => !old)}><ChevronDownIcon size={14} /></button>
      <button type="button" className="model-button" aria-label={t('Refresh model list', '刷新模型列表')} title={list ? t(`Fetched at ${new Date(list.fetchedAt).toLocaleTimeString(locale)}`, `获取于 ${new Date(list.fetchedAt).toLocaleTimeString(locale)}`) : t('Refresh', '刷新')} disabled={loading} onClick={() => void load(true)}><span className={loading ? 'spinning' : ''}><RefreshIcon size={14} /></span></button>
    </div>
    {loading && <small role="status">{t('Loading models… You can also enter a model manually.', '正在读取模型… 也可手动输入模型。')}</small>}
    {open && <div className="model-menu popover-menu" role="listbox" aria-label={t('Model suggestions', '模型建议')} onMouseDown={event => event.preventDefault()}>
      <button type="button" role="option" className="popover-option" aria-selected={!value} onClick={() => pick('')}><span className="popover-check">{!value && <CheckIcon size={14} />}</span><span className="popover-text"><span className="popover-label">{inheritLabel}</span><small>{t(`Empty = follow ${providerName(provider)} settings`, `留空 = 跟随 ${providerName(provider)} 配置`)}</small></span></button>
      {MODEL_GROUPS.map(([name, sources]) => { const items = matches.filter(model => sources.includes(model.source)); return items.length ? <div key={name}><span className="model-group">{name}</span>{items.map(model => <button type="button" role="option" className="popover-option" key={model.id} aria-selected={model.id === value} onClick={() => pick(model.id)}><span className="popover-check">{model.id === value && <CheckIcon size={14} />}</span><span className="popover-text"><span className="popover-label">{model.label}</span>{model.description && <small>{model.description}</small>}</span></button>)}</div> : null; })}
      {list?.restricted && <span className="model-group">{t('availableModels in organization / project settings limits the choices', '组织/项目 availableModels 已限制可选模型')}</span>}
      {list?.warnings?.map(warning => <span key={warning} className="model-group">{warning}</span>)}
      {error && <span className="model-group">{error}</span>}
    </div>}
  </div>;
}

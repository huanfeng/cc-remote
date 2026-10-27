import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { CheckIcon, RefreshIcon } from './icons';
import { EFFORT_NOTES, MODEL_GROUPS, loadModels, modelEfforts, type Api, type ModelList } from './ModelPicker';
import { t } from './i18n';
import { providerName, type Provider } from './providers';

export type QuickKind = 'model' | 'effort';

/** Groups that open as side submenus instead of crowding the main list. */
const FOLDED = [t('Aliases', '别名'), t('Recently used', '最近使用')];
export type QuickTarget = { kind: QuickKind; anchor: DOMRect };

/** A list under a model or effort chip: picking an item switches at once. */
export function QuickSwitch({ api, sessionId, provider = 'claude', model = '', target, current, inherit, close, pick }: { api: Api; sessionId: string; provider?: Provider; model?: string; target: QuickTarget; current: string; inherit?: string; close: () => void; pick: (value: string) => void }) {
  const [loaded, setLoaded] = useState<{ api: Api; sessionId: string; provider: Provider; data: ModelList }>();
  const list = loaded?.api === api && loaded.sessionId === (sessionId || '') && loaded.provider === provider ? loaded.data : undefined;
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [place, setPlace] = useState({ left: target.anchor.left, top: target.anchor.bottom + 4 });
  const [sub, setSub] = useState<{ name: string; rect: DOMRect } | null>(null);
  const menu = useRef<HTMLDivElement>(null);
  const generation = useRef(0);
  async function load(refresh = false) {
    const requestGeneration = ++generation.current;
    setLoading(true); setError('');
    try { const data = await loadModels(api, sessionId, refresh, provider); if (generation.current === requestGeneration) setLoaded({ api, sessionId, provider, data }); } catch (err) { if (generation.current === requestGeneration) setError((err as Error).message); } finally { if (generation.current === requestGeneration) setLoading(false); }
  }
  useEffect(() => { setLoaded(undefined); if (target.kind === 'model' || provider === 'codex') void load(); return () => { generation.current++; }; }, [api, target.kind, sessionId, provider]);
  useEffect(() => {
    const away = (event: MouseEvent | TouchEvent) => { if (!menu.current?.contains(event.target as Node) && !(event.target as Element).closest?.('[data-quick-switch],.quick-switch-sub')) close(); };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.stopPropagation(); close(); } };
    document.addEventListener('mousedown', away); document.addEventListener('touchstart', away); document.addEventListener('keydown', key, true);
    return () => { document.removeEventListener('mousedown', away); document.removeEventListener('touchstart', away); document.removeEventListener('keydown', key, true); };
  }, [close]);
  // Keep the menu inside the window (phones, chips near the right edge).
  useLayoutEffect(() => {
    const box = menu.current?.getBoundingClientRect();
    if (!box) return;
    const below = target.anchor.bottom + 4 + box.height <= window.innerHeight - 8;
    setPlace({ left: Math.max(8, Math.min(target.anchor.left, window.innerWidth - box.width - 8)), top: below ? target.anchor.bottom + 4 : Math.max(8, target.anchor.top - 4 - box.height) });
  }, [target, list]);
  const option = (value: string, label: string, note?: string) => <button type="button" role="option" className="popover-option" key={value || 'inherit'} aria-selected={value === current} onClick={() => { pick(value); close(); }}>
    <span className="popover-check">{value === current && <CheckIcon size={14} />}</span><span className="popover-text"><span className="popover-label">{label}</span>{note && <small>{note}</small>}</span>
  </button>;
  return <div ref={menu} className="quick-switch popover-menu" role="listbox" aria-label={target.kind === 'model' ? t('Switch model', '切换模型') : t('Switch effort', '切换推理强度')} style={{ left: place.left, top: place.top }}>
    {target.kind === 'effort' ? <>
      {inherit !== undefined && option('', t(`Use default (${inherit || `${providerName(provider)} settings`})`, `跟随默认（${inherit || `${providerName(provider)} 配置`}）`))}
      {modelEfforts(list, model, provider).map(level => option(level, level, EFFORT_NOTES[level]))}
    </> : <>
      <div className="quick-switch-head"><span>{t('Switch model', '切换模型')}</span><button type="button" className="model-button" aria-label={t('Refresh model list', '刷新模型列表')} disabled={loading} onClick={() => void load(true)}><span className={loading ? 'spinning' : ''}><RefreshIcon size={13} /></span></button></div>
      {inherit !== undefined && option('', t(`Use default (${inherit || `${providerName(provider)} settings`})`, `跟随默认（${inherit || `${providerName(provider)} 配置`}）`))}
      {/* The host's real catalog first; aliases and recents open as side submenus. */}
      {MODEL_GROUPS.filter(([name]) => !FOLDED.includes(name)).map(([name, sources]) => { const items = (list?.models || []).filter(model => sources.includes(model.source)); return items.length ? <div key={name}><span className="model-group">{name}</span>{items.map(model => option(model.id, model.label, model.description))}</div> : null; })}
      {MODEL_GROUPS.filter(([name]) => FOLDED.includes(name)).map(([name, sources]) => { const items = (list?.models || []).filter(model => sources.includes(model.source)); if (!items.length) return null;
        return <div key={name}><button type="button" className="model-group model-group-toggle" aria-haspopup="menu" aria-expanded={sub?.name === name} onClick={event => { const rect = event.currentTarget.getBoundingClientRect(); setSub(old => old?.name === name ? null : { name, rect }); }}><span>{name}</span><span className="model-group-count">{items.length}</span><span className="model-group-arrow">›</span></button></div>; })}
      {!list && loading && <span className="model-group">{t('Loading…', '正在读取…')}</span>}
      {sub && (() => {
        const group = MODEL_GROUPS.find(([name]) => name === sub.name);
        const items = group ? (list?.models || []).filter(model => group[1].includes(model.source)) : [];
        if (!items.length) return null;
        const width = 230;
        const flip = sub.rect.right + width + 12 > window.innerWidth;
        return createPortal(<div className="quick-switch-sub popover-menu" role="listbox" style={{ left: flip ? Math.max(8, sub.rect.left - width - 6) : sub.rect.right + 6, top: Math.max(8, sub.rect.top - 6) }} onMouseDown={event => event.preventDefault()}>{items.map(model => option(model.id, model.label, model.description))}</div>, document.body);
      })()}
      {list?.warnings?.map(warning => <span key={warning} className="model-group">{warning}</span>)}
      {error && <span className="model-group">{error}</span>}
    </>}
  </div>;
}

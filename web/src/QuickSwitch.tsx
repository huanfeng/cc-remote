import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { CheckIcon, RefreshIcon } from './icons';
import { EFFORTS, loadModels, type Api, type ModelList } from './ModelPicker';

export type QuickKind = 'model' | 'effort';
export type QuickTarget = { kind: QuickKind; anchor: DOMRect };

const EFFORT_NOTES: Record<string, string> = { low: '更快、更省', medium: '均衡', high: '更深入思考', xhigh: '高强度思考', max: '最大推理预算' };
const GROUPS: [string, string[]][] = [['别名', ['alias']], ['配置 / 环境', ['settings', 'env']], ['最近使用', ['recent']], ['API', ['api']]];

/** A list under a model or effort chip: picking an item switches at once. */
export function QuickSwitch({ api, sessionId, target, current, inherit, close, pick }: { api: Api; sessionId: string; target: QuickTarget; current: string; inherit?: string; close: () => void; pick: (value: string) => void }) {
  const [list, setList] = useState<ModelList>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [place, setPlace] = useState({ left: target.anchor.left, top: target.anchor.bottom + 4 });
  const menu = useRef<HTMLDivElement>(null);
  async function load(refresh = false) {
    setLoading(true); setError('');
    try { setList(await loadModels(api, sessionId, refresh)); } catch (err) { setError((err as Error).message); } finally { setLoading(false); }
  }
  useEffect(() => { if (target.kind === 'model') void load(); }, [target.kind, sessionId]);
  useEffect(() => {
    const away = (event: MouseEvent | TouchEvent) => { if (!menu.current?.contains(event.target as Node)) close(); };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') close(); };
    document.addEventListener('mousedown', away); document.addEventListener('touchstart', away); document.addEventListener('keydown', key);
    return () => { document.removeEventListener('mousedown', away); document.removeEventListener('touchstart', away); document.removeEventListener('keydown', key); };
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
  return <div ref={menu} className="quick-switch popover-menu" role="listbox" aria-label={target.kind === 'model' ? '切换模型' : '切换推理强度'} style={{ left: place.left, top: place.top }}>
    {target.kind === 'effort' ? <>
      {inherit !== undefined && option('', `跟随默认（${inherit || 'Claude 配置'}）`)}
      {EFFORTS.map(level => option(level, level, EFFORT_NOTES[level]))}
    </> : <>
      <div className="quick-switch-head"><span>切换模型</span><button type="button" className="model-button" aria-label="刷新模型列表" disabled={loading} onClick={() => void load(true)}><span className={loading ? 'spinning' : ''}><RefreshIcon size={13} /></span></button></div>
      {inherit !== undefined && option('', `跟随默认（${inherit || 'Claude 配置'}）`)}
      {GROUPS.map(([name, sources]) => { const items = (list?.models || []).filter(model => sources.includes(model.source)); return items.length ? <div key={name}><span className="model-group">{name}</span>{items.map(model => option(model.id, model.label, model.description))}</div> : null; })}
      {!list && loading && <span className="model-group">正在读取…</span>}
      {error && <span className="model-group">{error}</span>}
    </>}
  </div>;
}

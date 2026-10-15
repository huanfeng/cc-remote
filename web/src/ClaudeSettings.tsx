import { useEffect, useState, type KeyboardEvent } from 'react';
import { CloseIcon } from './icons';
import { ModelPicker, confirmMode, dangerousMode, effortOptions, permissionOptions, type Api } from './ModelPicker';
import { Select } from './Select';

export type { Api } from './ModelPicker';
export type ClaudePreferences = { model: string; effortLevel: string; permissionMode: string; revision: string; path?: string };
export type RemotePreferences = { permissionMode: string; model: string; effort: string; revision: string };

// Settings sections; add an entry here and a matching panel below to extend the dialog.
const SECTIONS = [
  { id: 'remote', label: 'CC Remote 默认', hint: '推荐' },
  { id: 'claude', label: 'Claude 用户配置', hint: 'settings.json' },
] as const;
type SectionId = typeof SECTIONS[number]['id'];

export function ClaudeSettings({ api, close }: { api: Api; close: () => void }) {
  const [section, setSection] = useState<SectionId>('remote');
  const [prefs, setPrefs] = useState<RemotePreferences>();
  const [savedPrefs, setSavedPrefs] = useState<RemotePreferences>();
  const [value, setValue] = useState<ClaudePreferences>();
  const [savedValue, setSavedValue] = useState<ClaudePreferences>();
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState('');
  async function load() {
    setError(''); setNotice('');
    try { const p = await api<RemotePreferences>('/api/preferences'); setPrefs(p); setSavedPrefs(p); } catch (err) { setError((err as Error).message); }
  }
  async function loadClaude() {
    try { const v = await api<ClaudePreferences>('/api/claude/settings'); setValue(v); setSavedValue(v); } catch (err) { setError((err as Error).message); }
  }
  useEffect(() => { void load(); }, [api]);
  useEffect(() => { if (section === 'claude' && !value) void loadClaude(); }, [section]);
  async function save(run: () => Promise<void>) {
    setSaving(true); setError(''); setNotice('');
    try { await run(); } catch (err) { setError((err as Error).message); } finally { setSaving(false); }
  }
  const edit = (update: Partial<RemotePreferences>) => { setNotice(''); setPrefs(old => old && { ...old, ...update }); };
  const editClaude = (update: Partial<ClaudePreferences>) => { setNotice(''); setValue(old => old && { ...old, ...update }); };
  const tabKey = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    const next = { ArrowRight: index + 1, ArrowDown: index + 1, ArrowLeft: index - 1, ArrowUp: index - 1, Home: 0, End: SECTIONS.length - 1 }[event.key];
    if (next === undefined) return;
    event.preventDefault();
    const target = SECTIONS[(next + SECTIONS.length) % SECTIONS.length];
    setSection(target.id); document.getElementById(`settings-tab-${target.id}`)?.focus();
  };
  return <div className="modal-backdrop"><div className="modal settings-modal" role="dialog" aria-label="设置">
    <div className="modal-heading"><h2>设置</h2><button type="button" className="icon" aria-label="关闭设置" onClick={close}><CloseIcon size={18} /></button></div>
    <div className="settings-layout">
      <div className="settings-tabs" role="tablist" aria-label="设置分区" aria-orientation="vertical">
        {SECTIONS.map((item, index) => <button key={item.id} id={`settings-tab-${item.id}`} type="button" role="tab" aria-selected={section === item.id} aria-controls={`settings-panel-${item.id}`} tabIndex={section === item.id ? 0 : -1} onClick={() => setSection(item.id)} onKeyDown={event => tabKey(event, index)}>{item.label}<small>{item.hint}</small></button>)}
      </div>
      <div className="settings-panel" role="tabpanel" id={`settings-panel-${section}`} aria-labelledby={`settings-tab-${section}`}>
        {section === 'remote' && <form onSubmit={event => { event.preventDefault(); if (!prefs || !savedPrefs) return; if (!confirmMode(prefs.permissionMode, savedPrefs.permissionMode)) return; void save(async () => { const result = await api<RemotePreferences>('/api/preferences', { method: 'PUT', body: JSON.stringify({ ...prefs, confirmDangerous: dangerousMode(prefs.permissionMode) }) }); setPrefs(result); setSavedPrefs(result); setNotice('CC Remote 默认值已保存，托管会话下一轮生效。'); }); }}>
          <h3>CC Remote 默认（推荐，不修改 Claude 配置）</h3>
          <p>以启动参数传给托管会话和 <code>cc-remote -- claude</code> 新启动的共享终端；会话可单独覆盖。留空则跟随 Claude 自身配置。终端命令行显式传入的参数优先。</p>
          {prefs ? <>
            <label>权限模式<Select label="CC Remote 默认权限模式" value={prefs.permissionMode} options={permissionOptions('跟随 Claude 配置', prefs.permissionMode)} onChange={permissionMode => edit({ permissionMode })} /></label>
            {prefs.permissionMode === 'bypassPermissions' && <small className="danger-note">跳过所有审批：Claude 可直接修改文件、执行命令。仅在可信项目中使用。</small>}
            <label>模型<ModelPicker api={api} label="CC Remote 默认模型" inheritLabel="跟随 Claude 配置" value={prefs.model} onChange={model => edit({ model })} /></label>
            <label>推理强度<Select label="CC Remote 默认推理强度" value={prefs.effort} options={effortOptions('跟随 Claude 配置')} onChange={effort => edit({ effort })} /></label>
            <button className="primary" disabled={saving}>{saving ? '保存中…' : '保存 CC Remote 默认'}</button>
          </> : <p>正在读取设置…</p>}
        </form>}
        {section === 'claude' && <div>
          <h3>Claude 用户配置（~/.claude/settings.json）</h3>
          <p>修改 Claude 用户级默认值，会影响此账户下所有 Claude Code 使用（包括不经过 CC Remote 的终端）。只改写 model、effortLevel、permissions.defaultMode 三项，保留原有键顺序、缩进与换行；本次宿主运行内首次写入前备份为 <code>settings.json.cc-remote.bak</code>。</p>
          {value && savedValue ? <form onSubmit={event => { event.preventDefault(); if (!confirmMode(value.permissionMode, savedValue.permissionMode)) return; void save(async () => { const result = await api<ClaudePreferences>('/api/claude/settings', { method: 'PUT', body: JSON.stringify({ ...value, confirmDangerous: dangerousMode(value.permissionMode) }) }); setValue(result); setSavedValue(result); setNotice('已写入 Claude 用户配置。'); }); }}>
            <label>默认模型<ModelPicker api={api} label="Claude 默认模型" inheritLabel="跟随 Claude 默认值" value={value.model} onChange={model => editClaude({ model })} /></label>
            <label>默认推理强度<Select label="Claude 默认推理强度" value={value.effortLevel} options={effortOptions('跟随 Claude 默认值')} onChange={effortLevel => editClaude({ effortLevel })} /></label>
            <label>默认权限模式<Select label="Claude 默认权限模式" value={value.permissionMode} options={permissionOptions('跟随 Claude 默认值', savedValue.permissionMode)} onChange={permissionMode => editClaude({ permissionMode })} /></label>
            <small>项目、组织策略及启动参数可能覆盖这些默认值；其它 Claude 配置原样保留。</small>
            <small className="settings-path">{value.path}</small>
            <button className="secondary" disabled={saving}>{saving ? '保存中…' : '写入 Claude 用户配置'}</button>
          </form> : <p>正在读取…</p>}
        </div>}
        {notice && <p role="status">{notice}</p>}
        {error && <div className="error" role="alert">{error}<button type="button" className="secondary" onClick={() => { void load(); if (section === 'claude' || value) void loadClaude(); }}>重新加载</button></div>}
      </div>
    </div>
  </div></div>;
}

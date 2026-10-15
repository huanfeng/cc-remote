import { useEffect, useState } from 'react';

export type ClaudePreferences = { model: string; effortLevel: string; permissionMode: string; revision: string; path?: string };
export type Api = <T>(path: string, options?: RequestInit) => Promise<T>;

export function ClaudeSettings({ api, close }: { api: Api; close: () => void }) {
  const [value, setValue] = useState<ClaudePreferences>();
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  async function load() {
    setError(''); setSaved(false);
    try { setValue(await api<ClaudePreferences>('/api/claude/settings')); } catch (err) { setError((err as Error).message); }
  }
  useEffect(() => { void load(); }, [api]);
  return <div className="modal-backdrop"><form className="modal" onSubmit={async event => {
    event.preventDefault(); if (!value) return;
    setSaving(true); setError(''); setSaved(false);
    try { setValue(await api<ClaudePreferences>('/api/claude/settings', { method: 'PUT', body: JSON.stringify(value) })); setSaved(true); }
    catch (err) { setError((err as Error).message); } finally { setSaving(false); }
  }}><div className="modal-heading"><h2>Claude 设置</h2><button type="button" className="icon" aria-label="关闭 Claude 设置" onClick={close}>×</button></div>
    <p>修改宿主 Claude 的用户级默认值。托管会话下一轮读取；共享终端的模型与推理强度需要重新启动，或在终端用 /model、/effort 即时切换。</p>
    {value ? <>
      <label>默认模型<input aria-label="默认模型" placeholder="留空跟随 Claude；也可填 sonnet、opus 或模型 ID" value={value.model} onChange={event => { setSaved(false); setValue({ ...value, model: event.target.value }); }} /></label>
      <label>默认推理强度<select aria-label="默认推理强度" value={value.effortLevel} onChange={event => { setSaved(false); setValue({ ...value, effortLevel: event.target.value }); }}><option value="">跟随 Claude 默认值</option>{['low', 'medium', 'high', 'xhigh', 'max'].map(x => <option key={x}>{x}</option>)}</select></label>
      <label>默认权限模式<select aria-label="默认权限模式" value={value.permissionMode} onChange={event => { setSaved(false); setValue({ ...value, permissionMode: event.target.value }); }}><option value="">跟随 Claude 默认值</option><option value="default">default · 每次审批</option><option value="plan">plan · 规划模式</option><option value="acceptEdits">acceptEdits · 自动接受文件修改</option>{!['', 'default', 'plan', 'acceptEdits'].includes(value.permissionMode) && <option value={value.permissionMode}>{value.permissionMode} · 现有原生配置</option>}</select></label>
      <small>模型可用性和推理强度受账户支持限制。项目、组织策略及启动参数可能覆盖默认值；已有权限规则和其它 Claude 配置会保留。</small>
      <small className="settings-path">{value.path}</small>
      {saved && <p role="status">设置已保存。</p>}
    </> : <p>正在读取设置…</p>}
    {error && <div className="error" role="alert">{error}<button type="button" className="secondary" onClick={() => void load()}>重新加载</button></div>}
    <button className="primary" disabled={!value || saving}>{saving ? '保存中…' : '保存默认设置'}</button>
  </form></div>;
}

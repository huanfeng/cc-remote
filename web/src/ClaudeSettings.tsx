import { useEffect, useState, type KeyboardEvent } from 'react';
import { CloseIcon } from './icons';
import { ModelPicker, confirmMode, dangerousMode, effortOptions, permissionOptions, type Api } from './ModelPicker';
import { Select } from './Select';
import { hostLang, langOverride, setLang, t, type Lang } from './i18n';
import { applyIdentity } from './hostIdentity';

export type { Api } from './ModelPicker';
export type ClaudePreferences = { model: string; effortLevel: string; permissionMode: string; revision: string; path?: string };
export type RemotePreferences = { permissionMode: string; model: string; effort: string; revision: string };
type IdentitySettings = { name: string; color: string; shownName: string; shownColor: string; defaultName: string; defaultColor: string; palette: { key: string; hex: string }[] };

// Settings sections; add an entry here and a matching panel below to extend the dialog.
const SECTIONS = [
  { id: 'remote', label: t('CC Remote defaults', 'CC Remote 默认'), hint: t('Recommended', '推荐') },
  { id: 'claude', label: t('Claude user settings', 'Claude 用户配置'), hint: 'settings.json' },
  { id: 'host', label: t('This host', '此主机'), hint: t('Name and color', '名称与颜色') },
  { id: 'browser', label: t('This browser', '此浏览器'), hint: t('Language', '语言') },
] as const;
// Language names are written in their own language.
const LANG_NAMES: Record<Lang, string> = { zh: '中文', en: 'English' };
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
  const [identity, setIdentity] = useState<IdentitySettings>();
  const [hostName, setHostName] = useState('');
  const [hostColor, setHostColor] = useState('');
  function showIdentity(id: IdentitySettings) { setIdentity(id); setHostName(id.name || ''); setHostColor(id.color || ''); applyIdentity({ name: id.shownName, color: id.shownColor }); }
  useEffect(() => { if (section === 'host' && !identity) api<IdentitySettings>('/api/identity').then(showIdentity).catch(err => setError((err as Error).message)); }, [section]);
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
  return <div className="modal-backdrop"><div className="modal settings-modal" role="dialog" aria-label={t('Settings', '设置')}>
    <div className="modal-heading"><h2>{t('Settings', '设置')}</h2><button type="button" className="icon" aria-label={t('Close settings', '关闭设置')} onClick={close}><CloseIcon size={18} /></button></div>
    <div className="settings-layout">
      <div className="settings-tabs" role="tablist" aria-label={t('Settings sections', '设置分区')} aria-orientation="vertical">
        {SECTIONS.map((item, index) => <button key={item.id} id={`settings-tab-${item.id}`} type="button" role="tab" aria-selected={section === item.id} aria-controls={`settings-panel-${item.id}`} tabIndex={section === item.id ? 0 : -1} onClick={() => setSection(item.id)} onKeyDown={event => tabKey(event, index)}>{item.label}<small>{item.hint}</small></button>)}
      </div>
      <div className="settings-panel" role="tabpanel" id={`settings-panel-${section}`} aria-labelledby={`settings-tab-${section}`}>
        {section === 'remote' && <form onSubmit={event => { event.preventDefault(); if (!prefs || !savedPrefs) return; if (!confirmMode(prefs.permissionMode, savedPrefs.permissionMode)) return; void save(async () => { const result = await api<RemotePreferences>('/api/preferences', { method: 'PUT', body: JSON.stringify({ ...prefs, confirmDangerous: dangerousMode(prefs.permissionMode) }) }); setPrefs(result); setSavedPrefs(result); setNotice(t('CC Remote defaults saved; managed sessions use them from their next turn.', 'CC Remote 默认值已保存，托管会话下一轮生效。')); }); }}>
          <h3>{t('CC Remote defaults (recommended, Claude settings unchanged)', 'CC Remote 默认（推荐，不修改 Claude 配置）')}</h3>
          <p>{t('Passed as launch flags to managed and terminal sessions; each session can override them. Leave empty to follow Claude\'s own settings.', '以启动参数传给托管会话和终端会话；会话可单独覆盖。留空则跟随 Claude 自己的配置。')}</p>
          {prefs ? <>
            <label>{t('Permission mode', '权限模式')}<Select label={t('CC Remote default permission mode', 'CC Remote 默认权限模式')} value={prefs.permissionMode} options={permissionOptions(t('Follow Claude settings', '跟随 Claude 配置'), prefs.permissionMode)} onChange={permissionMode => edit({ permissionMode })} /></label>
            {prefs.permissionMode === 'bypassPermissions' && <small className="danger-note">{t('Skips all approvals: Claude can edit files and run commands directly. Use it only in trusted projects.', '跳过所有审批：Claude 可直接修改文件、执行命令。仅在可信项目中使用。')}</small>}
            <label>{t('Model', '模型')}<ModelPicker api={api} label={t('CC Remote default model', 'CC Remote 默认模型')} inheritLabel={t('Follow Claude settings', '跟随 Claude 配置')} value={prefs.model} onChange={model => edit({ model })} /></label>
            <label>{t('Effort', '推理强度')}<Select label={t('CC Remote default effort', 'CC Remote 默认推理强度')} value={prefs.effort} options={effortOptions(t('Follow Claude settings', '跟随 Claude 配置'))} onChange={effort => edit({ effort })} /></label>
            <button className="primary" disabled={saving}>{saving ? t('Saving…', '保存中…') : t('Save CC Remote defaults', '保存 CC Remote 默认')}</button>
          </> : <p>{t('Loading settings…', '正在读取设置…')}</p>}
        </form>}
        {section === 'claude' && <div>
          <h3>{t('Claude user settings (~/.claude/settings.json)', 'Claude 用户配置（~/.claude/settings.json）')}</h3>
          <p>{t('Changes Claude\'s user-level defaults, which affects every Claude Code use under this account (including terminals that do not go through CC Remote). Only model, effortLevel and permissions.defaultMode are rewritten, keeping the existing key order, indentation and line endings; before the first write in this host run, a backup is saved as ', '修改 Claude 用户级默认值，会影响此账户下所有 Claude Code 使用（包括不经过 CC Remote 的终端）。只改写 model、effortLevel、permissions.defaultMode 三项，保留原有键顺序、缩进与换行；本次宿主运行内首次写入前备份为 ')}<code>settings.json.cc-remote.bak</code>{t('.', '。')}</p>
          {value && savedValue ? <form onSubmit={event => { event.preventDefault(); if (!confirmMode(value.permissionMode, savedValue.permissionMode)) return; void save(async () => { const result = await api<ClaudePreferences>('/api/claude/settings', { method: 'PUT', body: JSON.stringify({ ...value, confirmDangerous: dangerousMode(value.permissionMode) }) }); setValue(result); setSavedValue(result); setNotice(t('Written to Claude user settings.', '已写入 Claude 用户配置。')); }); }}>
            <label>{t('Default model', '默认模型')}<ModelPicker api={api} label={t('Claude default model', 'Claude 默认模型')} inheritLabel={t('Follow Claude default', '跟随 Claude 默认值')} value={value.model} onChange={model => editClaude({ model })} /></label>
            <label>{t('Default effort', '默认推理强度')}<Select label={t('Claude default effort', 'Claude 默认推理强度')} value={value.effortLevel} options={effortOptions(t('Follow Claude default', '跟随 Claude 默认值'))} onChange={effortLevel => editClaude({ effortLevel })} /></label>
            <label>{t('Default permission mode', '默认权限模式')}<Select label={t('Claude default permission mode', 'Claude 默认权限模式')} value={value.permissionMode} options={permissionOptions(t('Follow Claude default', '跟随 Claude 默认值'), savedValue.permissionMode)} onChange={permissionMode => editClaude({ permissionMode })} /></label>
            <small>{t('Project and organization policies and launch flags may override these defaults; other Claude settings are kept as they are.', '项目、组织策略及启动参数可能覆盖这些默认值；其它 Claude 配置原样保留。')}</small>
            <small className="settings-path">{value.path}</small>
            <button className="secondary" disabled={saving}>{saving ? t('Saving…', '保存中…') : t('Write Claude user settings', '写入 Claude 用户配置')}</button>
          </form> : <p>{t('Loading…', '正在读取…')}</p>}
        </div>}
        {section === 'host' && <form onSubmit={event => { event.preventDefault(); void save(async () => { showIdentity(await api<IdentitySettings>('/api/identity', { method: 'PUT', body: JSON.stringify({ name: hostName.trim(), color: hostColor }) })); setNotice(t('Saved: the tab title, icon and logo of every page on this host change.', '已保存：此主机所有页面的标签页标题、图标和标志都会更新。')); }); }}>
          <h3>{t('Host name and color', '主机名称与颜色')}</h3>
          <p>{t('Tell several hosts apart in the browser: the name is in the tab title, the sidebar and the login page, the color is the tab icon and logo. Leave them empty for the defaults: the machine name, and a color picked by the name. Also: ', '在浏览器里区分多台宿主：名称显示在标签页标题、侧栏和登录页，颜色用于标签页图标和标志。留空使用默认值：机器名，以及按名称挑选的颜色。也可以用命令：')}<code>cc-remote service config --name NAME --color COLOR</code></p>
          {identity ? <>
            <label>{t('Name', '名称')}<input value={hostName} maxLength={40} placeholder={identity.defaultName} onChange={event => setHostName(event.target.value)} /></label>
            <div className="host-colors" role="radiogroup" aria-label={t('Color', '颜色')}>
              <button type="button" role="radio" aria-checked={!hostColor} className={`host-color auto${!hostColor ? ' chosen' : ''}`} style={{ background: identity.palette.find(c => c.key === identity.defaultColor)?.hex }} title={t('Automatic', '自动')} onClick={() => setHostColor('')}>{t('Auto', '自动')}</button>
              {identity.palette.map(c => <button key={c.key} type="button" role="radio" aria-checked={hostColor === c.key} aria-label={c.key} title={c.key} className={`host-color${hostColor === c.key ? ' chosen' : ''}`} style={{ background: c.hex }} onClick={() => setHostColor(c.key)} />)}
            </div>
            <button className="primary" disabled={saving}>{saving ? t('Saving…', '保存中…') : t('Save', '保存')}</button>
          </> : <p>{t('Loading…', '加载中…')}</p>}
        </form>}
        {section === 'browser' && <div>
          <h3>{t('Language', '语言')}</h3>
          <p>{t('Saved in this browser only; by default the page follows the language configured on the host. Changing it reloads the page.', '只保存在此浏览器；默认跟随宿主配置的语言。切换后页面会重新加载。')}</p>
          <label>{t('Interface language', '界面语言')}<Select label={t('Interface language', '界面语言')} value={langOverride()} options={[{ value: '', label: t(`Follow host (${LANG_NAMES[hostLang]})`, `跟随宿主（${LANG_NAMES[hostLang]}）`) }, { value: 'zh', label: LANG_NAMES.zh }, { value: 'en', label: LANG_NAMES.en }]} onChange={value => setLang(value as Lang | '')} /></label>
        </div>}
        {notice && <p role="status">{notice}</p>}
        {error && <div className="error" role="alert">{error}<button type="button" className="secondary" onClick={() => { void load(); if (section === 'claude' || value) void loadClaude(); }}>{t('Reload', '重新加载')}</button></div>}
      </div>
    </div>
  </div></div>;
}

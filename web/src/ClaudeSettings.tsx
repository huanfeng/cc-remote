import { providerOf, providerName, providerQuery, type Provider } from './providers';
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { CloseIcon } from './icons';
import { ModelPicker, confirmMode, dangerousMode, effortOptions, permissionOptions, type Api } from './ModelPicker';
import { Select } from './Select';
import './session-details.css';
import { STANDALONE } from './hosts';
import { hostLang, langOverride, setLang, t, type Lang } from './i18n';
import { applyIdentity } from './hostIdentity';
import { ColorChoices, IconChoices } from './HostLook';
import { notifyPrefs, notifySupported, saveNotifyPrefs, type NotifyEvent, type NotifyPrefs } from './notify';

export type { Api } from './ModelPicker';
export type ClaudePreferences = { model: string; effortLevel: string; permissionMode: string; revision: string; path?: string };
export type RemotePreferences = { provider?: Provider; permissionMode: string; model: string; effort: string; revision: string };
type IdentitySettings = { name: string; color: string; icon?: string; shownName: string; shownColor: string; defaultName: string; defaultColor: string; palette: { key: string; hex: string }[] };

// Settings sections; add an entry here and a matching panel below to extend the dialog.
const SECTIONS = [
  { id: 'remote', label: t('Session defaults', '会话默认'), hint: t('Model, effort and permissions', '模型、推理与权限') },
  { id: 'host', label: t('This host', '此主机'), hint: t('Name, color and icon', '名称、颜色与图标') },
  { id: 'browser', label: t('This browser', '此浏览器'), hint: t('Language, notifications', '语言、通知') },
] as const;
const NOTIFY_EVENTS: [NotifyEvent, string][] = [
  ['waiting', t('Needs my answer: a permission, a question or a plan', '需要我操作：权限审批、提问、计划确认')],
  ['done', t('A turn finished', '一轮执行完成')],
  ['failed', t('A turn failed or Claude Code exited', '执行失败或 Claude Code 退出')],
];

/** Notifications of this browser: asks for permission when turned on. */
function NotifySettings() {
  const [prefs, setPrefs] = useState<NotifyPrefs>(notifyPrefs);
  const [permission, setPermission] = useState(() => notifySupported() ? Notification.permission : 'denied');
  const update = (next: NotifyPrefs) => { setPrefs(next); saveNotifyPrefs(next); };
  const toggle = async (on: boolean) => {
    if (on && Notification.permission !== 'granted') {
      // Older Safari only takes a callback and returns nothing: read the outcome afterwards.
      try { await new Promise(done => { const asked = Notification.requestPermission(done); if (asked) asked.then(done, done); }); } catch { /* treated as not allowed */ }
      const answer = Notification.permission as NotificationPermission; // changed by the request above
      setPermission(answer);
      if (answer !== 'granted') return;
    }
    update({ ...prefs, on });
  };
  if (!notifySupported()) return <p>{t('This page cannot show notifications: browsers allow them only over HTTPS (or on localhost), and some do not support them at all.', '此页面无法发送通知：浏览器只允许 HTTPS（或 localhost）页面发通知，有的浏览器完全不支持。')}</p>;
  const on = prefs.on && permission === 'granted';
  return <>
    <label className="checkbox"><input type="checkbox" checked={on} disabled={permission === 'denied'} onChange={event => void toggle(event.target.checked)} />{t('Show a system notification when a session needs me', '会话需要我时发系统通知')}</label>
    {permission === 'denied' && <p className="error">{t('The browser blocks notifications for this site; allow them in the site settings (the icon left of the address), then reload.', '浏览器已禁止此网站的通知；请在网站设置（地址栏左侧图标）里允许通知，然后刷新页面。')}</p>}
    {on && NOTIFY_EVENTS.map(([event, label]) => <label key={event} className="checkbox notify-event"><input type="checkbox" checked={prefs[event]} onChange={change => update({ ...prefs, [event]: change.target.checked })} />{label}</label>)}
  </>;
}

// Language names are written in their own language.
const LANG_NAMES: Record<Lang, string> = { zh: '中文', en: 'English' };
type SectionId = typeof SECTIONS[number]['id'];

export function ClaudeSettings({ api, close, provider: initialProvider = 'claude', providers = ['claude'], sendKey, onSendKey, onStatus }: { api: Api; close: () => void; provider?: Provider; providers?: Provider[]; sendKey?: string; onSendKey?: (value: string) => void; onStatus?: () => void }) {
  const [provider, setProvider] = useState<Provider>(providers.includes(initialProvider) ? initialProvider : 'claude');
  const sections = SECTIONS;
  const [nativeOpen, setNativeOpen] = useState(false);
  const requestId = useRef(0);
  const [hostInfo, setHostInfo] = useState<{ version?: string; claudeAvailable?: boolean; claudeVersion?: string; codexAvailable?: boolean; codexVersion?: string }>();
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
  const [hostColor, setHostColor] = useState(''); // #rrggbb, or '' for auto
  const [hostIcon, setHostIcon] = useState('');
  const paletteHex = (id: IdentitySettings, color: string) => id.palette.find(c => c.key === color)?.hex || color;
  function showIdentity(id: IdentitySettings) { setIdentity(id); setHostName(id.name || ''); setHostColor(paletteHex(id, id.color || '')); setHostIcon(id.icon || ''); applyIdentity({ name: id.shownName, color: id.shownColor, icon: id.icon || '' }); }
  useEffect(() => { if (section === 'host' && !identity) api<IdentitySettings>('/api/identity').then(showIdentity).catch(err => setError((err as Error).message)); }, [section]);
  async function load() {
    const request = ++requestId.current;
    setPrefs(undefined); setSavedPrefs(undefined); setError(''); setNotice('');
    try { const p = await api<RemotePreferences>(providerQuery('/api/preferences', provider)); if (providerOf(p) !== provider) throw new Error(t('This host does not support these provider defaults.', '此主机不支持该提供方的默认设置。')); if (request === requestId.current) { setPrefs(p); setSavedPrefs(p); } } catch (err) { if (request === requestId.current) setError((err as Error).message); }
  }
  async function loadClaude() {
    try { const v = await api<ClaudePreferences>('/api/claude/settings'); setValue(v); setSavedValue(v); } catch (err) { setError((err as Error).message); }
  }
  useEffect(() => { void load(); return () => { requestId.current++; }; }, [api, provider]);
  useEffect(() => { if (section === 'remote' && provider === 'claude' && nativeOpen && !value) void loadClaude(); }, [section, provider, nativeOpen]);
  useEffect(() => { let active = true; if (section === 'host' && !hostInfo) api<NonNullable<typeof hostInfo>>('/api/info').then(info => { if (active) setHostInfo(info); }).catch(err => { if (active) setError((err as Error).message); }); return () => { active = false; }; }, [api, section, hostInfo]);
  async function save(run: () => Promise<void>) {
    if (saving) return;
    setSaving(true); setError(''); setNotice('');
    try { await run(); } catch (err) { setError((err as Error).message); } finally { setSaving(false); }
  }
  const edit = (update: Partial<RemotePreferences>) => { setNotice(''); setPrefs(old => old && { ...old, ...update }); };
  const editClaude = (update: Partial<ClaudePreferences>) => { setNotice(''); setValue(old => old && { ...old, ...update }); };
  const remoteDirty = !!prefs && !!savedPrefs && (prefs.model !== savedPrefs.model || prefs.effort !== savedPrefs.effort || prefs.permissionMode !== savedPrefs.permissionMode);
  const nativeDirty = !!value && !!savedValue && (value.model !== savedValue.model || value.effortLevel !== savedValue.effortLevel || value.permissionMode !== savedValue.permissionMode);
  const hostDirty = !!identity && (hostName !== (identity.name || '') || hostColor !== paletteHex(identity, identity.color || '') || hostIcon !== (identity.icon || ''));
  const dirty = remoteDirty || nativeDirty || hostDirty;
  function mayLeave() {
    if (saving) return false;
    if (!dirty) return true;
    if (!window.confirm(t('Discard unsaved settings changes?', '放弃未保存的设置修改？'))) return false;
    setPrefs(savedPrefs); setValue(savedValue);
    if (identity) { setHostName(identity.name || ''); setHostColor(paletteHex(identity, identity.color || '')); setHostIcon(identity.icon || ''); }
    return true;
  }
  const requestClose = () => { if (mayLeave()) close(); };
  const selectSection = (next: SectionId) => { if (next === section) return true; if (!mayLeave()) return false; setSection(next); setError(''); setNotice(''); return true; };
  const closeRef = useRef(requestClose); closeRef.current = requestClose;
  useEffect(() => {
    const escape = (event: globalThis.KeyboardEvent) => { if (event.key === 'Escape') closeRef.current(); };
    document.addEventListener('keydown', escape);
    return () => document.removeEventListener('keydown', escape);
  }, []);
  useEffect(() => {
    const unload = (event: BeforeUnloadEvent) => { if (dirty || saving) { event.preventDefault(); event.returnValue = ''; } };
    window.addEventListener('beforeunload', unload);
    return () => window.removeEventListener('beforeunload', unload);
  }, [dirty, saving]);
  const tabKey = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    const next = { ArrowRight: index + 1, ArrowDown: index + 1, ArrowLeft: index - 1, ArrowUp: index - 1, Home: 0, End: sections.length - 1 }[event.key];
    if (next === undefined) return;
    event.preventDefault();
    const target = sections[(next + sections.length) % sections.length];
    if (selectSection(target.id)) document.getElementById(`settings-tab-${target.id}`)?.focus();
  };
  return <div className="modal-backdrop" onClick={event => { if (event.target === event.currentTarget) requestClose(); }}><div className="modal settings-modal" role="dialog" aria-modal="true" aria-label={t('Settings', '设置')}>
    <div className="modal-heading"><h2>{t('Settings', '设置')}</h2><button type="button" className="icon" aria-label={t('Close settings', '关闭设置')} disabled={saving} onClick={requestClose}><CloseIcon size={18} /></button></div>

    <div className="settings-layout">
      <div className="settings-tabs" role="tablist" aria-label={t('Settings sections', '设置分区')} aria-orientation="vertical">
        {sections.map((item, index) => <button key={item.id} id={`settings-tab-${item.id}`} type="button" role="tab" aria-selected={section === item.id} aria-controls={`settings-panel-${item.id}`} tabIndex={section === item.id ? 0 : -1} disabled={saving} onClick={() => selectSection(item.id)} onKeyDown={event => tabKey(event, index)}>{item.label}<small>{item.hint}</small></button>)}
      </div>
      <fieldset disabled={saving} className="settings-panel" role="tabpanel" id={`settings-panel-${section}`} aria-labelledby={`settings-tab-${section}`}>
        {section === 'remote' && <div>
          <div className="field">{t('Provider', '提供方')}<Select label={t('Settings provider', '设置提供方')} value={provider} options={providers.map(value => ({ value, label: providerName(value) }))} disabled={saving} onChange={value => { if (providers.includes(value as Provider) && value !== provider && mayLeave()) { setNativeOpen(false); setPrefs(undefined); setSavedPrefs(undefined); setProvider(value as Provider); } }} /></div>
          <form onSubmit={event => { event.preventDefault(); if (!prefs || !savedPrefs || !providers.includes(provider)) return; if (!confirmMode(prefs.permissionMode, savedPrefs.permissionMode, provider)) return; void save(async () => { const result = await api<RemotePreferences>(providerQuery('/api/preferences', provider), { method: 'PUT', body: JSON.stringify({ permissionMode: prefs.permissionMode, model: prefs.model, effort: prefs.effort, revision: prefs.revision, confirmDangerous: dangerousMode(prefs.permissionMode, provider) }) }); if (providerOf(result) !== provider) throw new Error(t('This host does not support these provider defaults.', '此主机不支持该提供方的默认设置。')); setPrefs(result); setSavedPrefs(result); setNotice(t('CC Remote defaults saved; managed sessions use them on the next send after current work and background tasks finish.', 'CC Remote 默认值已保存，托管会话在当前工作及后台任务结束后，下次发送时生效。')); }); }}>
          <h3>{t(`${providerName(provider)} · CC Remote defaults`, `${providerName(provider)} · CC Remote 默认`)}</h3>
          <p>{t(`Defaults for ${providerName(provider)} sessions only. Each session can override them; empty fields follow the provider configuration.`, `仅用于 ${providerName(provider)} 会话。会话可单独覆盖；留空跟随该提供方配置。`)}</p>
          {prefs ? <>
            <div className="field">{t('Permission mode', '权限模式')}<Select label={t('CC Remote default permission mode', 'CC Remote 默认权限模式')} value={prefs.permissionMode} options={permissionOptions(t(`Follow ${providerName(provider)} settings`, `跟随 ${providerName(provider)} 配置`), prefs.permissionMode, provider)} onChange={permissionMode => edit({ permissionMode })} /></div>
            {dangerousMode(prefs.permissionMode, provider) && <small className="danger-note">{t('Allows broad file and command access. Use only in trusted projects.', '允许广泛访问文件和执行命令。仅用于可信项目。')}</small>}
            <div className="field">{t('Model', '模型')}<ModelPicker api={api} provider={provider} label={t('CC Remote default model', 'CC Remote 默认模型')} inheritLabel={t(`Follow ${providerName(provider)} settings`, `跟随 ${providerName(provider)} 配置`)} value={prefs.model} onChange={model => edit({ model })} /></div>
            <div className="field">{t('Effort', '推理强度')}<Select label={t('CC Remote default effort', 'CC Remote 默认推理强度')} value={prefs.effort} options={effortOptions(t(`Follow ${providerName(provider)} settings`, `跟随 ${providerName(provider)} 配置`), provider)} onChange={effort => edit({ effort })} /></div>
            <button className="primary" disabled={saving}>{saving ? t('Saving…', '保存中…') : t('Save CC Remote defaults', '保存 CC Remote 默认')}</button>
          </> : <p>{t('Loading settings…', '正在读取设置…')}</p>}
        </form>
        {provider === 'claude' && <details open={nativeOpen} onToggle={event => setNativeOpen(event.currentTarget.open)}>
          <summary>{t('Native configuration · Advanced', '原生配置 · 高级')}</summary>
          <h3>{t('Claude user settings (~/.claude/settings.json)', 'Claude 用户配置（~/.claude/settings.json）')}</h3>
          <p>{t('Changes Claude\'s user-level defaults, which affects every Claude Code use under this account (including terminals that do not go through CC Remote). Only model, effortLevel and permissions.defaultMode are rewritten, keeping the existing key order, indentation and line endings; before the first write in this host run, a backup is saved as ', '修改 Claude 用户级默认值，会影响此账户下所有 Claude Code 使用（包括不经过 CC Remote 的终端）。只改写 model、effortLevel、permissions.defaultMode 三项，保留原有键顺序、缩进与换行；本次宿主运行内首次写入前备份为 ')}<code>settings.json.cc-remote.bak</code>{t('.', '。')}</p>
          {value && savedValue ? <form onSubmit={event => { event.preventDefault(); if (!confirmMode(value.permissionMode, savedValue.permissionMode)) return; void save(async () => { const result = await api<ClaudePreferences>('/api/claude/settings', { method: 'PUT', body: JSON.stringify({ ...value, confirmDangerous: dangerousMode(value.permissionMode) }) }); setValue(result); setSavedValue(result); setNotice(t('Written to Claude user settings.', '已写入 Claude 用户配置。')); }); }}>
            <div className="field">{t('Default model', '默认模型')}<ModelPicker api={api} label={t('Claude default model', 'Claude 默认模型')} inheritLabel={t('Follow Claude default', '跟随 Claude 默认值')} value={value.model} onChange={model => editClaude({ model })} /></div>
            <div className="field">{t('Default effort', '默认推理强度')}<Select label={t('Claude default effort', 'Claude 默认推理强度')} value={value.effortLevel} options={effortOptions(t('Follow Claude default', '跟随 Claude 默认值'))} onChange={effortLevel => editClaude({ effortLevel })} /></div>
            <div className="field">{t('Default permission mode', '默认权限模式')}<Select label={t('Claude default permission mode', 'Claude 默认权限模式')} value={value.permissionMode} options={permissionOptions(t('Follow Claude default', '跟随 Claude 默认值'), savedValue.permissionMode)} onChange={permissionMode => editClaude({ permissionMode })} /></div>
            <small>{t('Project and organization policies and launch flags may override these defaults; other Claude settings are kept as they are.', '项目、组织策略及启动参数可能覆盖这些默认值；其它 Claude 配置原样保留。')}</small>
            <small className="settings-path">{value.path}</small>
            <button className="secondary" disabled={saving}>{saving ? t('Saving…', '保存中…') : t('Write Claude user settings', '写入 Claude 用户配置')}</button>
          </form> : <p>{t('Loading…', '正在读取…')}</p>}
        </details>}
        </div>}
        {section === 'host' && <form onSubmit={event => { event.preventDefault(); void save(async () => { showIdentity(await api<IdentitySettings>('/api/identity', { method: 'PUT', body: JSON.stringify({ name: hostName.trim(), color: hostColor, icon: hostIcon }) })); setNotice(t('Saved: the tab title, icon and logo of every page on this host change.', '已保存：此主机所有页面的标签页标题、图标和标志都会更新。')); }); }}>
          <h3>{t('Providers and service', '提供方与服务')}</h3>
          <dl className="session-detail-list">
            <div><dt>CC Remote</dt><dd>{hostInfo?.version || t('Not reported', '未报告')}</dd></div>
            {(['claude', 'codex'] as const).map(name => <div key={name}><dt>{providerName(name)}</dt><dd>{hostInfo?.[`${name}Available`] === undefined ? t('Not reported', '未报告') : hostInfo[`${name}Available`] ? t('Installed', '已安装') : t('Unavailable', '不可用')} · {hostInfo?.[`${name}Version`] || t('Version not reported', '版本未报告')}</dd></div>)}
          </dl>
          <p>{t('Configuration sources: CC Remote session defaults and provider native configuration; CLI reports determine actual execution values.', '配置来源：CC Remote 会话默认值与提供方原生配置；实际执行值以 CLI 报告为准。')}</p>
          {onStatus && <button type="button" className="secondary" onClick={() => { if (mayLeave()) { close(); onStatus(); } }}>{t('Service status, diagnostics and subscription', '服务状态、诊断与订阅')}</button>}
          <h3>{t('Host name, color and icon', '主机名称、颜色与图标')}</h3>
          <p>{t('Tell several hosts apart in the browser: the name is in the tab title, the sidebar and the login page, the color and icon make the tab icon and logo. Leave them empty for the defaults: the machine name, a color picked by the name, and the CC Remote logo. Also: ', '在浏览器里区分多台宿主：名称显示在标签页标题、侧栏和登录页，颜色和图标组成标签页图标和标志。留空使用默认值：机器名、按名称挑选的颜色，以及 CC Remote 标志。也可以用命令：')}<code>cc-remote service config --name NAME --color COLOR --icon ICON</code></p>
          {identity ? <>
            <label>{t('Name', '名称')}<input value={hostName} maxLength={40} placeholder={identity.defaultName} onChange={event => setHostName(event.target.value)} /></label>
            <ColorChoices value={hostColor} onChange={setHostColor} auto={{ label: t('Auto', '自动'), title: t('Automatic', '自动'), color: paletteHex(identity, identity.defaultColor) }} />
            <IconChoices value={hostIcon} onChange={setHostIcon} color={hostColor || paletteHex(identity, identity.defaultColor)} auto={{ label: t('Logo', '标志'), title: t('The CC Remote logo', 'CC Remote 标志'), color: hostColor || paletteHex(identity, identity.defaultColor) }} />
            <button className="primary" disabled={saving}>{saving ? t('Saving…', '保存中…') : t('Save', '保存')}</button>
          </> : <p>{t('Loading…', '加载中…')}</p>}
        </form>}
        {section === 'browser' && <div>
          <h3>{t('Language', '语言')}</h3>
          <p>{t('Saved in this browser only; by default the page follows the language configured on the host. Changing it reloads the page.', '只保存在此浏览器；默认跟随宿主配置的语言。切换后页面会重新加载。')}</p>
          <div className="field">{t('Interface language', '界面语言')}<Select label={t('Interface language', '界面语言')} value={langOverride()} options={[{ value: '', label: STANDALONE ? t(`Follow the browser (${LANG_NAMES[hostLang]})`, `跟随浏览器（${LANG_NAMES[hostLang]}）`) : t(`Follow host (${LANG_NAMES[hostLang]})`, `跟随宿主（${LANG_NAMES[hostLang]}）`) }, { value: 'zh', label: LANG_NAMES.zh }, { value: 'en', label: LANG_NAMES.en }]} onChange={value => { if (mayLeave()) setLang(value as Lang | ''); }} /></div>
          {onSendKey && <div className="field">{t('Send shortcut', '发送快捷键')}<Select label={t('Send shortcut', '发送快捷键')} value={sendKey || 'ctrl-enter'} options={[{ value: 'ctrl-enter', label: 'Ctrl / ⌘ + Enter' }, { value: 'enter', label: t('Enter sends · Shift + Enter adds a line', 'Enter 发送 · Shift + Enter 换行') }]} onChange={onSendKey} /></div>}
          <h3>{t('Notifications', '通知')}</h3>
          <p>{t('Saved in this browser only. They come while this page is open, also in a background tab or minimized; a phone may pause a page in the background, and then they wait until you open it. A notification is not shown for the session you are looking at. The tab title also counts the sessions waiting for you.', '只保存在此浏览器。页面打开时（包括后台标签页、最小化）才会收到；手机浏览器切到后台后可能暂停页面，通知会推迟到再次打开时。正在查看的会话不提醒。标签页标题也会显示等待你操作的会话数。')}</p>
          <NotifySettings />
        </div>}
        {notice && <p role="status">{notice}</p>}
        {error && <div className="error" role="alert">{error}<button type="button" className="secondary" disabled={saving} onClick={() => { if (!mayLeave()) return; void load(); if (nativeOpen || value) void loadClaude(); }}>{t('Reload', '重新加载')}</button></div>}
      </fieldset>
    </div>
  </div></div>;
}

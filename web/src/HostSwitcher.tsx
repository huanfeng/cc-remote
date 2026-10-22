import { useEffect, useRef, useState, type FormEvent } from 'react';
import { createPortal } from 'react-dom';
import { copyText } from './clipboard';
import { CloseIcon, EditIcon, LogoutIcon, PlusIcon } from './icons';
import { PALETTE, apiBase, blockedHttp, currentHost, findHost, forgetAllHosts, hostLink, loadHosts, normalizeHostUrl, randomColor, rememberHost, removeHost, reopen, onLan, resolveEntry, shownColor, shownIcon, shownName, switchHost, updateHost, type HostEntry, type HostVia } from './hosts';
import { lang, t } from './i18n';
import { iconURL } from './hostIcon';
import { ColorChoices, IconChoices } from './HostLook';
import { LangSwitch } from './LangSwitch';
import { SafetyNotice, useSafetyAck } from './SafetyNotice';

type Ping = { online: boolean; running?: number; waiting?: number };

/** Checks a host with its saved token: reachable, and how many sessions run or wait. */
async function ping(host: HostEntry, signal: AbortSignal): Promise<Ping> {
  try {
    const response = await fetch(`${host.id === currentHost?.id ? apiBase : host.url}/api/sessions`, { headers: { Authorization: `Bearer ${host.token}`, 'X-CC-Lang': lang }, cache: 'no-store', signal });
    if (!response.ok) return { online: response.status !== 401 && response.status < 500 };
    const items: { status?: string }[] = await response.json();
    return { online: true, running: items.filter(x => x.status === 'running').length, waiting: items.filter(x => x.status === 'waiting').length };
  } catch { return { online: false }; }
}

/** What a host runs to let this page in: it applies at once (cc-remote --allow-origin
 * does the same for a host started by hand). */
export const allowCommand = () => `cc-remote service config --allow-origin ${location.origin}`;

/** Why a host could not be reached from this page, in words the user can act on. A host
 * that does not allow this page fails the preflight: that looks like no answer at all. */
export function connectError(status?: number): string {
  if (status === 401) return t('The access token is not valid for this host', '访问令牌与这个宿主不符');
  if (status) return t(`The host answered with an error (${status})`, `宿主返回错误（${status}）`);
  return t(`Cannot reach the host. Check the address, that it is served over HTTPS (this page is ${location.protocol.replace(':', '')}), and that the host allows this page (on the host: ${allowCommand()})`, `无法连接宿主。请检查地址、宿主是否通过 HTTPS 提供（本页为 ${location.protocol.replace(':', '')}），以及宿主是否允许本页面（在宿主上运行：${allowCommand()}）`);
}

const VIA_LABEL = { direct: () => t('Direct', '直连'), text: () => t('Address URL', '文本地址') };

/** Adds a host (or signs in to the current one again) and opens it, or edits a host
 * (editing: its token stays when left empty; the name and colour this page shows for it
 * are saved without reaching the host). Besides its address, a host can be given
 * as an entry that answers with the address as plain text (a tunnel whose port
 * changes). */
export function HostLogin({ initialUrl = '', initialVia = 'direct', initialLan = '', error: shown = '', editing, onCancel }: { initialUrl?: string; initialVia?: HostVia; initialLan?: string; error?: string; editing?: HostEntry; onCancel?: () => void }) {
  const [url, setUrl] = useState(editing ? editing.entry || editing.url : initialUrl);
  const [via, setVia] = useState<HostVia>(editing ? editing.via || 'direct' : initialVia);
  const [token, setToken] = useState('');
  const [lan, setLan] = useState(editing ? editing.lan || '' : initialLan);
  const [alias, setAlias] = useState(editing?.alias || '');
  const [tint, setTint] = useState(editing?.tint || '');
  const [emblem, setEmblem] = useState(editing?.emblem || '');
  const [error, setError] = useState(shown);
  useEffect(() => setError(shown), [shown]); // the page reports a failed reconnect after mounting this
  const [busy, setBusy] = useState(false);
  const safety = useSafetyAck();
  const others = loadHosts().hosts.filter(x => x.id !== currentHost?.id);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!safety.ack) return;
    const entered = normalizeHostUrl(url);
    if (!entered) { setError(via === 'direct' ? t('Enter the host address, such as https://ccr.example.com', '请输入宿主地址，例如 https://ccr.example.com') : t('Enter the entry URL, such as https://example.com/ccr', '请输入入口地址，例如 https://example.com/ccr')); return; }
    // The entry keeps its query (a token for the address service, say); a bare one gets https.
    const entry = /^[a-z][a-z0-9+.-]*:\/\//i.test(url.trim()) ? url.trim() : `${location.protocol === 'http:' ? 'http' : 'https'}://${url.trim()}`;
    const lanUrl = lan.trim() ? normalizeHostUrl(lan) : '';
    if (lan.trim() && !lanUrl) { setError(t('Enter the home-network address, such as https://lan.example.com', '请输入内网地址，例如 https://lan.example.com')); return; }
    if (lanUrl && blockedHttp(lanUrl)) { setError(t('The home-network address must be https://: this page is served over HTTPS and cannot reach a plain http:// address. Point a domain name at the private address and get it a certificate.', '内网地址必须是 https://：本页面通过 HTTPS 提供，无法访问 http:// 的地址。可以让一个域名指向内网 IP，并为它申请证书。')); return; }
    // Checked when it is used: away from home it cannot answer now.
    const look = { alias: alias.trim() || undefined, tint: tint || undefined, emblem: emblem || undefined, lan: lanUrl || undefined };
    // Only the name, colour, icon or home-network address changed: nothing to check on the host, which may be offline.
    if (editing && !token.trim() && (via === 'direct' ? !editing.via && entered === editing.url : via === editing.via && entry === editing.entry)) {
      updateHost(editing.id, look); // reopens the page when it is the current host
      onCancel?.();
      return;
    }
    setBusy(true); setError('');
    try {
      let base = entered;
      if (via !== 'direct') {
        try { base = await resolveEntry(entry); } catch (err) { setError((err as Error).message); return; }
      }
      const taken = editing && findHost(base, via === 'direct' ? undefined : entry, editing.id);
      if (taken) { setError(t(`${shownName(taken)} is already on this page with that address`, `${shownName(taken)} 已经使用这个地址`)); return; }
      const key = token.trim() || editing?.token || '';
      const response = await fetch(`${base}/api/info`, { headers: { Authorization: `Bearer ${key}`, 'X-CC-Lang': lang }, cache: 'no-store' }).catch(() => undefined);
      if (!response?.ok) { setError((via !== 'direct' ? t(`Found ${base}. `, `解析到 ${base}。`) : '') + connectError(response?.status)); return; }
      const info: { name?: string; color?: string } = await response.json();
      const record = { url: base, token: key, name: info.name, color: info.color, entry: via === 'direct' ? undefined : entry, via: via === 'direct' ? undefined : via };
      if (editing) {
        updateHost(editing.id, { ...record, ...look }); // reopens the page when it is the current host
        onCancel?.();
        return;
      }
      reopen(rememberHost({ ...record, lan: look.lan }).id);
    } finally { setBusy(false); }
  }
  const hint = via === 'text' ? t('A URL that answers with the host address as plain text, such as https://home.example.com:45678. It must allow this page (CORS). When the host stops answering, the address is fetched again.', '一个以纯文本返回宿主地址（如 https://home.example.com:45678）的 URL，必须允许本页面读取（CORS）。宿主连不上时会重新获取地址。')
    : '';
  return <form className="login-card host-login" onSubmit={submit}>
    {editing ? <h2>{t(`Edit ${shownName(editing)}`, `编辑 ${shownName(editing)}`)}</h2> : onCancel ? <h2>{t('Add host', '添加宿主')}</h2> : <><div className="brand-symbol">↗</div><span className="eyebrow">YOUR WORKSPACE, ANYWHERE</span><h1>CC Remote</h1></>}
    <p>{editing ? t('A new address or token is checked before it is saved.', '修改地址或令牌时，保存前会用新的设置连接一次。') : t('Connect to a cc-remote host. This page keeps a list of hosts and their tokens on this device.', '连接一个 cc-remote 宿主。此页面在本设备上保存宿主列表和令牌。')}</p>
    <div className="host-via options" role="radiogroup" aria-label={t('How to reach the host', '连接方式')}>{(['direct', 'text'] as const).map(v => <button type="button" key={v} role="radio" aria-checked={via === v} className={via === v ? 'chosen' : ''} onClick={() => setVia(v)}>{VIA_LABEL[v]()}</button>)}</div>
    <label>{via === 'direct' ? t('Host address', '宿主地址') : t('Address URL', '地址获取 URL')}<input value={url} onChange={event => setUrl(event.target.value)} placeholder={via === 'direct' ? 'https://ccr.example.com' : 'https://example.com/ccr.txt'} autoComplete="url" inputMode="url" required /></label>
    {hint && <small className="host-via-hint">{hint}</small>}
    <label>{t('Host access token', '宿主访问令牌')}<input type="password" value={token} onChange={event => setToken(event.target.value)} placeholder={editing ? t('Leave empty to keep the current token', '留空则保留原令牌') : t('The access token of that host', '该宿主的访问令牌')} autoComplete={editing ? 'new-password' : 'current-password'} required={!editing} /></label>
    <label>{t('Home-network address (optional)', '内网地址（可选）')}<input value={lan} onChange={event => setLan(event.target.value)} placeholder="https://lan.example.com" autoComplete="off" inputMode="url" /></label>
    <small className="host-via-hint">{t('An https name pointing at this host on your home network. While it answers, this page uses it; away from home it goes back to the address above by itself.', '指向内网中这台宿主的 https 域名。能连上时优先使用它，离开内网后自动切回上面的地址。')}</small>
    {editing && <>
      <label>{t('Name on this page', '在此页面显示的名称')}<input value={alias} maxLength={40} onChange={event => setAlias(event.target.value)} placeholder={editing.name || t('The host\'s own name', '宿主自己的名称')} /></label>
      <ColorChoices value={tint} onChange={setTint} auto={{ label: t('Host', '宿主'), title: t('The host\'s own color', '宿主自己的颜色'), color: editing.color || PALETTE[0] }} random={() => randomColor(loadHosts().hosts.filter(x => x.id !== editing.id).map(shownColor), tint || editing.color)} />
      <IconChoices value={emblem} onChange={setEmblem} color={tint || editing.color || PALETTE[0]} auto={{ label: t('Host', '宿主'), title: t('The host\'s own icon', '宿主自己的图标'), color: tint || editing.color || PALETTE[0], icon: editing.icon }} withLogo />
      <small className="host-via-hint">{t('Saved on this device only, over the name, color and icon set on the host; leave them empty or at Host for the host\'s own.', '只保存在此设备，覆盖宿主上设置的名称、颜色和图标；名称留空、颜色和图标选「宿主」则使用宿主自己的设置。')}</small>
    </>}
    {!editing && safety.show && <SafetyNotice checked={safety.ack} onChange={safety.setAck} />}
    {error && <div className="error" role="alert">{error}</div>}
    <button className="primary" disabled={busy || !safety.ack}>{busy ? t('Connecting…', '连接中…') : editing ? t('Save', '保存') : t('Connect to host', '连接宿主')} <span>→</span></button>
    {onCancel && <button type="button" className="secondary host-login-cancel" onClick={onCancel}>{t('Cancel', '取消')}</button>}
    {!onCancel && !editing && others.length > 0 && <div className="host-login-others"><span className="eyebrow">{t('OTHER HOSTS', '其他宿主')}</span>{others.map(x => <button type="button" key={x.id} onClick={() => switchHost(x.id)}><img className="host-swatch" src={iconURL(shownColor(x), shownIcon(x))} alt="" />{shownName(x)}<small>{x.entry || x.url}</small></button>)}</div>}
    {!editing && <AllowHint />}
  </form>;
}

/** The standalone page while it is not connected: what the page is, with a button to
 * add a host, or the current host's connection state. It never opens the add form by
 * itself: on an unsteady network the page would sit on a form asking for the token again.
 * retrying: the page tries again by itself (no answer, not a refused token). */
export function StandaloneHome({ error, retrying, onRetry }: { error: string; retrying: boolean; onRetry: () => void }) {
  const [form, setForm] = useState<'' | 'add' | 'edit'>('');
  const safety = useSafetyAck();
  const host = currentHost;
  if (form === 'add') return <HostLogin onCancel={() => setForm('')} />;
  if (form === 'edit' && host) return <HostLogin editing={host} onCancel={() => setForm('')} />;
  const others = loadHosts().hosts.filter(x => x.id !== host?.id);
  const add = <button type="button" className={host ? 'secondary host-home-add' : 'primary'} disabled={!host && !safety.ack} onClick={() => setForm('add')}>{host ? <><PlusIcon size={15} />{t('Add host', '添加宿主')}</> : <>{t('Add a host', '添加宿主')} <span>→</span></>}</button>;
  if (!host) return <div className="login-card host-home">
    <LangSwitch />
    <div className="brand-symbol">↗</div><span className="eyebrow">YOUR WORKSPACE, ANYWHERE</span><h1>CC Remote</h1>
    <p>{t('Use Claude Code on your computer from this browser: follow sessions, send messages, answer approvals and questions. This page is only the frontend; it keeps the hosts you add and their tokens on this device.', '在这个浏览器里使用你电脑上的 Claude Code：查看会话、发送消息、回答审批和提问。此页面只是前端，你添加的宿主和令牌只保存在本设备。')}</p>
    <ol className="host-home-steps">
      <li>{t('Run a cc-remote host on the computer with Claude Code.', '在装有 Claude Code 的电脑上运行 cc-remote 宿主。')}</li>
      <li>{t('Let this page in on the host (below).', '在宿主上允许本页面访问（见下方）。')}</li>
      <li>{t('Add the host here with its address and access token.', '在这里添加宿主，填写地址和访问令牌。')}</li>
    </ol>
    {safety.show && <SafetyNotice checked={safety.ack} onChange={safety.setAck} />}
    {add}
    <AllowHint />
  </div>;
  return <div className="login-card host-home">
    <LangSwitch />
    <span className="eyebrow">CC REMOTE</span>
    <div className="host-home-current"><img className="host-swatch" src={iconURL(shownColor(host), shownIcon(host))} alt="" /><div><strong>{shownName(host)}</strong><small>{host.entry || host.url}</small></div></div>
    {error ? <div className="error" role="alert">{error}</div> : <p className="host-home-state"><span className="pulse" />{t('Connecting…', '正在连接…')}</p>}
    {error && retrying && <small className="host-home-note">{t('Trying again every 10 seconds; the page opens by itself once the host answers.', '每 10 秒自动重试，宿主恢复后自动进入。')}</small>}
    <div className="host-home-actions">
      <button type="button" className="primary" onClick={onRetry}>{t('Try again now', '立即重试')}</button>
      <button type="button" className="secondary" onClick={() => setForm('edit')}><EditIcon size={15} />{t('Edit host', '编辑宿主')}</button>
    </div>
    {others.length > 0 && <div className="host-login-others"><span className="eyebrow">{t('OTHER HOSTS', '其他宿主')}</span>{others.map(x => <button type="button" key={x.id} onClick={() => switchHost(x.id)}><img className="host-swatch" src={iconURL(shownColor(x), shownIcon(x))} alt="" />{shownName(x)}<small>{x.entry || x.url}</small></button>)}</div>}
    {add}
  </div>;
}

/** How to let this page into a host: the command, copied on click. */
function AllowHint() {
  const [copied, setCopied] = useState(false);
  const copy = async () => { try { await copyText(allowCommand()); setCopied(true); window.setTimeout(() => setCopied(false), 1600); } catch {} };
  return <small className="allow-hint">{t('Each host must allow this page. On the host, run (applies at once):', '每个宿主需允许本页面，在宿主上运行（立即生效）：')}
    <button type="button" className="allow-command" title={t('Copy', '复制')} onClick={() => void copy()}><code>{allowCommand()}</code><span>{copied ? t('Copied', '已复制') : t('Copy', '复制')}</span></button>
    {t('It replaces the frontends the host allows: list them all, separated by commas. A host started by hand takes --allow-origin instead.', '它会替换宿主允许的前端列表，有多个时用逗号一起列出。手动前台运行的宿主改用 --allow-origin 启动参数。')}</small>;
}

/** The sidebar's host name, opening the host list: switch (each row also links to the
 * host, to open it in a tab of its own), add, edit or forget hosts. */
export function HostSwitcher({ name }: { name: string }) {
  const [open, setOpen] = useState(false);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<HostEntry>();
  const [pings, setPings] = useState<Record<string, Ping>>({});
  const box = useRef<HTMLDivElement>(null);
  const hosts = loadHosts().hosts;
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    for (const host of hosts) void ping(host, controller.signal).then(result => { if (!controller.signal.aborted) setPings(old => ({ ...old, [host.id]: result })); });
    const away = (event: MouseEvent | TouchEvent) => { if (!box.current?.contains(event.target as Node)) setOpen(false); };
    const key = (event: KeyboardEvent) => { if (event.key === 'Escape') setOpen(false); };
    document.addEventListener('mousedown', away); document.addEventListener('touchstart', away); document.addEventListener('keydown', key);
    return () => { controller.abort(); document.removeEventListener('mousedown', away); document.removeEventListener('touchstart', away); document.removeEventListener('keydown', key); };
  }, [open]);
  const forget = (host: HostEntry) => { if (window.confirm(t(`Remove ${shownName(host)} from this page? Its token and drafts saved here are deleted.`, `从此页面移除 ${shownName(host)}？此处保存的令牌和草稿会一并删除。`))) { removeHost(host.id); setOpen(false); } };
  const forgetAll = () => { if (window.confirm(t(`Sign out of all ${hosts.length} hosts and clear this page? Every host, token, draft and tab saved here is deleted; adding a host again needs its token.`, `退出全部 ${hosts.length} 个宿主并清除此页面的数据？此处保存的所有宿主、令牌、草稿和标签页记录都会删除，无法恢复；再次添加宿主需要重新输入令牌。`))) forgetAllHosts(); };
  // A plain click switches this tab; a modified one (or a long press) opens a tab of its own.
  const pick = (event: MouseEvent, host: HostEntry) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    if (host.id === currentHost?.id) setOpen(false); else switchHost(host.id);
  };
  return <div className="host-switcher" ref={box}>
    <button type="button" className="host-switch" aria-haspopup="menu" aria-expanded={open} title={t(`Switch host (${hosts.length})`, `切换宿主（共 ${hosts.length} 个）`)} onClick={() => setOpen(old => !old)}><span>{name}</span><span className="host-switch-caret" aria-hidden="true">▾</span></button>
    {open && <div className="host-menu popover-menu" role="menu">
      {hosts.map(host => { const p = pings[host.id]; return <div key={host.id} className={`host-row${host.id === currentHost?.id ? ' current' : ''}`}>
        <a role="menuitem" href={hostLink(host.id)} onClick={event => pick(event.nativeEvent, host)}>
          <img className="host-swatch" src={iconURL(shownColor(host), shownIcon(host))} alt="" />
          <span className="host-text"><strong>{shownName(host)}</strong><small title={[host.entry ? `${host.entry} → ${host.url}` : '', host.lan ? t(`Home network: ${host.lan}`, `内网：${host.lan}`) : ''].filter(Boolean).join('\n') || undefined}>{host.id === currentHost?.id && onLan ? `${host.lan} · ${t('Home network', '内网')}` : <>{host.url}{host.via && ` · ${VIA_LABEL[host.via]()}`}</>}</small></span>
          <span className="host-state">{!p ? '…' : !p.online ? t('Offline', '离线') : p.waiting ? t(`${p.waiting} waiting`, `${p.waiting} 待操作`) : p.running ? t(`${p.running} running`, `${p.running} 执行中`) : <span className="dot idle" />}</span>
        </a>
        <button type="button" className="icon-button" aria-label={t(`Edit ${shownName(host)}`, `编辑 ${shownName(host)}`)} title={t('Edit name, color, icon, address or token', '编辑名称、颜色、图标、地址或令牌')} onClick={() => { setOpen(false); setEditing(host); }}><EditIcon size={15} /></button>
        <button type="button" className="icon-button" aria-label={t(`Remove ${shownName(host)}`, `移除 ${shownName(host)}`)} title={t('Remove from this page', '从此页面移除')} onClick={() => forget(host)}><CloseIcon size={15} /></button>
      </div>; })}
      <button type="button" role="menuitem" className="host-add" onClick={() => { setOpen(false); setAdding(true); }}><PlusIcon size={14} />{t('Add host', '添加宿主')}</button>
      <button type="button" role="menuitem" className="host-add host-clear" onClick={forgetAll}><LogoutIcon size={14} />{t('Sign out of all hosts and clear…', '退出并清除所有宿主…')}</button>
    </div>}
    {/* On the body: the phone sidebar's transform would otherwise hold the fixed backdrop. */}
    {adding && createPortal(<div className="modal-backdrop" onClick={event => { if (event.target === event.currentTarget) setAdding(false); }}><HostLogin onCancel={() => setAdding(false)} /></div>, document.body)}
    {editing && createPortal(<div className="modal-backdrop" onClick={event => { if (event.target === event.currentTarget) setEditing(undefined); }}><HostLogin key={editing.id} editing={editing} onCancel={() => setEditing(undefined)} /></div>, document.body)}
  </div>;
}

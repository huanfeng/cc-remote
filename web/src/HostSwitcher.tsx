import { useEffect, useRef, useState, type FormEvent } from 'react';
import { createPortal } from 'react-dom';
import { copyText } from './clipboard';
import { CloseIcon, EditIcon, PlusIcon } from './icons';
import { currentHost, findHost, loadHosts, normalizeHostUrl, rememberHost, removeHost, reopen, resolveEntry, switchHost, updateHost, type HostEntry, type HostVia } from './hosts';
import { lang, t } from './i18n';

type Ping = { online: boolean; running?: number; waiting?: number };

/** Checks a host with its saved token: reachable, and how many sessions run or wait. */
async function ping(host: HostEntry, signal: AbortSignal): Promise<Ping> {
  try {
    const response = await fetch(`${host.url}/api/sessions`, { headers: { Authorization: `Bearer ${host.token}`, 'X-CC-Lang': lang }, cache: 'no-store', signal });
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

const VIA_LABEL = { direct: () => t('Direct', '直连'), redirect: () => t('Redirect', '302 跳转'), text: () => t('Address URL', '文本地址') };

/** Adds a host (or signs in to the current one again) and opens it, or edits a host
 * (editing: its token stays when left empty). Besides its address, a host can be given
 * as an entry that leads to it: a URL redirecting there, or one answering with the
 * address as plain text (a tunnel whose port changes). */
export function HostLogin({ initialUrl = '', initialVia = 'direct', error: shown = '', editing, onCancel }: { initialUrl?: string; initialVia?: HostVia; error?: string; editing?: HostEntry; onCancel?: () => void }) {
  const [url, setUrl] = useState(editing ? editing.entry || editing.url : initialUrl);
  const [via, setVia] = useState<HostVia>(editing ? editing.via || 'direct' : initialVia);
  const [token, setToken] = useState('');
  const [error, setError] = useState(shown);
  useEffect(() => setError(shown), [shown]); // the page reports a failed reconnect after mounting this
  const [busy, setBusy] = useState(false);
  const others = loadHosts().hosts.filter(x => x.id !== currentHost?.id);
  async function submit(event: FormEvent) {
    event.preventDefault();
    const entered = normalizeHostUrl(url);
    if (!entered) { setError(via === 'direct' ? t('Enter the host address, such as https://ccr.example.com', '请输入宿主地址，例如 https://ccr.example.com') : t('Enter the entry URL, such as https://example.com/ccr', '请输入入口地址，例如 https://example.com/ccr')); return; }
    // The entry keeps its query (a token for the address service, say); a bare one gets https.
    const entry = /^[a-z][a-z0-9+.-]*:\/\//i.test(url.trim()) ? url.trim() : `${location.protocol === 'http:' ? 'http' : 'https'}://${url.trim()}`;
    setBusy(true); setError('');
    try {
      let base = entered;
      if (via !== 'direct') {
        try { base = await resolveEntry(entry, via); } catch (err) { setError((err as Error).message); return; }
      }
      const taken = editing && findHost(base, via === 'direct' ? undefined : entry, editing.id);
      if (taken) { setError(t(`${taken.name || taken.url} is already on this page with that address`, `${taken.name || taken.url} 已经使用这个地址`)); return; }
      const key = token.trim() || editing?.token || '';
      const response = await fetch(`${base}/api/info`, { headers: { Authorization: `Bearer ${key}`, 'X-CC-Lang': lang }, cache: 'no-store' }).catch(() => undefined);
      if (!response?.ok) { setError((via !== 'direct' ? t(`Found ${base}. `, `解析到 ${base}。`) : '') + connectError(response?.status)); return; }
      const info: { name?: string; color?: string } = await response.json();
      const record = { url: base, token: key, name: info.name, color: info.color, entry: via === 'direct' ? undefined : entry, via: via === 'direct' ? undefined : via };
      if (editing) {
        updateHost(editing.id, record); // reopens the page when it is the current host
        onCancel?.();
        return;
      }
      rememberHost(record);
      reopen();
    } finally { setBusy(false); }
  }
  const hint = via === 'redirect' ? t('A URL that redirects (302) to the host, such as the jump link of a tunnel. It and the host must allow this page (CORS).', '一个会 302 跳转到宿主的地址，例如内网穿透的跳转链接。它和宿主都必须允许本页面读取（CORS）。')
    : via === 'text' ? t('A URL that answers with the host address as plain text, such as https://home.example.com:45678. It must allow this page (CORS). When the host stops answering, the address is fetched again.', '一个以纯文本返回宿主地址（如 https://home.example.com:45678）的 URL，必须允许本页面读取（CORS）。宿主连不上时会重新获取地址。')
    : '';
  return <form className="login-card host-login" onSubmit={submit}>
    {editing ? <h2>{t(`Edit ${editing.name || 'host'}`, `编辑 ${editing.name || '宿主'}`)}</h2> : onCancel ? <h2>{t('Add host', '添加宿主')}</h2> : <><div className="brand-symbol">↗</div><span className="eyebrow">YOUR WORKSPACE, ANYWHERE</span><h1>CC Remote</h1></>}
    <p>{editing ? t('The new address is checked before it is saved. The name and colour are the host\'s own: change them in its settings.', '保存前会用新的设置连接一次。名称和颜色是宿主自己的设置，请在宿主的设置中修改。') : t('Connect to a cc-remote host. This page keeps a list of hosts and their tokens on this device.', '连接一个 cc-remote 宿主。此页面在本设备上保存宿主列表和令牌。')}</p>
    <div className="host-via options" role="radiogroup" aria-label={t('How to reach the host', '连接方式')}>{(['direct', 'redirect', 'text'] as const).map(v => <button type="button" key={v} role="radio" aria-checked={via === v} className={via === v ? 'chosen' : ''} onClick={() => setVia(v)}>{VIA_LABEL[v]()}</button>)}</div>
    <label>{via === 'direct' ? t('Host address', '宿主地址') : via === 'redirect' ? t('Redirect URL', '跳转地址') : t('Address URL', '地址获取 URL')}<input value={url} onChange={event => setUrl(event.target.value)} placeholder={via === 'direct' ? 'https://ccr.example.com' : via === 'redirect' ? 'https://example.com/ccr' : 'https://example.com/ccr.txt'} autoComplete="url" inputMode="url" required /></label>
    {hint && <small className="host-via-hint">{hint}</small>}
    <label>{t('Host access token', '宿主访问令牌')}<input type="password" value={token} onChange={event => setToken(event.target.value)} placeholder={editing ? t('Leave empty to keep the current token', '留空则保留原令牌') : t('The access token of that host', '该宿主的访问令牌')} autoComplete={editing ? 'new-password' : 'current-password'} required={!editing} /></label>
    {error && <div className="error" role="alert">{error}</div>}
    <button className="primary" disabled={busy}>{busy ? t('Connecting…', '连接中…') : editing ? t('Save', '保存') : t('Connect to host', '连接宿主')} <span>→</span></button>
    {onCancel && <button type="button" className="secondary host-login-cancel" onClick={onCancel}>{t('Cancel', '取消')}</button>}
    {!onCancel && !editing && others.length > 0 && <div className="host-login-others"><span className="eyebrow">{t('OTHER HOSTS', '其他宿主')}</span>{others.map(x => <button type="button" key={x.id} onClick={() => switchHost(x.id)}><span className="host-swatch" style={{ background: x.color || '#293c2e' }} />{x.name || x.url}<small>{x.entry || x.url}</small></button>)}</div>}
    {!editing && <AllowHint />}
  </form>;
}

/** How to let this page into a host: the command, copied on click. */
function AllowHint() {
  const [copied, setCopied] = useState(false);
  const copy = async () => { try { await copyText(allowCommand()); setCopied(true); window.setTimeout(() => setCopied(false), 1600); } catch {} };
  return <small className="allow-hint">{t('Each host must allow this page. On the host, run (applies at once):', '每个宿主需允许本页面，在宿主上运行（立即生效）：')}
    <button type="button" className="allow-command" title={t('Copy', '复制')} onClick={() => void copy()}><code>{allowCommand()}</code><span>{copied ? t('Copied', '已复制') : t('Copy', '复制')}</span></button>
    {t('It replaces the frontends the host allows: list them all, separated by commas. A host started by hand takes --allow-origin instead.', '它会替换宿主允许的前端列表，有多个时用逗号一起列出。手动前台运行的宿主改用 --allow-origin 启动参数。')}</small>;
}

/** The host list under the sidebar's host name: switch, add, edit or forget hosts. */
export function HostSwitcher() {
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
  const forget = (host: HostEntry) => { if (window.confirm(t(`Remove ${host.name || host.url} from this page? Its token and drafts saved here are deleted.`, `从此页面移除 ${host.name || host.url}？此处保存的令牌和草稿会一并删除。`))) { removeHost(host.id); setOpen(false); } };
  return <div className="host-switcher" ref={box}>
    <button type="button" className="host-switch" aria-haspopup="menu" aria-expanded={open} title={t('Switch host', '切换宿主')} onClick={() => setOpen(old => !old)}>{t(`${hosts.length} hosts`, `${hosts.length} 个宿主`)} ▾</button>
    {open && <div className="host-menu popover-menu" role="menu">
      {hosts.map(host => { const p = pings[host.id]; return <div key={host.id} className={`host-row${host.id === currentHost?.id ? ' current' : ''}`}>
        <button type="button" role="menuitem" onClick={() => host.id === currentHost?.id ? setOpen(false) : switchHost(host.id)}>
          <span className="host-swatch" style={{ background: host.color || '#293c2e' }} />
          <span className="host-text"><strong>{host.name || host.url}</strong><small title={host.entry ? `${host.entry} → ${host.url}` : undefined}>{host.url}{host.via && ` · ${VIA_LABEL[host.via]()}`}</small></span>
          <span className="host-state">{!p ? '…' : !p.online ? t('Offline', '离线') : p.waiting ? t(`${p.waiting} waiting`, `${p.waiting} 待操作`) : p.running ? t(`${p.running} running`, `${p.running} 执行中`) : <span className="dot idle" />}</span>
        </button>
        <button type="button" className="icon-button" aria-label={t(`Edit ${host.name || host.url}`, `编辑 ${host.name || host.url}`)} title={t('Edit address or token', '编辑地址或令牌')} onClick={() => { setOpen(false); setEditing(host); }}><EditIcon size={12} /></button>
        <button type="button" className="icon-button" aria-label={t(`Remove ${host.name || host.url}`, `移除 ${host.name || host.url}`)} title={t('Remove from this page', '从此页面移除')} onClick={() => forget(host)}><CloseIcon size={12} /></button>
      </div>; })}
      <button type="button" role="menuitem" className="host-add" onClick={() => { setOpen(false); setAdding(true); }}><PlusIcon size={14} />{t('Add host', '添加宿主')}</button>
    </div>}
    {/* On the body: the phone sidebar's transform would otherwise hold the fixed backdrop. */}
    {adding && createPortal(<div className="modal-backdrop" onClick={event => { if (event.target === event.currentTarget) setAdding(false); }}><HostLogin onCancel={() => setAdding(false)} /></div>, document.body)}
    {editing && createPortal(<div className="modal-backdrop" onClick={event => { if (event.target === event.currentTarget) setEditing(undefined); }}><HostLogin key={editing.id} editing={editing} onCancel={() => setEditing(undefined)} /></div>, document.body)}
  </div>;
}

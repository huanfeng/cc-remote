import { useEffect, useRef, useState, type FormEvent } from 'react';
import { createPortal } from 'react-dom';
import { CloseIcon, PlusIcon } from './icons';
import { currentHost, loadHosts, normalizeHostUrl, rememberHost, removeHost, reopen, switchHost, type HostEntry } from './hosts';
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

/** Why a host could not be reached from this page, in words the user can act on. A host
 * that does not allow this page fails the preflight: that looks like no answer at all. */
export function connectError(status?: number): string {
  if (status === 401) return t('The access token is not valid for this host', '访问令牌与这个宿主不符');
  if (status) return t(`The host answered with an error (${status})`, `宿主返回错误（${status}）`);
  return t(`Cannot reach the host. Check the address, that it is served over HTTPS (this page is ${location.protocol.replace(':', '')}), and that the host allows this page: --allow-origin ${location.origin}`, `无法连接宿主。请检查地址、宿主是否通过 HTTPS 提供（本页为 ${location.protocol.replace(':', '')}），以及宿主是否允许本页面：--allow-origin ${location.origin}`);
}

/** Adds a host (or signs in to the current one again) and opens it. */
export function HostLogin({ initialUrl = '', error: shown = '', onCancel }: { initialUrl?: string; error?: string; onCancel?: () => void }) {
  const [url, setUrl] = useState(initialUrl);
  const [token, setToken] = useState('');
  const [error, setError] = useState(shown);
  useEffect(() => setError(shown), [shown]); // the page reports a failed reconnect after mounting this
  const [busy, setBusy] = useState(false);
  const others = loadHosts().hosts.filter(x => x.id !== currentHost?.id);
  async function submit(event: FormEvent) {
    event.preventDefault();
    const base = normalizeHostUrl(url);
    if (!base) { setError(t('Enter the host address, such as https://ccr.example.com', '请输入宿主地址，例如 https://ccr.example.com')); return; }
    setBusy(true); setError('');
    try {
      const response = await fetch(`${base}/api/info`, { headers: { Authorization: `Bearer ${token.trim()}`, 'X-CC-Lang': lang }, cache: 'no-store' }).catch(() => undefined);
      if (!response?.ok) { setError(connectError(response?.status)); return; }
      const info: { name?: string; color?: string } = await response.json();
      rememberHost({ url: base, token: token.trim(), name: info.name, color: info.color });
      reopen();
    } finally { setBusy(false); }
  }
  return <form className="login-card host-login" onSubmit={submit}>
    {onCancel ? <h2>{t('Add host', '添加宿主')}</h2> : <><div className="brand-symbol">↗</div><span className="eyebrow">YOUR WORKSPACE, ANYWHERE</span><h1>CC Remote</h1></>}
    <p>{t('Connect to a cc-remote host. This page keeps a list of hosts and their tokens on this device.', '连接一个 cc-remote 宿主。此页面在本设备上保存宿主列表和令牌。')}</p>
    <label>{t('Host address', '宿主地址')}<input value={url} onChange={event => setUrl(event.target.value)} placeholder="https://ccr.example.com" autoComplete="url" inputMode="url" required /></label>
    <label>{t('Host access token', '宿主访问令牌')}<input type="password" value={token} onChange={event => setToken(event.target.value)} placeholder={t('The access token of that host', '该宿主的访问令牌')} autoComplete="current-password" required /></label>
    {error && <div className="error" role="alert">{error}</div>}
    <button className="primary" disabled={busy}>{busy ? t('Connecting…', '连接中…') : t('Connect to host', '连接宿主')} <span>→</span></button>
    {onCancel && <button type="button" className="secondary host-login-cancel" onClick={onCancel}>{t('Cancel', '取消')}</button>}
    {!onCancel && others.length > 0 && <div className="host-login-others"><span className="eyebrow">{t('OTHER HOSTS', '其他宿主')}</span>{others.map(x => <button type="button" key={x.id} onClick={() => switchHost(x.id)}><span className="host-swatch" style={{ background: x.color || '#293c2e' }} />{x.name || x.url}<small>{x.url}</small></button>)}</div>}
    <small>{t(`Each host must allow this page: cc-remote --allow-origin ${location.origin}`, `每个宿主需允许本页面：cc-remote --allow-origin ${location.origin}`)}</small>
  </form>;
}

/** The host list under the sidebar's host name: switch, add or forget hosts. */
export function HostSwitcher() {
  const [open, setOpen] = useState(false);
  const [adding, setAdding] = useState(false);
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
          <span className="host-text"><strong>{host.name || host.url}</strong><small>{host.url}</small></span>
          <span className="host-state">{!p ? '…' : !p.online ? t('Offline', '离线') : p.waiting ? t(`${p.waiting} waiting`, `${p.waiting} 待操作`) : p.running ? t(`${p.running} running`, `${p.running} 执行中`) : <span className="dot idle" />}</span>
        </button>
        <button type="button" className="icon-button" aria-label={t(`Remove ${host.name || host.url}`, `移除 ${host.name || host.url}`)} title={t('Remove from this page', '从此页面移除')} onClick={() => forget(host)}><CloseIcon size={12} /></button>
      </div>; })}
      <button type="button" role="menuitem" className="host-add" onClick={() => { setOpen(false); setAdding(true); }}><PlusIcon size={14} />{t('Add host', '添加宿主')}</button>
    </div>}
    {/* On the body: the phone sidebar's transform would otherwise hold the fixed backdrop. */}
    {adding && createPortal(<div className="modal-backdrop" onClick={event => { if (event.target === event.currentTarget) setAdding(false); }}><HostLogin onCancel={() => setAdding(false)} /></div>, document.body)}
  </div>;
}

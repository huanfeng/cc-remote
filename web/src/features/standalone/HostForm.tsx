// Adds a host and opens it, or edits one. Editing keeps the token when it is
// left empty, and the name, colour and icon this page shows for the host are
// saved without reaching the host. Besides its address, a host can be given as
// an entry URL that answers with the address as plain text (a tunnel whose
// port changes). A new address or token is checked against /api/info first.
import { useState, type FormEvent, type ReactNode } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Spinner } from '@/components/ui/spinner';
import { cn } from '@/components/lib/utils';
import {
  PALETTE, blockedHttp, checkHost, connectError, findHost, loadHosts, normalizeHostUrl, randomColor, rememberHost, reopen,
  resolveEntry, shownColor, shownName, updateHost, type HostEntry, type HostVia,
} from '@/lib/hosts';
import { t } from '@/lib/i18n';
import { SurfaceShell, TOUCH } from '@/features/dialogs/SurfaceShell';
import { ErrorText } from '@/features/dialogs/common';
import { SafetyNotice, useSafetyAck } from '@/features/auth/SafetyNotice';
import { AllowHint, ColorChoices, IconChoices } from './parts';
import { DESKTOP } from '@/platform/desktop';

const VIA_LABEL: Record<HostVia, () => string> = { direct: () => t('Direct', '直连'), text: () => t('Address URL', '文本地址') };

export function HostFormDialog({ open, onOpenChange, editing, mobile }: { open: boolean; onOpenChange: (open: boolean) => void; editing?: HostEntry; mobile: boolean }) {
  return (
    <SurfaceShell
      open={open}
      onOpenChange={onOpenChange}
      mobile={mobile}
      title={editing ? t(`Edit ${shownName(editing)}`, `编辑 ${shownName(editing)}`) : t('Add host', '添加宿主')}
      description={editing ? t('A new address or token is checked before it is saved.', '修改地址或令牌时，保存前会用新的设置连接一次。') : t('This page keeps the hosts you add and their tokens on this device.', '此页面在本设备上保存你添加的宿主和令牌。')}
      className="sm:max-w-xl"
      bodyClassName={cn(mobile && TOUCH)}
    >
      {open && <HostForm key={editing?.id ?? 'new'} editing={editing} mobile={mobile} onDone={() => onOpenChange(false)} />}
    </SurfaceShell>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1.5">
      <span className="text-ui-sm font-medium text-foreground-subtle">{label}</span>
      {children}
      {hint && <span className="text-ui-xs leading-5 text-foreground-subtlest">{hint}</span>}
    </label>
  );
}

function HostForm({ editing, mobile, onDone }: { editing?: HostEntry; mobile: boolean; onDone: () => void }) {
  const [url, setUrl] = useState(editing ? editing.entry || editing.url : '');
  const [via, setVia] = useState<HostVia>(editing?.via || 'direct');
  const [token, setToken] = useState('');
  const [lan, setLan] = useState(editing?.lan || '');
  const [alias, setAlias] = useState(editing?.alias || '');
  const [tint, setTint] = useState(editing?.tint || '');
  const [emblem, setEmblem] = useState(editing?.emblem || '');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const safety = useSafetyAck();
  const input = cn(mobile && 'h-10 text-mobile-input-safe');

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy || (!editing && !safety.ack)) return;
    const entered = normalizeHostUrl(url);
    if (!entered) {
      setError(via === 'direct' ? t('Enter the host address, such as https://ccr.example.com', '请输入宿主地址，例如 https://ccr.example.com') : t('Enter the entry URL, such as https://example.com/ccr', '请输入入口地址，例如 https://example.com/ccr'));
      return;
    }
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
      onDone();
      return;
    }
    setBusy(true); setError('');
    try {
      let base = entered;
      if (via !== 'direct') {
        try { base = await resolveEntry(entry); } catch (err) { setError((err as Error).message); return; }
      } else if (blockedHttp(base)) {
        setError(t('This page is served over HTTPS and cannot reach a plain http:// host. Serve the host over HTTPS.', '本页面通过 HTTPS 提供，无法访问 http:// 的宿主。请让宿主通过 HTTPS 提供。'));
        return;
      }
      const taken = findHost(base, via === 'direct' ? undefined : entry, editing?.id);
      if (taken && editing) { setError(t(`${shownName(taken)} is already on this page with that address`, `${shownName(taken)} 已经使用这个地址`)); return; }
      const key = token.trim() || editing?.token || '';
      const reply = await checkHost(base, key);
      if (!reply.ok) { setError((via !== 'direct' ? t(`Found ${base}. `, `解析到 ${base}。`) : '') + connectError(reply.status)); return; }
      const record = { url: base, token: key, name: reply.name, color: reply.color, icon: reply.icon, entry: via === 'direct' ? undefined : entry, via: via === 'direct' ? undefined : via };
      if (editing) {
        updateHost(editing.id, { ...record, ...look }); // reopens the page when it is the current host
        onDone();
        return;
      }
      reopen(rememberHost({ ...record, lan: look.lan }).id);
    } finally {
      setBusy(false);
    }
  }

  const ownColor = editing?.color || PALETTE[0];
  return (
    <form onSubmit={e => void submit(e)} className="flex flex-col gap-4">
      <div role="radiogroup" aria-label={t('How to reach the host', '连接方式')} className="flex w-fit items-center rounded-lg bg-surface p-0.5">
        {(['direct', 'text'] as const).map(v => (
          <button key={v} type="button" role="radio" aria-checked={via === v} onClick={() => setVia(v)}
            className={cn('h-7 rounded-md px-3 text-ui-sm font-medium transition-colors max-md:h-9', via === v ? 'bg-card text-foreground shadow-xs' : 'text-foreground-subtle hover:text-foreground')}>
            {VIA_LABEL[v]()}
          </button>
        ))}
      </div>

      <Field
        label={via === 'direct' ? t('Host address', '宿主地址') : t('Address URL', '地址获取 URL')}
        hint={via === 'text' ? t('A URL that answers with the host address as plain text, such as https://home.example.com:45678. It must allow this page (CORS). When the host stops answering, the address is fetched again.', '一个以纯文本返回宿主地址（如 https://home.example.com:45678）的 URL，必须允许本页面读取（CORS）。宿主连不上时会重新获取地址。') : undefined}
      >
        <Input size="lg" value={url} onChange={e => setUrl(e.target.value)} placeholder={via === 'direct' ? 'https://ccr.example.com' : 'https://example.com/ccr.txt'}
          autoComplete="url" inputMode="url" required autoFocus={!mobile && !editing} className={input} />
      </Field>

      <Field label={t('Host access token', '宿主访问令牌')}>
        <Input size="lg" type="password" value={token} onChange={e => setToken(e.target.value)}
          placeholder={editing ? t('Leave empty to keep the current token', '留空则保留原令牌') : t('The access token of that host', '该宿主的访问令牌')}
          autoComplete={editing ? 'new-password' : 'current-password'} required={!editing} className={input} />
      </Field>

      <Field label={t('Home-network address (optional)', '内网地址（可选）')}
        hint={t('An https name pointing at this host on your home network. While it answers, this page uses it; away from home it goes back to the address above by itself.', '指向内网中这台宿主的 https 域名。能连上时优先使用它，离开内网后自动切回上面的地址。')}>
        <Input size="lg" value={lan} onChange={e => setLan(e.target.value)} placeholder="https://lan.example.com" autoComplete="off" inputMode="url" className={input} />
      </Field>

      {editing && (
        <div className="flex flex-col gap-4 rounded-xl border border-border/70 p-4">
          <Field label={t('Name on this page', '在此页面显示的名称')}>
            <Input size="lg" value={alias} maxLength={40} onChange={e => setAlias(e.target.value)} placeholder={editing.name || t("The host's own name", '宿主自己的名称')} className={input} />
          </Field>
          <div className="flex flex-col gap-1.5">
            <span className="text-ui-sm font-medium text-foreground-subtle">{t('Colour', '颜色')}</span>
            <ColorChoices value={tint} onChange={setTint} auto={ownColor}
              random={() => randomColor(loadHosts().hosts.filter(x => x.id !== editing.id).map(shownColor), tint || editing.color)} />
          </div>
          <div className="flex flex-col gap-1.5">
            <span className="text-ui-sm font-medium text-foreground-subtle">{t('Icon', '图标')}</span>
            <IconChoices value={emblem} onChange={setEmblem} color={tint || ownColor} auto={editing.icon || ''} mobile={mobile} />
          </div>
          <p className="text-ui-xs leading-5 text-foreground-subtlest">
            {t("Saved on this device only, over the name, colour and icon set on the host; leave them empty or at Host for the host's own.", '只保存在此设备，覆盖宿主上设置的名称、颜色和图标；名称留空、颜色和图标选「宿主」则使用宿主自己的设置。')}
          </p>
        </div>
      )}

      {!editing && safety.show && <SafetyNotice checked={safety.ack} onChange={safety.setAck} />}
      <ErrorText>{error}</ErrorText>

      <div className="flex flex-wrap items-center justify-end gap-2">
        <Button type="button" variant="ghost" size="lg" onClick={onDone} disabled={busy}>{t('Cancel', '取消')}</Button>
        <Button type="submit" size="lg" disabled={busy || (!editing && !safety.ack)}>
          {busy && <Spinner className="size-3.5" />}
          {busy ? t('Connecting…', '连接中…') : editing ? t('Save', '保存') : t('Connect to host', '连接宿主')}
        </Button>
      </div>
      {!editing && !DESKTOP && <AllowHint />}
    </form>
  );
}

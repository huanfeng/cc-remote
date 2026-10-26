import { useEffect, useState } from 'react';
import type { Api } from './ModelPicker';
import type { Provider } from './providers';
import { defaultProfile, profileBody, profilesFor, supportsProfileCatalog, type ProfileCatalog } from './profiles';
import { Select } from './Select';

export function SupplierBadge({ session }: { session: { profileId?: string; profileName?: string } }) {
  const name = session.profileName || session.profileId || t('Native host configuration', '宿主原生配置');
  return <span className="chip supplier-chip" title={t('Supplier: ', '供应商：') + name}>{t('Supplier: ', '供应商：')}{name}</span>;
}
import { t } from './i18n';

function useProfiles(api: Api) {
  const [loaded, setLoaded] = useState<{ api: Api; catalog?: ProfileCatalog; error?: string; unsupported?: boolean }>();
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let active = true;
    setLoaded({ api });
    api<ProfileCatalog>('/api/profiles').then(catalog => { if (active) setLoaded(supportsProfileCatalog(catalog) ? { api, catalog } : { api, unsupported: true }); }).catch(err => {
      if (active) setLoaded((err as { status?: number }).status === 404 ? { api, unsupported: true } : { api, error: (err as Error).message });
    });
    return () => { active = false; };
  }, [api, retry]);
  return { unsupported: loaded?.api === api && loaded.unsupported, catalog: loaded?.api === api ? loaded.catalog : undefined, error: loaded?.api === api ? loaded.error || '' : '', retry: () => setRetry(value => value + 1) };
}

function defaultLabel(catalog: ProfileCatalog, app: Provider) {
  const profile = defaultProfile(catalog, app);
  return profile ? t(`Default · ${profile.name}`, `默认 · ${profile.name}`) : t('Default · native configuration', '默认 · 原生配置');
}

function CatalogError({ error, retry }: { error: string; retry: () => void }) {
  return <div className="profile-error" role="alert"><span>{t('Could not load supplier configurations: ', '无法加载供应商配置：')}{error}</span><button type="button" className="secondary" onClick={retry}>{t('Retry', '重试')}</button></div>;
}

export function NewSessionProfile({ api, app, value, onChange, resuming = false }: { resuming?: boolean; api: Api; app: Provider; value: string; onChange: (id: string) => void }) {
  const { catalog, error, retry, unsupported } = useProfiles(api);
  if (error) return <CatalogError error={error} retry={retry} />;
  if (unsupported || catalog && resuming && !catalog.capabilities?.resumeSelection) return <p role="status">{t("Upgrade this host to select CC-Switch suppliers. The standalone frontend cannot read local databases.", "请升级此宿主以选择 CC-Switch 供应商。纯前端无法读取本地数据库。")}</p>;
  if (!catalog) return null;
  return <div className="field">{t('Supplier configuration', '供应商配置')}<Select label={resuming ? t('Resume session supplier configuration', '恢复会话供应商配置') : t('New session supplier configuration', '新会话供应商配置')} value={value} options={[
    { value: '', label: resuming ? t('Keep session configuration', '沿用原会话配置') : defaultLabel(catalog, app) },
    { value: '@native', label: t('Native host configuration', '宿主原生配置') },
    ...profilesFor(catalog, app).map(profile => ({ value: profile.id, label: profile.name })),
  ]} onChange={onChange} /><small>{t('The selected configuration is kept for this session when defaults change.', '默认项改变后，此会话仍保留所选配置。')}</small>{catalog.found === false && <small>{t('CC-Switch database not found on this host. Set --cc-switch-db to its path.', '此宿主未找到 CC-Switch 数据库，可通过 --cc-switch-db 指定路径。')}</small>}{catalog.unavailable?.filter(p => p.app === app).map(p => <small key={p.id}>{p.name} · {p.reason}</small>)}</div>;
}

export function SessionProfile<T extends { id: string; profileId?: string; profileName?: string }>({ api, session, app, onUpdated, onBusy }: { api: Api; session: T; app: Provider; onUpdated: (session: T) => void; onBusy: (busy: boolean) => void }) {
  const { catalog, error: catalogError, retry, unsupported } = useProfiles(api);
  const [selected, setSelected] = useState(session.profileId || '');
  const [saving, setSaving] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  useEffect(() => { setSelected(session.profileId || ''); setConfirm(false); }, [session.profileId]);
  async function change(force = false) {
    if (saving) return;
    setSaving(true); onBusy(true); setError(''); setNotice('');
    try {
      const updated = await api<T>(`/api/sessions/${encodeURIComponent(session.id)}/profile`, { method: 'POST', body: JSON.stringify(profileBody(selected, force)) });
      onUpdated(updated); setConfirm(false);
      setNotice(t('Supplier configuration switched. Continue the conversation when ready; interrupted requests are not resent automatically.', '供应商配置已切换。准备好后可继续对话；被中断的请求不会自动重发。'));
    } catch (err) {
      if ((err as { status?: number }).status === 409) { setConfirm(true); setError((err as Error).message); }
      else { setConfirm(false); setError((err as Error).message); }
    } finally { setSaving(false); onBusy(false); }
  }
  if (catalogError) return <CatalogError error={catalogError} retry={retry} />;
  if (unsupported || catalog && !catalog.capabilities?.switchSupplier) return <p role="status">{t("Upgrade this host to switch CC-Switch suppliers.", "请升级此宿主以切换 CC-Switch 供应商。")}</p>;
  if (!catalog || (!profilesFor(catalog, app).length && !session.profileId)) return null;
  const available = profilesFor(catalog, app);
  const current = available.find(profile => profile.id === session.profileId);
  const options = [{ value: '', label: defaultLabel(catalog, app) }, { value: '@native', label: t('Native host configuration', '宿主原生配置') }, ...available.map(profile => ({ value: profile.id, label: profile.name }))];
  if (session.profileId && !current) options.push({ value: session.profileId, label: t(`Unavailable · ${session.profileId}`, `配置不可用 · ${session.profileId}`) });
  return <section className="session-profile">
    <h3>{t('Supplier configuration', '供应商配置')}</h3>
    <p>{t('Current · ', '当前 · ')}<strong>{session.profileName || current?.name || session.profileId || t('Native configuration', '原生配置')}</strong></p>
    <Select label={t('Switch session supplier configuration', '切换会话供应商配置')} value={selected} options={options} disabled={saving} onChange={id => { setSelected(id); setConfirm(false); setError(''); setNotice(''); }} />
    <p>{t('Switching restarts the session process with its saved context. Changes already made to files remain.', '切换会重启会话进程并恢复已有上下文，已经执行的文件修改会保留。')}</p>
    {error && <div className="error" role="alert">{error}</div>}
    {confirm ? <div className="profile-confirm" role="alertdialog" aria-label={t('Interrupt and switch supplier?', '中断并切换供应商？')}>
      <p>{t('This session is active. Switching now stops the current work and background tasks, keeps saved context, and does not replay the interrupted request. Already completed actions cannot be undone.', '此会话正在运行。立即切换将停止当前执行及后台任务、保留已保存的上下文，并且不会自动重发被中断的请求。已经完成的操作无法撤销。')}</p>
      <div className="modal-actions"><button type="button" className="secondary" disabled={saving} onClick={() => { setConfirm(false); setError(''); }}>{t('Cancel', '取消')}</button><button type="button" className="danger-button solid" disabled={saving} onClick={() => void change(true)}>{saving ? t('Switching…', '切换中…') : t('Interrupt and switch', '中断并切换')}</button></div>
    </div> : <button type="button" className="secondary" disabled={saving || selected === (session.profileId || '')} onClick={() => void change()}>{saving ? t('Switching…', '切换中…') : t('Switch supplier', '切换供应商')}</button>}
    {notice && <p role="status">{notice}</p>}
  </section>;
}

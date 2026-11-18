import { useEffect, useState } from 'react';
import { useBackend } from '@/backend/backend';
import type { SupplierConfig, SupplierConfigs } from '@/backend/supplierConfig';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { GroupCard } from '@/features/dialogs/SurfaceShell';
import { ConfirmDialog, ErrorText, errorMessage, useProfileCatalog } from '@/features/dialogs/common';
import { t } from '@/lib/i18n';
import { GroupTitle, SectionHeader, useGuard } from './parts';
import { OAuthSection } from './OAuthSection';
import { SupplierEditor } from './SupplierEditor';
import type { SupplierDraft } from './supplierWizardState';

export function SuppliersSection({ mobile }: { mobile: boolean }) {
  const backend = useBackend();
  const guard = useGuard();
  const { catalog, error: catalogError, reload: reloadCatalog } = useProfileCatalog();
  const [collection, setCollection] = useState<SupplierConfigs | null>();
  const [wizard, setWizard] = useState(false);
  const [editing, setEditing] = useState<SupplierConfig>();
  const [removing, setRemoving] = useState<SupplierConfig>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const load = async () => {
    setError('');
    try {
      setCollection(await (backend.supplierConfigs?.() ?? Promise.resolve(null)));
      setWizard(false); setEditing(undefined);
    }
    catch (e) { setError(errorMessage(e)); }
  };
  useEffect(() => {
    let live = true;
    (backend.supplierConfigs?.() ?? Promise.resolve(null)).then(v => { if (live) setCollection(v); }, e => { if (live) setError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend]);
  const save = async (draft: SupplierDraft) => {
    if (!collection || !backend.saveSupplierConfig) throw new Error(t('Reload the supplier list first.', '请先刷新供应商列表。'));
    const { name, app, baseUrl, model, authType, credential } = draft;
    if (!collection.capabilities?.modelNames) delete draft.modelNames;
    const next = await backend.saveSupplierConfig(editing?.id, { ...(collection.capabilities?.advanced ? draft : { name, app, baseUrl, model, authType, credential }), revision: collection.revision });
    setCollection(next); reloadCatalog();
    setNotice(t('Saved. Choose this supplier in application settings or a session.', '已保存。可在应用配置或会话中选择此供应商。'));
  };
  const remove = async () => {
    if (!collection || !removing || !backend.removeSupplierConfig) return;
    setBusy(true); setError('');
    try { setCollection(await backend.removeSupplierConfig(removing.id, collection.revision)); reloadCatalog(); setRemoving(undefined); }
    catch (e) { setError(errorMessage(e)); } finally { setBusy(false); }
  };
  const importOne = async (id: string, app: 'claude' | 'codex') => {
    if (!collection || !backend.importCCSwitch) return;
    setBusy(true); setError('');
    try { setCollection(await backend.importCCSwitch(id, app, collection.revision)); reloadCatalog(); setNotice(t('Imported as an independent local configuration. Credentials stay on this server.', '已导入为独立本地配置，凭据仅保存在宿主。')); }
    catch(e) { setError(errorMessage(e)); } finally { setBusy(false); }
  };
  const visibility = async (hidden: boolean) => {
    if (!collection || !backend.setCCSwitchVisibility) return;
    setBusy(true); setError('');
    try { setCollection(await backend.setCCSwitchVisibility(hidden, collection.revision)); reloadCatalog(); }
    catch(e) { setError(errorMessage(e)); } finally { setBusy(false); }
  };
  const imports = catalog?.profiles.filter(p => p.id.startsWith('ccswitch:')) ?? [];
  return <div>
    {!mobile && <SectionHeader title={t('Suppliers', '供应商')} description={t('Connections saved on the selected server. Existing credentials are never sent back to this form.', '在当前服务器管理供应商配置。支持新增、编辑和切换，已有凭据不会回填。')} />}
    <GroupTitle>{t('API connections', 'API 连接')}</GroupTitle>
    {collection === null ? <p className="text-ui-sm text-foreground-subtle">{t('Upgrade this server to manage API connections. OAuth and imported suppliers remain available below.', '升级此服务器后可管理 API 连接；下方的 OAuth 和导入供应商仍可使用。')}</p>
      : collection === undefined ? !error && <Spinner className="size-4" />
      : <>
        <GroupCard>
          {collection.profiles.length === 0 && <p className="px-4 py-4 text-ui-sm text-foreground-subtle">{t('No API connections saved.', '尚未保存 API 连接。')}</p>}
          {collection.profiles.map(p => <div key={p.id} className="flex flex-wrap items-center gap-2 border-t border-border/60 px-4 py-3 first:border-t-0">
            <div className="min-w-0 flex-1"><p className="break-words text-ui-sm font-medium">{p.name} <span className="font-normal text-foreground-subtle">· {p.app === 'claude' ? 'Claude Code' : 'Codex'}</span></p><p className="break-all text-ui-xs text-foreground-subtle">{p.baseUrl}{p.model ? ` · ${p.model}` : ''}</p><p className="text-ui-xs text-foreground-subtle">{p.hasCredential ? t('Credential configured', '已配置凭据') : t('No credential', '未配置凭据')}</p>{p.notes && <p className="mt-1 line-clamp-2 break-words text-ui-xs text-foreground-subtle">{p.notes}</p>}</div>
            <Button variant="outline" disabled={busy || wizard} onClick={() => { setEditing(p); setWizard(true); }}>{t('Edit', '编辑')}</Button>
            <Button variant="ghost" disabled={busy || wizard} onClick={() => setRemoving(p)}>{t('Remove', '删除')}</Button>
          </div>)}
        </GroupCard>
        <div className="mt-3 flex justify-end"><Button disabled={busy || wizard || !backend.saveSupplierConfig} onClick={() => { setEditing(undefined); setWizard(true); setNotice(''); }}>{t('Add API supplier', '添加 API 供应商')}</Button></div>
      </>}
    {notice && <p role="status" className="mt-2 text-ui-sm text-success">{notice}</p>}
    {error && <div className="mt-2"><ErrorText>{error}</ErrorText><Button variant="ghost" disabled={busy} onClick={() => guard(() => void load())}>{t('Reload', '刷新')}</Button></div>}
    <OAuthSection mobile={mobile} embedded />
    <GroupTitle>CC-Switch</GroupTitle>
    <GroupCard>
      <div className="px-4 py-3 text-ui-sm">
        <p className="mb-2 text-foreground-subtle">{t('Import API connections as independent local copies. Model mappings and supported connection options are copied; global tool settings, routing, MCP and usage history are not imported. Unsupported connection options are reported.', '将 API 连接复制为独立的本地配置，包含模型映射及支持的连接参数；不导入全局工具设置、路由、MCP 和用量历史。不支持的连接参数会明确提示。')}</p>
        {collection?.capabilities?.ccSwitchImport && <label className="flex items-center gap-2"><input type="checkbox" disabled={busy || wizard} checked={!collection.ccSwitchHidden} onChange={e => void visibility(!e.target.checked)} />{t('Show CC-Switch providers', '显示 CC-Switch 供应商列表')}</label>}
      </div>
      {!collection?.ccSwitchHidden && (imports.length ? imports.map(p => { const imported = collection?.profiles.some(local => local.sourceId === p.id && local.app === p.app); return <div key={`${p.app}:${p.id}`} className="flex flex-wrap items-center gap-2 border-t border-border/60 px-4 py-3 text-ui-sm"><span className="min-w-0 flex-1 break-words">{p.name} <span className="text-foreground-subtle">· {p.app === 'claude' ? 'Claude Code' : 'Codex'}</span></span>{collection?.capabilities?.ccSwitchImport && <Button variant="outline" disabled={busy || wizard || imported} onClick={() => void importOne(p.id,p.app)}>{imported ? t('Imported', '已导入') : t('Import local copy', '导入本地副本')}</Button>}</div>; }) : <p className="px-4 py-3 text-ui-sm text-foreground-subtle">{catalogError ? t('Could not read CC-Switch suppliers.', '无法读取 CC-Switch 供应商。') : t('No CC-Switch suppliers.', '暂无 CC-Switch 供应商。')}</p>)}
    </GroupCard>
    {wizard && <SupplierEditor mobile={mobile} editing={editing} advanced={!!collection?.capabilities?.advanced} modelNames={!!collection?.capabilities?.modelNames} probe={!!collection?.capabilities?.probe} onClose={() => { setWizard(false); setEditing(undefined); }} onSave={save} />}
    <ConfirmDialog open={!!removing} onOpenChange={o => { if (!o && !busy) setRemoving(undefined); }} title={t('Remove API connection?', '删除 API 连接？')} description={t('Sessions using this connection will need another supplier on their next launch.', '使用此连接的会话下次启动时需要选择其它供应商。')} confirmLabel={t('Remove', '删除')} destructive onConfirm={() => void remove()} />
  </div>;
}

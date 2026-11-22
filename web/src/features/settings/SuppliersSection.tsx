import { useEffect, useRef, useState } from 'react';
import { CopyPlus, KeyRound, Pencil, Plus, Server, Trash2 } from 'lucide-react';
import { Reorder } from 'motion/react';
import { useBackend } from '@/backend/backend';
import type { SupplierConfig, SupplierConfigs } from '@/backend/supplierConfig';
import type { CodexAccount, Provider, SupplierProfile } from '@/backend/types';
import { Button } from '@/components/ui/button';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Spinner } from '@/components/ui/spinner';
import { GroupCard } from '@/features/dialogs/SurfaceShell';
import { ConfirmDialog, ErrorText, errorMessage, errorStatus, useProfileCatalog } from '@/features/dialogs/common';
import { mergeSupplierOrder, mergeSupplierProfiles, moveSupplier, orderSuppliers, supplierOrderKey } from '@/domain/supplierOrder';
import { useSupplierOrder } from '@/lib/supplierOrder';
import { t } from '@/lib/i18n';
import { SectionHeader, useGuard } from './parts';
import { SupplierEditor } from './SupplierEditor';
import { OAuthTokenEditor } from './OAuthTokenEditor';
import { CodexAccountEditor } from './CodexAccountEditor';
import { SettingsReorderRow } from './SettingsReorderRow';
import type { SupplierDraft } from './supplierWizardState';

type OAuthEntry = { id: string; name: string; notes?: string };

export function SuppliersSection({ mobile }: { mobile: boolean }) {
  const backend = useBackend();
  const { order, setOrder } = useSupplierOrder();
  const guard = useGuard();
  const { catalog, error: catalogError, reload: reloadCatalog } = useProfileCatalog();
  const codexAPI = catalog?.capabilities?.codexAccounts ? backend.codexAccounts : undefined;
  const [app, setApp] = useState<Provider>('claude');
  const [collection, setCollection] = useState<SupplierConfigs | null>();
  const [oauth, setOAuth] = useState<OAuthEntry[]>();
  const [oauthUnsupported, setOAuthUnsupported] = useState(false);
  const [codexAccounts, setCodexAccounts] = useState<CodexAccount[]>();
  const [accountsVersion, setAccountsVersion] = useState(0);
  const [wizard, setWizard] = useState(false);
  const [editing, setEditing] = useState<SupplierConfig>();
  const [oauthWizard, setOAuthWizard] = useState<{ replacing?: OAuthEntry }>();
  const [codexEditor, setCodexEditor] = useState<{ account?: CodexAccount }>();
  const [removing, setRemoving] = useState<SupplierConfig>();
  const [removingOAuth, setRemovingOAuth] = useState<OAuthEntry>();
  const [removingCodex, setRemovingCodex] = useState<CodexAccount>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [previewOrder, setPreviewOrder] = useState<string[] | null>(null);
  const pendingOrder = useRef<string[] | null>(null);
  const finishReorder = (cancelled: boolean) => {
    if (!cancelled && pendingOrder.current) setOrder(mergeSupplierOrder(pendingOrder.current, order));
    pendingOrder.current = null;
    setPreviewOrder(null);
  };

  const loadOAuth = async () => {
    try { setOAuth(await backend.oauthProfiles()); setOAuthUnsupported(false); }
    catch (e) { if ([404, 405, 501].includes(errorStatus(e) ?? 0)) { setOAuthUnsupported(true); setOAuth([]); } else setError(errorMessage(e)); }
  };
  const reloadAccounts = () => { setAccountsVersion(v => v + 1); reloadCatalog(); };
  const load = async () => {
    setError('');
    try { setCollection(await (backend.supplierConfigs?.() ?? Promise.resolve(null))); }
    catch (e) { setError(errorMessage(e)); }
    await loadOAuth();
    reloadAccounts();
  };
  useEffect(() => {
    let live = true;
    (backend.supplierConfigs?.() ?? Promise.resolve(null)).then(v => { if (live) setCollection(v); }, e => { if (live) setError(errorMessage(e)); });
    backend.oauthProfiles().then(v => { if (live) setOAuth(v); }, e => { if (!live) return; if ([404, 405, 501].includes(errorStatus(e) ?? 0)) { setOAuthUnsupported(true); setOAuth([]); } else setError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend]);
  useEffect(() => {
    if (!codexAPI) return;
    const controller = new AbortController();
    codexAPI.list(controller.signal).then(
      accounts => { if (!controller.signal.aborted) setCodexAccounts(accounts); },
      err => { if (!controller.signal.aborted) setError(errorMessage(err)); },
    );
    return () => controller.abort();
  }, [codexAPI, accountsVersion]);
  const save = async (draft: SupplierDraft) => {
    if (!collection || !backend.saveSupplierConfig) throw new Error(t('Reload the supplier list first.', '请先刷新供应商列表。'));
    const { name, app, baseUrl, model, authType, credential } = draft;
    if (!collection.capabilities?.modelNames) delete draft.modelNames;
    setCollection(await backend.saveSupplierConfig(editing?.id, { ...(collection.capabilities?.advanced ? draft : { name, app, baseUrl, model, authType, credential }), revision: collection.revision }));
    reloadCatalog();
    setNotice(t('Supplier saved.', '供应商已保存。'));
  };
  const remove = async () => {
    if (!collection || !removing || !backend.removeSupplierConfig) return;
    setBusy(true); setError('');
    try { setCollection(await backend.removeSupplierConfig(removing.id, collection.revision)); reloadCatalog(); setRemoving(undefined); }
    catch (e) { setError(errorMessage(e)); } finally { setBusy(false); }
  };
  const removeCodex = async () => {
    if (!codexAPI || !removingCodex) return;
    setBusy(true); setError('');
    try {
      await codexAPI.remove(removingCodex.id);
      setRemovingCodex(undefined);
      reloadAccounts();
    } catch (err) { setError(errorMessage(err)); reloadAccounts(); }
    finally { setBusy(false); }
  };
  const importOne = async (id: string, provider: Provider) => {
    if (!collection || !backend.importCCSwitch) return;
    setBusy(true); setError('');
    try { setCollection(await backend.importCCSwitch(id, provider, collection.revision)); reloadCatalog(); setNotice(t('Imported as a local copy.', '已导入为本地副本。')); }
    catch (e) { setError(errorMessage(e)); } finally { setBusy(false); }
  };
  const visibility = async (hidden: boolean) => {
    if (!collection || !backend.setCCSwitchVisibility) return;
    setBusy(true); setError('');
    try { setCollection(await backend.setCCSwitchVisibility(hidden, collection.revision)); reloadCatalog(); }
    catch (e) { setError(errorMessage(e)); } finally { setBusy(false); }
  };

  // Independent stores keep their entries visible when an older host lacks another API.
  const availableAccounts = codexAPI ? codexAccounts ?? [] : [];
  const retrySignOut = availableAccounts.some(p => p.id === removingCodex?.id && p.signOutPending);
  const accountProfiles = availableAccounts.map(p => ({ ...p, app: 'codex' as const, kind: 'oauth' }));
  const merged = mergeSupplierProfiles<SupplierProfile>(catalog?.profiles ?? [], [...(collection?.profiles ?? []), ...accountProfiles], oauth ?? [], !!collection?.ccSwitchHidden);
  const entries = orderSuppliers(merged.filter(p => p.app === app), previewOrder ?? order);
  const disabled = busy || wizard || !!oauthWizard || !!codexEditor;

  return <div>
    {!mobile && <SectionHeader title={t('Suppliers', '供应商')} />}
    <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
      <Tabs value={app} onValueChange={v => { finishReorder(true); setApp(v as Provider); }}>
        <TabsList><TabsTrigger value="claude">Claude</TabsTrigger><TabsTrigger value="codex">Codex</TabsTrigger></TabsList>
      </Tabs>
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" disabled={disabled || !collection || !backend.saveSupplierConfig} onClick={() => { setEditing(undefined); setWizard(true); setNotice(''); }}><Plus />{t('Add API', '添加 API')}</Button>
        {app === 'codex' ? (
          <Button variant="outline" disabled={disabled || !codexAPI} title={catalog && !codexAPI ? t('Upgrade the host to add ChatGPT accounts.', '升级宿主后可添加 ChatGPT 账号。') : undefined} onClick={() => { setCodexEditor({}); setNotice(''); }}><Plus />{t('Add ChatGPT account', '添加 ChatGPT 账号')}</Button>
        ) : (
          <Button variant="outline" disabled={disabled || oauthUnsupported || !oauth} onClick={() => setOAuthWizard({})}><Plus />{t('Add OAuth', '添加 OAuth')}</Button>
        )}
      </div>
    </div>
    {collection?.capabilities?.ccSwitchImport && <label className="mb-3 flex items-center gap-2 text-ui-sm text-foreground-subtle"><input type="checkbox" disabled={disabled} checked={!collection.ccSwitchHidden} onChange={e => void visibility(!e.target.checked)} />{t('Show CC-Switch suppliers', '显示 CC-Switch 供应商')}</label>}
    <GroupCard className="overflow-visible">
      {!catalog && !collection && !oauth && !error && <div className="flex h-16 items-center justify-center"><Spinner className="size-4" /></div>}
      {!!entries.length && <Reorder.Group key={app} axis="y" className="list-none"
        values={entries.map(supplierOrderKey)}
        onReorder={next => { pendingOrder.current = next; setPreviewOrder(next); }}>
      {entries.map(p => {
        const key = supplierOrderKey(p);
        const local = collection?.profiles.find(item => item.id === p.id && item.app === p.app);
        const token = p.app === 'claude' && p.kind === 'oauth' ? oauth?.find(item => item.id === p.id) : undefined;
        const account = p.app === 'codex' && p.kind === 'oauth' ? availableAccounts.find(item => item.id === p.id) : undefined;
        const imported = p.id.startsWith('ccswitch:');
        const copied = collection?.profiles.some(item => item.sourceId === p.id && item.app === p.app);
        const Icon = p.kind === 'oauth' ? KeyRound : Server;
        const edit = () => {
          if (local) { setEditing(local); setWizard(true); }
          else if (token) setOAuthWizard({ replacing: token });
          else if (account) { setCodexEditor({ account }); setNotice(''); }
        };
        const removeEntry = () => {
          if (local) setRemoving(local);
          else if (token) setRemovingOAuth(token);
          else if (account) { setRemovingCodex(account); setError(''); }
        };
        return <SettingsReorderRow key={key} id={key} name={p.name} disabled={disabled || entries.length < 2}
          className="border-t border-border/60 px-2 py-3 first:rounded-t-xl first:border-t-0 last:rounded-b-xl sm:px-3"
          onMove={direction => setOrder(moveSupplier(entries, order, key, direction))}
          onDragEnd={finishReorder}>
          <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-surface text-foreground-subtle"><Icon className="size-4" /></span>
          <div className="min-w-0 flex-1"><p className="break-words text-ui-sm font-medium">{p.name}</p><p className="break-all text-ui-xs text-foreground-subtle">{p.kind === 'oauth' ? t('OAuth · Official', 'OAuth · 官方') : imported ? 'API · CC-Switch' : local?.sourceId ? t('API · Local copy from CC-Switch', 'API · CC-Switch 本地副本') : local ? t('API · Local', 'API · 本地') : t('API · Source not provided', 'API · 来源未提供')}{local?.baseUrl ? ` · ${local.baseUrl}` : ''}</p>{(token?.notes || local?.notes) && <p className="mt-1 whitespace-pre-wrap break-words text-ui-xs text-foreground-subtle">{token?.notes || local?.notes}</p>}
            {account?.signOutPending && <p className="mt-1 text-ui-xs text-warning">{t('Sign-out pending', '尚未退出登录')}</p>}
          </div>
          {imported && collection?.capabilities?.ccSwitchImport && <Button variant="ghost" size="icon-lg" disabled={disabled || copied} aria-label={copied ? t('Already imported', '已导入') : t(`Import ${p.name}`, `导入 ${p.name}`)} title={copied ? t('Already imported', '已导入') : t('Import local copy', '导入本地副本')} onClick={() => void importOne(p.id, p.app)}><CopyPlus /></Button>}
          {(local || token || account && !account.disabled) && (
            <Button variant="ghost" size="icon-lg" disabled={disabled} aria-label={t(`Edit ${p.name}`, `编辑 ${p.name}`)} title={t('Edit', '编辑')} onClick={edit}><Pencil /></Button>
          )}
          {(local || token || account) && (
            <Button variant="ghost" size="icon-lg" disabled={disabled} aria-label={account?.signOutPending ? t(`Retry sign-out for ${p.name}`, `重试退出 ${p.name}`) : t(`Delete ${p.name}`, `删除 ${p.name}`)} title={account?.signOutPending ? t('Retry sign-out', '重试退出登录') : t('Delete', '删除')} onClick={removeEntry}><Trash2 /></Button>
          )}
        </SettingsReorderRow>;
      })}
      </Reorder.Group>}
      {!entries.length && (catalog || collection || oauth) && <p className="px-4 py-6 text-ui-sm text-foreground-subtle">{t('No suppliers for this application.', '此应用暂无供应商。')}</p>}
    </GroupCard>
    {collection === null && <p className="mt-2 text-ui-sm text-foreground-subtle">{t('Upgrade this host to manage API connections.', '升级此宿主后可管理 API 连接。')}</p>}
    {oauthUnsupported && app === 'claude' && <p className="mt-2 text-ui-sm text-foreground-subtle">{t('Upgrade this host to manage OAuth accounts.', '升级此宿主后可管理 OAuth 账号。')}</p>}
    {catalogError && <ErrorText>{catalogError}</ErrorText>}
    {notice && <p role="status" className="mt-2 text-ui-sm text-success">{notice}</p>}
    {error && <div className="mt-2"><ErrorText>{error}</ErrorText><Button variant="ghost" disabled={busy} onClick={() => guard(() => void load())}>{t('Reload', '刷新')}</Button></div>}
    {wizard && <SupplierEditor mobile={mobile} editing={editing} defaultApp={app} advanced={!!collection?.capabilities?.advanced} modelNames={!!collection?.capabilities?.modelNames} probe={!!collection?.capabilities?.probe} onClose={() => { setWizard(false); setEditing(undefined); }} onSave={save} />}
    {oauthWizard && <OAuthTokenEditor mobile={mobile} replacing={oauthWizard.replacing} notesSupported={!!catalog?.capabilities?.oauthNotes} onClose={() => setOAuthWizard(undefined)} onSave={async (name, token, notes) => { if (oauthWizard.replacing) await backend.editOAuthProfile(oauthWizard.replacing.id, { name, notes, ...(token ? { token } : {}) }); else await backend.addOAuthProfile(name, token, notes); await loadOAuth(); reloadCatalog(); }} />}
    {codexEditor && codexAPI && <CodexAccountEditor api={codexAPI} mobile={mobile} account={codexEditor.account} onClose={() => setCodexEditor(undefined)} onSaved={() => { reloadAccounts(); setNotice(t('ChatGPT account saved.', 'ChatGPT 账号已保存。')); }} />}
    <ConfirmDialog open={!!removing} onOpenChange={o => { if (!o && !busy) setRemoving(undefined); }} title={t('Remove API connection?', '删除 API 连接？')} description={t('Sessions using this connection will need another supplier on their next launch.', '使用此连接的会话下次启动时需要选择其它供应商。')} confirmLabel={t('Remove', '删除')} destructive busy={busy} onConfirm={() => void remove()} />
    <ConfirmDialog open={!!removingOAuth} onOpenChange={o => { if (!o && !busy) setRemovingOAuth(undefined); }} title={t(`Delete "${removingOAuth?.name ?? ''}"?`, `删除「${removingOAuth?.name ?? ''}」？`)} description={t('Sessions using it cannot start until you pick another configuration.', '使用它的会话需要重新选择配置才能启动。')} confirmLabel={t('Delete', '删除')} destructive busy={busy} onConfirm={() => { if (!removingOAuth) return; setBusy(true); setError(''); void backend.removeOAuthProfile(removingOAuth.id).then(async () => { setRemovingOAuth(undefined); await loadOAuth(); reloadCatalog(); }).catch(e => setError(errorMessage(e))).finally(() => setBusy(false)); }} />
    <ConfirmDialog open={!!removingCodex} onOpenChange={o => { if (!o && !busy) setRemovingCodex(undefined); }}
      title={retrySignOut ? t(`Retry sign-out for ${removingCodex?.name ?? ''}?`, `重试退出「${removingCodex?.name ?? ''}」？`) : t(`Remove ${removingCodex?.name ?? ''}?`, `移除「${removingCodex?.name ?? ''}」？`)}
      description={t('This signs the host out of this account. Its session history remains on the host, and sessions tied to it can no longer resume.', '此操作会在宿主退出该账号。会话历史仍保留在宿主，绑定该账号的会话将无法恢复。')}
      confirmLabel={retrySignOut ? t('Retry sign-out', '重试退出登录') : t('Remove account', '移除账号')} destructive busy={busy} onConfirm={() => void removeCodex()}>
      <ErrorText>{error}</ErrorText>
    </ConfirmDialog>
  </div>;
}

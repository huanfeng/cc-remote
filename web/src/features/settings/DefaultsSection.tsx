// Session defaults: which supplier new sessions start with, and CC Remote's
// own defaults (permission / model / effort) per provider, saved with the
// host's revision so a stale tab cannot overwrite a newer change.
import { useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';

import { FileCog } from 'lucide-react';

import { BackendError, useBackend, type ClaudeSettings } from '@/backend/backend';
import type { Preferences, Provider } from '@/backend/types';
import { t } from '@/lib/i18n';
import { dangerousPermission, providerName } from '@/domain/providers';
import { GroupCard, Row, SurfaceShell } from '@/features/dialogs/SurfaceShell';
import { ConfirmDialog, ErrorText, SimpleSelect, errorMessage, profileLabel, profilesFor, useProfileCatalog } from '@/features/dialogs/common';
import { EffortField, ModelField, PermissionField } from '@/features/dialogs/pickers';
import { GroupTitle, SectionHeader, useReportDirty } from './parts';
import { StatuslineSection } from './StatuslineSection';

export function DefaultsSection({ mobile, provider = 'claude' }: { mobile: boolean; provider?: Provider }) {
  return (
    <div>
      {!mobile && <SectionHeader title={provider === 'claude' ? 'Claude Code' : 'Codex'} description={t('Default supplier, model, effort and permissions for new sessions on this server.', '此服务器上新会话使用的供应商、模型、思考强度和权限。')} />}
      <DefaultSupplier key={`s-${provider}`} provider={provider} />
      <RemoteDefaults key={`d-${provider}`} provider={provider} />
      {provider === 'claude' && <NativeSettings mobile={mobile} />}
      {provider === 'claude' && <StatuslineSection mobile={mobile} />}
    </div>
  );
}

function DefaultSupplier({ provider }: { provider: Provider }) {
  const backend = useBackend();
  const { catalog, error, reload } = useProfileCatalog();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState('');
  if (error) return <ErrorText>{t(`Could not load supplier configurations: ${error}`, `无法加载供应商配置：${error}`)}</ErrorText>;
  const supported = !catalog || catalog.capabilities?.defaultProfile;
  const value = catalog?.configuredDefaults?.[provider] ?? '';
  const missing = !!value && value !== '@native' && !profilesFor(catalog, provider).some(p => p.id === value);
  const choose = async (next: string) => {
    setBusy(true); setProblem('');
    try { await backend.setDefaultProfile(provider, next); reload(); } catch (e) { setProblem(errorMessage(e)); } finally { setBusy(false); }
  };
  return (
    <>
      <GroupTitle>{t('Supplier', '供应商')}</GroupTitle>
      <GroupCard>
        <Row
          label={t('Supplier for new sessions', '新会话供应商')}
          description={supported
            ? t('Used by the web and by ccr when no supplier is named. Existing sessions keep theirs.', '网页和 ccr 在未指定供应商时使用。已有会话保持原供应商。')
            : t('Upgrade this host to choose the default supplier.', '请升级此宿主以指定默认供应商。')}
          control={supported && (
            <span className="flex items-center gap-2 max-sm:w-full">
              {busy && <Spinner className="size-3.5 text-foreground-subtle" />}
              <SimpleSelect
                value={value}
                onChange={v => void choose(v)}
                disabled={!catalog || busy}
                ariaLabel={t('Supplier for new sessions', '新会话供应商')}
                options={[
                  { value: '', label: t('Follow CC-Switch current', '跟随 CC-Switch 当前项') },
                  { value: '@native', label: t('Native host configuration', '宿主原生配置') },
                  ...profilesFor(catalog, provider).map(p => ({ value: p.id, label: profileLabel(p) })),
                  ...(missing ? [{ value, label: t(`Unavailable · ${value}`, `不可用 · ${value}`) }] : []),
                ]}
              />
            </span>
          )}
        />
      </GroupCard>
      {missing && <p className="mt-2 px-1 text-ui-sm text-destructive">{t('This supplier is gone; pick another.', '这个供应商已不存在，请重新选择。')}</p>}
      {problem && <div className="mt-2"><ErrorText>{problem}</ErrorText></div>}
    </>
  );
}

function RemoteDefaults({ provider }: { provider: Provider }) {
  const backend = useBackend();
  const [saved, setSaved] = useState<Preferences>();
  const [draft, setDraft] = useState<Preferences>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [conflict, setConflict] = useState(false);
  const [version, setVersion] = useState(0);
  useEffect(() => {
    let live = true;
    backend.preferences(provider).then(p => { if (live) { setSaved(p); setDraft(p); setConflict(false); } }, e => { if (live) setError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend, provider, version]);
  const dirty = !!saved && !!draft && (saved.permissionMode !== draft.permissionMode || saved.model !== draft.model || saved.effort !== draft.effort);
  useReportDirty(`defaults:${provider}`, dirty);
  const follow = t(`Follow ${providerName(provider)} settings`, `跟随 ${providerName(provider)} 配置`);

  const save = async () => {
    if (!draft || !saved) return;
    setBusy(true); setError(''); setNotice(''); setConflict(false);
    try {
      // The revision is the host's opaque content hash, sent back as read.
      const next = await backend.savePreferences(provider, {
        ...draft, revision: saved.revision,
        ...(dangerousPermission(draft.permissionMode, provider) ? { confirmDangerous: true } : {}),
      });
      setSaved(next); setDraft(next);
      setNotice(t('Saved. Managed sessions use them on the next send after current work finishes.', '已保存。托管会话在当前工作结束后的下次发送时生效。'));
    } catch (e) {
      if (e instanceof BackendError && e.status === 409) setConflict(true);
      else setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <GroupTitle>{t(`${providerName(provider)} · CC Remote defaults`, `${providerName(provider)} · CC Remote 默认值`)}</GroupTitle>
      {!draft ? (
        error ? <ErrorText>{error}</ErrorText> : <div className="flex h-24 items-center justify-center"><Spinner className="size-4 text-foreground-subtle" /></div>
      ) : (
        <>
          <GroupCard>
            <Row
              label={provider === 'codex' ? t('Sandbox', '沙箱权限') : t('Permission mode', '权限模式')}
              description={dangerousPermission(draft.permissionMode, provider)
                ? <span className="text-destructive">{t('Allows broad file and command access. Use only in trusted projects.', '允许广泛访问文件和执行命令，仅用于可信项目。')}</span>
                : t('Empty follows the provider configuration.', '留空跟随提供方配置。')}
              control={<PermissionField provider={provider} value={draft.permissionMode} inheritLabel={follow} onChange={permissionMode => setDraft(d => d && { ...d, permissionMode })} />}
            />
            <Row label={t('Model', '模型')} control={<ModelField provider={provider} value={draft.model} inheritLabel={follow} onChange={model => setDraft(d => d && { ...d, model })} />} />
            <Row label={t('Effort', '推理强度')} control={<EffortField provider={provider} model={draft.model} value={draft.effort} inheritLabel={follow} onChange={effort => setDraft(d => d && { ...d, effort })} />} />
          </GroupCard>
          {conflict && <ConflictNotice what={t('These defaults were', '这些默认值已')} onReload={() => setVersion(v => v + 1)} />}
          <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
            {notice && !dirty && <p role="status" className="mr-auto text-ui-sm text-success">{notice}</p>}
            <Button variant="ghost" size="lg" disabled={!dirty || busy} onClick={() => setDraft(saved)}>{t('Revert', '还原')}</Button>
            <Button size="lg" disabled={!dirty || busy || conflict} onClick={() => void save()}>
              {busy && <Spinner className="size-3.5" />}
              {busy ? t('Saving…', '保存中…') : t('Save', '保存')}
            </Button>
          </div>
          {error && <div className="mt-2"><ErrorText>{error}</ErrorText></div>}
        </>
      )}
    </>
  );
}

type NativeDraft = Pick<ClaudeSettings, 'model' | 'effortLevel' | 'permissionMode'>;

/** Claude's own user settings (~/.claude/settings.json), saved with its revision. Collapsed by default. */
function NativeSettings({ mobile }: { mobile: boolean }) {
  const backend = useBackend();
  const [open, setOpen] = useState(false);
  const [saved, setSaved] = useState<ClaudeSettings>();
  const [draft, setDraft] = useState<NativeDraft>();
  const [error, setError] = useState('');
  const [conflict, setConflict] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [version, setVersion] = useState(0);
  const [loaded, setLoaded] = useState(-1);
  const [jsonMode, setJsonMode] = useState(false);
  const [json, setJson] = useState('');
  const [discard, setDiscard] = useState(false);

  // Loads on first open and on Reload; collapsing and reopening keeps the draft.
  useEffect(() => {
    if (!open || loaded === version && saved) return;
    setLoaded(version);
    let live = true;
    setError('');
    backend.claudeSettings().then(v => {
      if (!live) return;
      setSaved(v); setDraft({ model: v.model, effortLevel: v.effortLevel, permissionMode: v.permissionMode }); setJson(JSON.stringify(v.document ?? {}, null, 2)); setConflict(false);
    }, e => { if (live) setError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend, open, version]);

  const dirty = !!saved && !!draft && (saved.model !== draft.model || saved.effortLevel !== draft.effortLevel || saved.permissionMode !== draft.permissionMode || json !== JSON.stringify(saved.document ?? {}, null, 2));
  const close = () => { if (!busy) { if (dirty) setDiscard(true); else setOpen(false); } };
  useReportDirty('native', dirty);
  const follow = t('Follow Claude default', '跟随 Claude 默认值');

  const save = async () => {
    if (!saved || !draft) return;
    setBusy(true); setError(''); setNotice(''); setConflict(false);
    try {
      const document = jsonMode ? JSON.parse(json) as Record<string, unknown> : undefined;
      if (jsonMode && (!document || typeof document !== 'object' || Array.isArray(document))) throw new Error(t('Expected a JSON object.', '请填写 JSON 对象。'));
      const mode = document ? (document.permissions as { defaultMode?: string } | undefined)?.defaultMode ?? '' : draft.permissionMode;
      const next = await backend.saveClaudeSettings({
        ...draft, revision: saved.revision,
        ...(jsonMode ? { document, editDocument: true } : {}),
        ...(dangerousPermission(mode, 'claude') ? { confirmDangerous: true } : {}),
      });
      setSaved(next); setDraft({ model: next.model, effortLevel: next.effortLevel, permissionMode: next.permissionMode }); setJson(JSON.stringify(next.document ?? {}, null, 2));
      setNotice(t('Written to the Claude user settings.', '已写入 Claude 用户配置。'));
    } catch (e) {
      if (e instanceof BackendError && e.status === 409) setConflict(true);
      else setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <GroupTitle>{t('Native configuration', '原生配置')}</GroupTitle>
      <GroupCard><Row label={t('Claude Code native configuration', 'Claude Code 原生配置')} description={t('Edit user settings in a separate dialog.', '编辑 Claude Code 用户配置，支持表单和 JSON。')} control={<Button variant="outline" onClick={() => setOpen(true)}>{t('Configure', '配置')}</Button>} /></GroupCard>
      <SurfaceShell open={open} mobile={mobile} onOpenChange={o => { if (!o) close(); }} title={t('Claude Code native configuration', 'Claude Code 原生配置')} className="sm:max-w-3xl sm:h-[min(86vh,760px)]">
        <div className="pt-2">
          {!saved || !draft ? (
            error ? <ErrorText>{error}</ErrorText> : <div className="flex h-24 items-center justify-center"><Spinner className="size-4 text-foreground-subtle" /></div>
          ) : (
            <>
              <div className="mb-4 flex gap-2"><Button variant={!jsonMode ? 'secondary' : 'outline'} onClick={() => { if (jsonMode && dirty) { setError(t('Save or revert JSON before switching.', '请先保存或还原 JSON 再切换。')); return; } setJsonMode(false); }}>{t('Form', '表单')}</Button><Button disabled={!saved.document} variant={jsonMode ? 'secondary' : 'outline'} onClick={() => {
                if (jsonMode) return;
                const document = { ...saved.document };
                if (draft.model) document.model = draft.model; else delete document.model;
                if (draft.effortLevel) document.effortLevel = draft.effortLevel; else delete document.effortLevel;
                const permissions = { ...(document.permissions as Record<string, unknown> ?? {}) };
                if (draft.permissionMode) permissions.defaultMode = draft.permissionMode; else delete permissions.defaultMode;
                if (Object.keys(permissions).length) document.permissions = permissions; else delete document.permissions;
                setJson(JSON.stringify(document, null, 2)); setJsonMode(true);
              }}>JSON</Button></div>
              {jsonMode ? <><p className="mb-3 text-ui-sm text-foreground-subtle">{t('Hidden settings use preservation markers. Their original values stay on the server. Unknown fields are preserved.', '敏感配置和扩展字段以保留标记显示，原值保存在宿主。保留标记或删除标记所在字段均不会清除已有敏感配置。')}</p><textarea disabled={busy} aria-label="Claude Code JSON" spellCheck={false} className="min-h-80 w-full rounded-lg border border-border bg-background p-3 font-mono text-ui-sm" value={json} onChange={e => setJson(e.target.value)} /></> : <GroupCard>

                <div className="flex items-start gap-2.5 px-4 py-3">
                  <FileCog className="mt-0.5 size-4 shrink-0 text-foreground-subtle" />
                  <div className="min-w-0 flex-1">
                    <div className="text-ui-base font-medium">{t('Claude user settings', 'Claude 用户配置')}</div>
                    <code className="block break-all font-mono text-ui-xs text-foreground-subtlest">{saved.path}</code>
                  </div>
                </div>
                <Row
                  label={t('Default permission mode', '默认权限模式')}
                  description={dangerousPermission(draft.permissionMode, 'claude')
                    ? <span className="text-destructive">{t('Allows broad file and command access. Use only in trusted projects.', '允许广泛访问文件和执行命令，仅用于可信项目。')}</span>
                    : undefined}
                  control={<PermissionField provider="claude" value={draft.permissionMode} inheritLabel={follow} onChange={permissionMode => setDraft(d => d && { ...d, permissionMode })} />}
                />
                <Row label={t('Default model', '默认模型')} control={<ModelField provider="claude" value={draft.model} inheritLabel={follow} onChange={model => setDraft(d => d && { ...d, model })} />} />
                <Row label={t('Default effort', '默认推理强度')} control={<EffortField provider="claude" model={draft.model} value={draft.effortLevel} inheritLabel={follow} onChange={effortLevel => setDraft(d => d && { ...d, effortLevel })} />} />
              </GroupCard>}
              <p className="mt-2 px-1 text-ui-xs text-foreground-subtlest">
                {t('Project and organisation policies and launch flags may override these; other Claude settings are kept as they are.', '项目、组织策略及启动参数可能覆盖这些默认值；其它 Claude 配置原样保留。')}
              </p>
              {conflict && <ConflictNotice what={t('settings.json was', 'settings.json 已')} onReload={() => setVersion(v => v + 1)} />}
              <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
                {notice && !dirty && <p role="status" className="mr-auto text-ui-sm text-success">{notice}</p>}
                <Button variant="ghost" size="lg" disabled={!dirty || busy} onClick={() => { setDraft({ model: saved.model, effortLevel: saved.effortLevel, permissionMode: saved.permissionMode }); setJson(JSON.stringify(saved.document ?? {}, null, 2)); }}>{t('Revert', '还原')}</Button>
                <Button size="lg" disabled={!dirty || busy || conflict} onClick={() => void save()}>
                  {busy && <Spinner className="size-3.5" />}
                  {busy ? t('Saving…', '保存中…') : t('Write settings.json', '写入 settings.json')}
                </Button>
              </div>
              {error && <div className="mt-2"><ErrorText>{error}</ErrorText></div>}
            </>
          )}
        </div>
      </SurfaceShell>
      <ConfirmDialog open={discard} onOpenChange={setDiscard} title={t('Discard changes?', '放弃更改？')} confirmLabel={t('Discard', '放弃')} onConfirm={() => { setDiscard(false); setOpen(false); setDraft(saved && { model:saved.model, effortLevel:saved.effortLevel, permissionMode:saved.permissionMode }); setJson(JSON.stringify(saved?.document ?? {}, null, 2)); }} />
    </>
  );
}

/** A save refused with 409: the value changed elsewhere since it was read. */
function ConflictNotice({ what, onReload }: { what: string; onReload: () => void }) {
  return (
    <div role="alert" className="mt-3 flex flex-wrap items-center gap-2 rounded-lg bg-warning-surface px-3 py-2 text-ui-sm text-warning-surface-foreground">
      <span className="min-w-0 flex-1">{t(`${what} changed elsewhere. Reload, then make your change again.`, `${what}在别处修改。请刷新后重新修改。`)}</span>
      <Button variant="outline" size="lg" onClick={onReload}>{t('Reload', '刷新')}</Button>
    </div>
  );
}

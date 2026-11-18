import { useState, useRef, useId, type ReactNode } from 'react';
import { useBackend } from '@/backend/backend';
import type { SupplierConfig } from '@/backend/supplierConfig';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Command, CommandInput, CommandList, CommandItem, CommandEmpty } from '@/components/ui/command';
import { ListFilter } from 'lucide-react';
import { Spinner } from '@/components/ui/spinner';
import { SurfaceShell } from '@/features/dialogs/SurfaceShell';
import { ConfirmDialog, ErrorText, errorMessage, SimpleSelect } from '@/features/dialogs/common';
import { t } from '@/lib/i18n';
import { useReportDirty } from './parts';
import type { SupplierDraft } from './supplierWizardState';
import { supplierDraft, supplierJSON, parseSupplierJSON, validateSupplier, reasoningEfforts, reasoningSummaries, verbosities, hasOneMContext, modelWithoutContext, withOneMContext } from './supplierEditorState';

function SupplierField({ label, description, children }: { label: string; description?: string; children: ReactNode }) {
  const id = useId();
  return <div role="group" aria-labelledby={id} className="min-w-0 space-y-1.5"><p id={id} className="text-ui-sm font-medium">{label}</p>{children}{description && <p className="text-ui-xs leading-relaxed text-foreground-subtle">{description}</p>}</div>;
}
function SupplierGroup({ title, description, children }: { title: string; description?: string; children: ReactNode }) {
  return <section className="space-y-4"><div className="border-b border-border/60 pb-2"><h3 className="text-ui-sm font-semibold">{title}</h3>{description && <p className="mt-1 text-ui-xs leading-relaxed text-foreground-subtle">{description}</p>}</div>{children}</section>;
}
function SupplierModelInput({ label, value, models, disabled, context = false, onChange }: { label: string; value: string; models: string[]; disabled: boolean; context?: boolean; onChange: (value: string) => void }) {
  const [open, setOpen] = useState(false);
  const oneM = context && hasOneMContext(value);
  const changeModel = (model: string) => onChange(context && oneM ? withOneMContext(model, true) : model);
  return <div className="flex min-w-0 gap-2">
    <Input size="lg" aria-label={label} disabled={disabled} maxLength={160} value={context ? modelWithoutContext(value) : value} placeholder={t('Default or enter model ID', '沿用默认，或输入模型 ID')} onChange={e => changeModel(e.target.value)} className="min-w-0 flex-1" />
    <Popover open={open} onOpenChange={setOpen}><PopoverTrigger asChild><Button variant="outline" disabled={disabled || !models.length} aria-label={t(`Choose ${label}`, `选择${label}`)} title={!models.length ? t('Fetch the model list first', '请先检测并获取模型列表') : undefined} size="lg" className="shrink-0 px-2"><ListFilter className="size-3.5" /></Button></PopoverTrigger>
      <PopoverContent align="end" className="w-[min(360px,calc(100vw-3rem))] p-0"><Command><CommandInput aria-label={t('Search models', '搜索模型')} placeholder={t('Search model ID…', '搜索模型 ID…')} /><CommandList><CommandEmpty>{t('No matching models', '没有匹配的模型')}</CommandEmpty>{models.map(model => <CommandItem key={model} value={model} onSelect={() => { changeModel(model); setOpen(false); }}><span className="break-all">{model}</span></CommandItem>)}</CommandList></Command></PopoverContent>
    </Popover>
  </div>;
}
function SupplierContextToggle({label, value, disabled, onChange}: {label: string; value: string; disabled: boolean; onChange: (value: string) => void}) {
  return <label className="flex h-8 items-center gap-2 whitespace-nowrap text-ui-xs text-foreground-subtle"><Checkbox aria-label={t(`${label} supports 1M context`, `${label} 支持 1M 上下文`)} disabled={disabled || !modelWithoutContext(value).trim()} checked={hasOneMContext(value)} onCheckedChange={checked => onChange(withOneMContext(value, checked === true))} />1M</label>;

}

type Panel = 'connection' | 'models' | 'json';
export function SupplierEditor({ mobile, editing, advanced, modelNames, probe, onClose, onSave }: {
  mobile: boolean; editing?: SupplierConfig; advanced: boolean; modelNames?: boolean; probe?: boolean; onClose: () => void;
  onSave: (draft: SupplierDraft) => Promise<void>;
}) {
  const backend = useBackend();
  const [preset, setPreset] = useState('custom');
  const [models, setModels] = useState<string[]>([]);
  const [probing, setProbing] = useState(false);
  const [probeNotice, setProbeNotice] = useState('');
  const [probeError, setProbeError] = useState('');
  const connectionVersion = useRef(0);
  const [draft, setDraft] = useState(() => supplierDraft(editing));
  const [panel, setPanel] = useState<Panel>('connection');
  const [json, setJSON] = useState('');
  const [jsonDirty, setJSONDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [discard, setDiscard] = useState(false);
  const dirty = supplierJSON(draft) !== supplierJSON(supplierDraft(editing)) || !!draft.credential || jsonDirty;
  useReportDirty('supplier-editor', dirty);
  const update = (value: Partial<SupplierDraft>) => { setDraft(d => ({ ...d, ...value })); setError(''); if ('app' in value || 'baseUrl' in value || 'authType' in value || 'credential' in value) { connectionVersion.current++; setModels([]); setProbeNotice(''); setProbeError(''); } };
  const close = () => { if (!busy) { if (dirty) setDiscard(true); else onClose(); } };
  const readJSON = (): SupplierDraft | undefined => {
    try { return parseSupplierJSON(json, draft); }
    catch { setError(t('Invalid connection JSON. Use supported public fields with their original types; credentials must be entered separately.', '连接 JSON 无效。请使用支持的公开字段及正确类型；凭据请在连接页面单独填写。')); return undefined; }
  };
  const navigate = (next: Panel) => {
    if (next === panel || probing) return;
    if (panel === 'json' && jsonDirty) { const parsed = readJSON(); if (!parsed) return; setDraft(parsed); setJSONDirty(false); setModels([]); setProbeNotice(''); setProbeError(''); }
    if (next === 'json') setJSON(supplierJSON(draft));
    setPanel(next); setError('');
  };
  const save = async () => {
    if (busy) return;
    const value = panel === 'json' && jsonDirty ? readJSON() : draft;
    if (!value) return;
    const invalid = validateSupplier(value, !!editing);
    if (invalid) {
      if (value !== draft) { setDraft(value); setJSONDirty(false); }
      setPanel(invalid === 'options' ? 'models' : 'connection');
      setError(invalid === 'name' ? t('Enter a name of up to 60 characters without control characters.', '请填写不超过 60 个字符的名称，不能包含控制字符。') : invalid === 'url' || invalid === 'website' ? t('Use HTTP(S) URLs without credentials, query parameters or fragments.', '请填写 HTTP(S) 地址，不能包含用户名、密码、查询参数或片段。') : invalid === 'credential' ? t('Enter the supplier credential.', '请填写供应商凭据。') : t('Check model names, option values and numeric ranges.', '请检查模型名称、选项及数值范围。'));
      return;
    }
    setBusy(true); setError('');
    try { await onSave({ ...value, name:value.name.trim(), baseUrl:value.baseUrl.trim(), model:value.model.trim(), credential:value.credential.trim(), website:value.website?.trim() }); onClose(); }
    catch (e) { setError(errorMessage(e)); }
    finally { setBusy(false); }
  };
  const checkAPI = async () => {
    if (!backend.probeSupplier || probing) return;
    const version = connectionVersion.current;
    setProbing(true); setError(''); setProbeNotice(''); setProbeError('');
    try {
      const result = await backend.probeSupplier({id:editing?.id, app:draft.app, baseUrl:draft.baseUrl.trim(), authType:draft.authType, credential:draft.credential.trim()});
      if (version !== connectionVersion.current) return;
      setModels(result.models);
      if (!draft.model && result.models.length === 1) update({model:result.models[0]});
      setProbeNotice(t(`Model endpoint connected · ${result.latencyMs} ms · ${result.models.length} models${result.truncated ? ' (partial)' : ''}`, `模型接口连通 · ${result.latencyMs} ms · ${result.models.length} 个模型${result.truncated ? '（部分列表）' : ''}`));
    } catch (e) { if (version === connectionVersion.current) setProbeError(errorMessage(e)); }
    finally { setProbing(false); }
  };
  const codex = draft.codexOptions ?? {};
  const disabled = busy || probing;
  const probeButton = (label: string) => <Button variant="outline" size="lg" className="shrink-0" disabled={disabled || !probe || !backend.probeSupplier} onClick={() => void checkAPI()}><span className="flex size-3.5 items-center justify-center">{probing ? <Spinner className="size-3.5" /> : <ListFilter className="size-3.5" />}</span>{label}</Button>;
  const probeStatus = (hint: string) => <div role="status" aria-live="polite" aria-atomic="true" title={probeError || probeNotice || hint} className={`h-5 truncate text-ui-xs leading-5 ${probeError ? 'text-destructive' : probeNotice ? 'text-success' : 'text-foreground-subtle'}`}>{probing ? t('Checking authentication and fetching models…', '正在检查认证并获取模型列表…') : probeError || probeNotice || (!probe ? t('Upgrade the server to enable connection tests.', '升级服务器后可检测连接。') : hint)}</div>;

  return <>
    <SurfaceShell open mobile={mobile} className="sm:h-[min(760px,86dvh)] sm:max-w-3xl" onOpenChange={o => { if (!o) close(); }} title={editing ? t('Edit supplier', '编辑供应商') : t('Add supplier', '添加供应商')} description={t('Edit any section directly. Changes are written only when saved.', '直接切换配置区域，点击保存后才会写入。')} footer={<>
      <Button variant="ghost" disabled={busy || probing} onClick={close}>{t('Cancel', '取消')}</Button>
      <Button disabled={busy || probing} onClick={() => void save()}>{busy && <Spinner className="size-3.5" />}{t('Save supplier', '保存供应商')}</Button>
    </>}>
      <div className="mb-5 flex gap-1 rounded-lg bg-surface p-1" role="group" aria-label={t('Supplier editor sections', '供应商编辑分区')}>
        {(['connection','models','json'] as const).map(p => <Button key={p} disabled={disabled || p === 'json' && !advanced} variant={panel === p ? 'secondary' : 'ghost'} className="flex-1" aria-pressed={panel === p} onClick={() => navigate(p)}>{p === 'connection' ? t('Connection & account', '连接与账号') : p === 'models' ? t('Models & options', '模型与选项') : t('Connection JSON', '连接 JSON')}</Button>)}
      </div>
      <div className="space-y-6 text-ui-sm">
        {panel === 'connection' && <>
          <SupplierGroup title={t('Basic information', '基础信息')}>
            <div className="grid gap-4 sm:grid-cols-2">
              <SupplierField label={t('Application', '应用')}><SimpleSelect className="w-full" value={draft.app} disabled={disabled || !!editing} ariaLabel={t('Application', '应用')} options={[{value:'claude',label:'Claude Code'},{value:'codex',label:'Codex'}]} onChange={v => { setPreset('custom'); setDraft(d => ({ ...supplierDraft({app:v === 'codex' ? 'codex' : 'claude'}), name:d.name, website:d.website, notes:d.notes })); setError(''); setModels([]); setProbeNotice(''); setProbeError(''); connectionVersion.current++; }} /></SupplierField>
              <SupplierField label={t('Configuration name', '配置名称')}><Input size="lg" aria-label={t('Configuration name', '配置名称')} autoFocus maxLength={60} disabled={disabled} value={draft.name} placeholder={t('For example: Work API', '例如：工作 API')} onChange={e => update({name:e.target.value})} /></SupplierField>
            </div>
            {!editing && <SupplierField label={t('Connection preset', '连接预设')} description={t('Choose a preset, then fill in the endpoint and credential below.', '选择预设后，在下方填写或调整地址与凭据。')}><SimpleSelect className="w-full" value={preset} disabled={disabled} ariaLabel={t('Connection preset', '连接预设')} options={[{value:'custom',label:t('Custom compatible API', draft.app === 'claude' ? '自定义兼容 API · 默认 Auth Token' : '自定义兼容 API')},{value:'official',label:draft.app === 'claude' ? 'Anthropic API · API Key' : 'OpenAI API · API Key'}]} onChange={v => {setPreset(v); update(v === 'official' ? {baseUrl:draft.app === 'claude' ? 'https://api.anthropic.com' : 'https://api.openai.com/v1',authType:'apiKey',name:draft.name || (draft.app === 'claude' ? 'Anthropic' : 'OpenAI')} : {baseUrl:'',authType:draft.app === 'claude' ? 'authToken' : 'apiKey'});}} /></SupplierField>}
          </SupplierGroup>
          <SupplierGroup title={t('Connection & authentication', '连接与认证')}>
            <SupplierField label={t('API base URL', 'API 基础地址')}>
              <div className="flex gap-2"><Input size="lg" className="min-w-0 flex-1" aria-label={t('API base URL', 'API 基础地址')} type="url" disabled={disabled} value={draft.baseUrl} placeholder="https://api.example.com/v1" onChange={e => update({baseUrl:e.target.value})} />{probeButton(t('Test', '测试'))}</div>
              {probeStatus(t('Use the supplier base URL; include /v1 only if required.', '填写供应商基础地址，仅在要求时包含 /v1。'))}
            </SupplierField>
            <div className="grid gap-4 sm:grid-cols-2">
              <SupplierField label={t('Authentication', '认证方式')}><SimpleSelect className="w-full" value={draft.authType} disabled={disabled || draft.app === 'codex'} ariaLabel={t('Authentication type', '认证方式')} options={draft.app === 'codex' ? [{value:'apiKey',label:'API Key'}] : [{value:'authToken',label:'Auth Token (Bearer)'},{value:'apiKey',label:'API Key'}]} onChange={v => update({authType:v === 'authToken' ? 'authToken' : 'apiKey'})} /></SupplierField>
              <SupplierField label={editing ? t('Replace credential', '替换凭据') : t('API credential', 'API 凭据')} description={editing ? t('Leave blank to keep the saved credential.', '留空保留已保存的凭据。') : t('Credentials are stored privately and never shown again.', '凭据保存在宿主，不会再次回显。')}><Input size="lg" aria-label={editing ? t('Replace credential (leave blank to keep)', '替换凭据（留空保留）') : t('API credential', 'API 凭据')} disabled={disabled} type="password" autoComplete="new-password" spellCheck={false} value={draft.credential} onChange={e => update({credential:e.target.value})} /></SupplierField>
            </div>
          </SupplierGroup>
          {advanced && <SupplierGroup title={t('Additional information', '补充信息')}>
            <SupplierField label={t('Website (optional)', '官网（可选）')}><Input size="lg" aria-label={t('Website (optional)', '官网（可选）')} disabled={disabled} type="url" value={draft.website} onChange={e => update({website:e.target.value})} /></SupplierField>
            <SupplierField label={t('Notes (optional)', '备注（可选）')} description={t('Do not include credentials in notes.', '请勿在备注中填写凭据。')}><textarea aria-label={t('Notes (optional)', '备注（可选）')} disabled={disabled} rows={2} maxLength={2000} className="w-full resize-y rounded-md border border-border bg-background px-3 py-2" value={draft.notes} onChange={e => update({notes:e.target.value})} /></SupplierField>
          </SupplierGroup>}
        </>}
        {panel === 'models' && <>
          <div className="border-b border-border/60 pb-2"><div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-ui-sm font-semibold">{t('Models', '模型配置')}</h3>{probeButton(t('Fetch model list', '获取模型列表'))}</div>{probeStatus(t('Enter a model ID or fetch available models.', '可直接输入模型 ID，或获取列表后选择。'))}</div>
          {!advanced ? <p className="text-foreground-subtle">{t('Upgrade this server for advanced model options.', '升级此服务器后可使用高级模型选项。')}</p> : draft.app === 'claude' ? <>
            <SupplierGroup title={t('Model mappings', '模型映射')} description={t('Map Claude aliases to supplier models; blank retains the application mapping.', '留空沿用应用映射；仅在供应商模型支持时勾选 1M 上下文。')}>
              <div className="overflow-x-auto"><table className="w-full min-w-[600px] table-fixed border-separate border-spacing-x-0 border-spacing-y-2">
                <colgroup><col className="w-20" />{modelNames && <col />}<col /><col className="w-14" /></colgroup>
                <thead><tr className="text-left text-ui-xs text-foreground-subtle"><th className="pb-1 font-normal">{t('Role', '模型角色')}</th>{modelNames && <th className="pb-1 pr-3 font-normal">{t('Display name', '显示名称')}</th>}<th className="pb-1 pr-3 font-normal">{t('Request model', '实际请求模型')}</th><th className="pb-1 font-normal">1M</th></tr></thead>
                <tbody>{(modelNames ? ['sonnet','opus','haiku','fable'] as const : ['sonnet','opus','haiku'] as const).map(key => {
                  const role = key[0].toUpperCase() + key.slice(1);
                  const value = draft.modelMappings?.[key] ?? '';
                  const change = (model: string) => update({modelMappings:{...draft.modelMappings,[key]:model}});
                  return <tr key={key}><th scope="row" className="pr-3 text-left text-ui-sm font-medium">{role}</th>
                    {modelNames && <td className="pr-3"><Input size="lg" aria-label={t(`${role} display name`, `${role} 显示名称`)} disabled={disabled} maxLength={160} value={draft.modelNames?.[key] ?? ''} placeholder={t('Optional display name', '可选显示名称')} onChange={e => update({modelNames:{...draft.modelNames,[key]:e.target.value}})} /></td>}
                    <td className="pr-3"><SupplierModelInput context label={t(`${role} request model`, `${role} 实际请求模型`)} disabled={disabled} models={models} value={value} onChange={change} /></td>
                    <td><SupplierContextToggle label={role} value={value} disabled={disabled} onChange={change} /></td>
                  </tr>;
                })}</tbody>
              </table></div>

            </SupplierGroup>
          </> : null}
          <SupplierGroup title={t('Fallback model', '默认兜底模型')} description={t('Blank follows the application default; explicit session choices take precedence.', '留空沿用应用默认模型；会话中明确选择的模型优先。')}>
            <div className="flex items-center gap-3"><div className="min-w-0 flex-1"><SupplierModelInput context={draft.app === 'claude'} label={t('Fallback model', '默认兜底模型')} disabled={disabled} models={models} value={draft.model} onChange={model => update({model})} /></div>{draft.app === 'claude' && <SupplierContextToggle label={t('Fallback model', '默认兜底模型')} value={draft.model} disabled={disabled} onChange={model => update({model})} />}</div>
          </SupplierGroup>
          {advanced && (draft.app === 'claude' ? <SupplierGroup title={t('Advanced options', '高级选项')}><SupplierField label={t('API timeout (milliseconds)', 'API 超时（毫秒）')} description={t('0 follows the application default.', '0 沿用应用默认值。')}><Input size="lg" aria-label={t('API timeout (milliseconds)', 'API 超时（毫秒）')} disabled={disabled} type="number" min={0} max={3600000} value={draft.timeoutMs ?? 0} onChange={e => update({timeoutMs:Number(e.target.value)})} /></SupplierField></SupplierGroup> : <SupplierGroup title={t('Advanced options', '高级选项')} description={t('Codex uses the Responses API. Choose values supported by your model and supplier.', 'Codex 使用 Responses API，请选择供应商与模型支持的参数。')}>
            <div className="grid gap-4 sm:grid-cols-2">{([{key:'reasoningEffort',label:t('Reasoning effort','思考强度'),values:reasoningEfforts},{key:'reasoningSummary',label:t('Reasoning summary','思考摘要'),values:reasoningSummaries},{key:'verbosity',label:t('Response verbosity','回答详细程度'),values:verbosities}] as const).map(item => <SupplierField key={item.key} label={item.label}><SimpleSelect className="w-full" disabled={disabled} ariaLabel={item.label} value={codex[item.key] ?? ''} options={item.values.map(value => ({value,label:value || t('Application default','沿用应用')}))} onChange={value => update({codexOptions:{...codex,[item.key]:value}})} /></SupplierField>)}</div>
            <div className="grid gap-4 sm:grid-cols-2">{([{key:'contextWindow',label:t('Context window (tokens)','上下文窗口（Token）')},{key:'autoCompactTokenLimit',label:t('Auto compact threshold (tokens)','自动压缩阈值（Token）')}] as const).map(item => <SupplierField key={item.key} label={item.label} description={t('0 follows the application default.', '0 沿用应用默认值。')}><Input size="lg" aria-label={item.label} disabled={disabled} type="number" min={0} max={100000000} value={codex[item.key] ?? 0} onChange={e => update({codexOptions:{...codex,[item.key]:Number(e.target.value)}})} /></SupplierField>)}</div>
            <SupplierField label={t('Response storage', '响应存储')}><SimpleSelect className="w-full" disabled={disabled} ariaLabel={t('Response storage', '响应存储')} value={codex.disableResponseStorage ? 'disabled' : 'default'} options={[{value:'default',label:t('Application default','沿用应用')},{value:'disabled',label:t('Disable storage','禁用存储')}]} onChange={v => update({codexOptions:{...codex,disableResponseStorage:v === 'disabled'}})} /></SupplierField>
          </SupplierGroup>)}
        </>}
        {panel === 'json' && <SupplierGroup title={t('Connection JSON', '连接 JSON')} description={t('Supported public connection fields only; credentials are entered separately. Save writes the configuration.', '编辑支持的公开连接字段；凭据单独填写。点击保存后才写入配置。')}><textarea disabled={disabled} aria-label={t('Connection JSON','连接 JSON')} spellCheck={false} rows={16} className="w-full rounded-lg border border-border bg-background p-3 font-mono text-ui-xs" value={json} onChange={e => {setJSON(e.target.value); setJSONDirty(true); setError('');}} /></SupplierGroup>}
        {error && <ErrorText>{error}</ErrorText>}
      </div>
    </SurfaceShell>
    <ConfirmDialog open={discard} onOpenChange={setDiscard} title={t('Discard unsaved changes?', '放弃未保存的修改？')} description={t('The supplier has not been saved. Entered credentials will be cleared.', '供应商尚未保存，已填写的凭据将被清除。')} confirmLabel={t('Discard', '放弃')} onConfirm={onClose} />
  </>;
}

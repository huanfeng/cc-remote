import type { SupplierConfig } from '../../backend/supplierConfig.ts';
import { validateSupplierStep, type SupplierDraft } from './supplierWizardState.ts';

export const reasoningEfforts = ['', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh'];
export const reasoningSummaries = ['', 'auto', 'concise', 'detailed', 'none'];
export const verbosities = ['', 'low', 'medium', 'high'];
const publicKeys = ['name', 'app', 'baseUrl', 'model', 'authType', 'website', 'notes', 'modelMappings', 'modelNames', 'timeoutMs', 'codexOptions'];

export function supplierDraft(p?: Partial<SupplierConfig>): SupplierDraft {
  return { name:p?.name ?? '', app:p?.app ?? 'claude', baseUrl:p?.baseUrl ?? '', model:p?.model ?? '', authType:p?.authType ?? (p?.app === 'codex' ? 'apiKey' : 'authToken'), credential:'', website:p?.website ?? '', notes:p?.notes ?? '', modelMappings:{ ...p?.modelMappings }, modelNames:{ ...p?.modelNames }, timeoutMs:p?.timeoutMs ?? 0, codexOptions:{ ...p?.codexOptions } };
}
export function supplierJSON(draft: SupplierDraft): string {
  return JSON.stringify(Object.fromEntries(publicKeys.map(key => [key, draft[key as keyof SupplierDraft]])), null, 2);
}
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function validObject(value: unknown, stringKeys: string[], numberKeys: string[], boolKeys: string[] = []): boolean {
  return record(value) && Object.entries(value).every(([key, v]) => stringKeys.includes(key) ? typeof v === 'string' : numberKeys.includes(key) ? typeof v === 'number' : boolKeys.includes(key) && typeof v === 'boolean');
}
export function parseSupplierJSON(text: string, current: SupplierDraft): SupplierDraft {
  const value: unknown = JSON.parse(text);
  if (!record(value) || Object.keys(value).some(k => !publicKeys.includes(k))) throw new Error('unsupported fields');
  for (const [key, v] of Object.entries(value)) {
    if (key === 'modelMappings' || key === 'modelNames') { if (!validObject(v, ['haiku','sonnet','opus','fable'], [])) throw new Error('invalid mappings'); }
    else if (key === 'codexOptions') { if (!validObject(v, ['reasoningEffort','reasoningSummary','verbosity'], ['contextWindow','autoCompactTokenLimit'], ['disableResponseStorage'])) throw new Error('invalid options'); }
    else if (key === 'timeoutMs') { if (typeof v !== 'number') throw new Error('invalid timeout'); }
    else if (typeof v !== 'string') throw new Error('invalid field');
  }
  if (value.app !== undefined && value.app !== current.app) throw new Error('application mismatch');
  if (value.authType !== undefined && !['apiKey','authToken'].includes(value.authType as string)) throw new Error('invalid authentication');
  return { ...current, ...value };
}
function validURL(value: string): boolean {
  try { const url = new URL(value.trim()); return ['http:','https:'].includes(url.protocol) && !!url.hostname && !url.username && !url.password && !url.search && !url.hash; } catch { return false; }
}
export function validateSupplier(draft: SupplierDraft, editing: boolean): 'name' | 'url' | 'credential' | 'website' | 'options' | undefined {
  const connection = validateSupplierStep(draft, 1, editing) ?? validateSupplierStep(draft, 2, editing);
  if (connection) return connection;
  if (!['apiKey','authToken'].includes(draft.authType) || draft.app === 'codex' && draft.authType !== 'apiKey') return 'options';
  if (draft.website && !validURL(draft.website)) return 'website';
  if (Array.from(draft.name.trim()).length > 60 || /[\x00-\x1f\x7f]/.test(draft.name)) return 'name';
  if ((draft.notes?.length ?? 0) > 2000 || draft.model.length > 160) return 'options';
  if (draft.app === 'claude') {
    if (draft.timeoutMs !== undefined && (!Number.isSafeInteger(draft.timeoutMs) || draft.timeoutMs < 0 || draft.timeoutMs > 3600000)) return 'options';
    for (const fields of [draft.modelMappings, draft.modelNames]) if (Object.entries(fields ?? {}).some(([key, v]) => !['sonnet','opus','haiku','fable'].includes(key) || typeof v !== 'string' || v.length > 160 || /[\x00-\x1f\x7f]/.test(v))) return 'options';
  } else {
    const c = draft.codexOptions ?? {};
    if (!reasoningEfforts.includes(c.reasoningEffort ?? '') || !reasoningSummaries.includes(c.reasoningSummary ?? '') || !verbosities.includes(c.verbosity ?? '')) return 'options';
    for (const v of [c.contextWindow, c.autoCompactTokenLimit]) if (v !== undefined && (!Number.isSafeInteger(v) || v < 0 || v > 100000000)) return 'options';
  }
}


export function hasOneMContext(model: string): boolean {
  return /\[1m\]$/i.test(model.trimEnd());
}
export function modelWithoutContext(model: string): string {
  return hasOneMContext(model) ? model.trimEnd().slice(0, -4).trimEnd() : model;
}
export function withOneMContext(model: string, enabled: boolean): string {
  const base = modelWithoutContext(model).trim();
  return base && enabled ? `${base}[1M]` : base;
}

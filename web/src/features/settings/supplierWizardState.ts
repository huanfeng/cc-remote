import type { SupplierConfigWrite } from '../../backend/supplierConfig.ts';

export type SupplierDraft = Omit<SupplierConfigWrite, 'revision'>;

export function validateSupplierStep(draft: SupplierDraft, step: number, editing: boolean): 'name' | 'url' | 'credential' | undefined {
  if (step === 1) {
    if (!draft.name.trim()) return 'name';
    try {
      const url = new URL(draft.baseUrl.trim());
      if (!['https:', 'http:'].includes(url.protocol) || !url.hostname || url.username || url.password || url.search || url.hash) return 'url';
    } catch { return 'url'; }
  }
  if (step === 2 && !editing && !draft.credential.trim()) return 'credential';
}

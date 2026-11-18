import type { Provider } from './types.ts';

export interface SupplierAdvanced {
  website?: string;
  notes?: string;
  modelMappings?: { haiku?: string; sonnet?: string; opus?: string; fable?: string };
  modelNames?: { haiku?: string; sonnet?: string; opus?: string; fable?: string };
  timeoutMs?: number;
  codexOptions?: { reasoningEffort?: string; reasoningSummary?: string; verbosity?: string; contextWindow?: number; autoCompactTokenLimit?: number; disableResponseStorage?: boolean };
}
export interface SupplierConfig extends SupplierAdvanced {
  sourceId?: string;
  id: string;
  name: string;
  app: Provider;
  baseUrl: string;
  model: string;
  authType: 'apiKey' | 'authToken';
  hasCredential: boolean;
  revision: string;
}
export interface SupplierConfigs { ccSwitchHidden?: boolean; profiles: SupplierConfig[]; revision: string; capabilities?: { advanced?: boolean; modelNames?: boolean; probe?: boolean; ccSwitchImport?: boolean } }
export interface SupplierConfigWrite extends SupplierAdvanced {
  name: string;
  app: Provider;
  baseUrl: string;
  model: string;
  authType: 'apiKey' | 'authToken';
  /** Write-only. Empty preserves an existing secret. */
  credential: string;
  revision: string;
}

export interface SupplierProbe { id?: string; app: Provider; baseUrl: string; authType: string; credential: string }
export interface SupplierProbeResult { models: string[]; latencyMs: number; truncated: boolean }

import type { HostInfo, NewSessionRequest, SessionMode } from '../backend/types.ts';

export type Provider = 'claude' | 'codex';
// Missing capability fields identify legacy Claude-only hosts. Availability false still
// means a modern host understands Codex routing (history/defaults remain useful).
export const hostProviders = (info?: { providers?: Provider[]; codexAvailable?: boolean }): Provider[] =>
  info?.providers?.includes('codex') || typeof info?.codexAvailable === 'boolean' ? ['claude', 'codex'] : ['claude'];

export function codexTerminalSupport(info?: Pick<HostInfo, 'features' | 'version'>): 'native' | 'suppliers' | undefined {
  if (info?.features?.includes('codex-terminal')) return 'suppliers';
  // Releases before the feature flag: 0.41 started terminals, 0.43 added supplier selection.
  const release = /^v?0\.(\d+)\.\d+$/.exec(info?.version ?? '');
  const minor = Number(release?.[1]);
  if (minor === 41 || minor === 42) return 'native';
  if (minor >= 43 && minor <= 45) return 'suppliers';
}

export const providerOf = (value?: { provider?: Provider }): Provider => value?.provider === 'codex' ? 'codex' : 'claude';
export const providerName = (provider: Provider) => provider === 'codex' ? 'Codex' : 'Claude';
export const providerQuery = (path: string, provider: Provider) => provider === 'codex' ? `${path}${path.includes('?') ? '&' : '?'}provider=codex` : path;
export const nativeListPath = (provider: Provider) => providerQuery('/api/native/sessions', provider);
export const providerEfforts = (provider: Provider) => provider === 'codex' ? ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'] : ['low', 'medium', 'high', 'xhigh', 'max'];
export const codexPermissions = ['read-only', 'workspace-write', 'danger-full-access'];
export const dangerousPermission = (mode: string, provider: Provider = 'claude') => provider === 'codex' ? mode === 'danger-full-access' : ['bypassPermissions', 'dontAsk', 'auto'].includes(mode);
export const sessionKey = (id: string, native: boolean, provider: Provider = 'claude') => `${native ? provider === 'codex' ? 'native-codex' : 'native' : 'session'}:${id}`;

export function newSessionBody(provider: Provider, title: string, cwd: string, overrides: { permissionMode: string; model: string; effort: string }, mode: SessionMode, trustFolder: boolean, profileId = ''): NewSessionRequest {
  const session = { title: title.trim(), cwd: cwd.trim(), ...(profileId ? { profileId } : {}) };
  if (provider === 'codex' && mode === 'terminal') return { ...session, provider, mode };
  return {
    ...session, ...overrides, model: overrides.model.trim(),
    confirmDangerous: dangerousPermission(overrides.permissionMode, provider),
    ...(provider === 'codex' ? { provider, mode } : mode === 'terminal' ? { mode, trustFolder } : {}),
  };
}

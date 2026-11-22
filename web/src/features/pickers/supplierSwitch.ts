// Supplier choices shared by creation, details and the anchored quick switch.
import type { ProfileCatalog, ProfileUsageReply, Provider, SupplierProfile, Session } from '../../backend/types.ts';
import type { Backend } from '../../backend/backend.ts';
import { BackendError } from '../../backend/errors.ts';
import { t } from '../../lib/i18n.ts';
import type { PickerOption } from './options.ts';
import { supplierUsageFor } from './supplierUsage.ts';

export const profilesFor = (catalog: ProfileCatalog | undefined, app: Provider): SupplierProfile[] =>
  catalog?.profiles.filter(p => p.app === app) ?? [];

export const profileLabel = (p: { name: string; kind?: string }) => (p.kind === 'oauth' && !/oauth/i.test(p.name) ? `${p.name} · OAuth` : p.name);

/** What "default" resolves to for app: a profile, the native configuration, or a profile that is gone. */
export function defaultSupplierLabel(catalog: ProfileCatalog | undefined, app: Provider): string {
  const id = catalog?.defaultProfiles[app];
  if (!id || id === '@native') return t('Default · native configuration', '默认 · 原生配置');
  const p = profilesFor(catalog, app).find(x => x.id === id);
  return p ? t(`Default · ${profileLabel(p)}`, `默认 · ${profileLabel(p)}`) : t('Default · unavailable', '默认 · 已不可用');
}

/** The supplier choices for a new or resumed session. */
export function supplierOptions(catalog: ProfileCatalog | undefined, app: Provider, resuming = false, usage: ProfileUsageReply | null | undefined = null): PickerOption[] {
  const description = (id: string) => app === 'claude' ? supplierUsageFor(usage, id) : undefined;
  const defaultID = catalog?.defaultProfiles[app] || '@native';
  return [
    { value: '', label: resuming ? t('Keep the session configuration', '沿用原会话配置') : defaultSupplierLabel(catalog, app), ...(resuming ? {} : { description: description(defaultID) }) },
    { value: '@native', label: t('Native host configuration', '宿主原生配置'), description: description('@native') },
    ...profilesFor(catalog, app).map(p => ({ value: p.id, label: profileLabel(p), description: p.kind === 'oauth' ? description(p.id) : undefined })),
  ];
}

type SupplierSwitchSession = Pick<Session, 'profileId' | 'codexAccountLocked'>;

export const isCodexAccount = (id: string | undefined) => id?.startsWith('codex-account:') ?? false;

export const hasLockedCodexAccount = (session: SupplierSwitchSession) =>
  !!session.codexAccountLocked && isCodexAccount(session.profileId);

export function supplierSwitchOptions(catalog: ProfileCatalog | undefined, app: Provider, usage: ProfileUsageReply | null | undefined, session?: SupplierSwitchSession): PickerOption[] {
  const options = supplierOptions(catalog, app, false, usage);
  const defaultID = catalog?.defaultProfiles[app] || '@native';
  if (app === 'codex') {
    if (!session?.codexAccountLocked) return options;
    if (hasLockedCodexAccount(session)) return [];
    return options.filter(({ value }) => !isCodexAccount(value || defaultID));
  }
  const native = usage?.profiles['@native'];
  if (native?.reason !== 'no-login' || native.available !== false || native.stale || native.error) return options;
  return options.filter(({ value }) => value !== '@native' && (value !== '' || defaultID !== '@native'));
}

/** A session stores its resolved supplier; an empty ID means native, not a default request. */
export const currentSupplierValue = (session: Pick<Session, 'profileId'>): string => session.profileId || '@native';

/** A conflict requires explicit confirmation; never retry or replay work here. */
export async function switchSupplier(backend: Pick<Backend, 'switchProfile'>, session: Pick<Session, 'id'>, profileId: string, force = false) {
  try {
    const updated = await backend.switchProfile(session.id, profileId, force);
    return { status: 'switched' as const, session: updated };
  } catch (error) {
    if (!force && error instanceof BackendError && error.status === 409) return { status: 'confirmation' as const };
    throw error;
  }
}

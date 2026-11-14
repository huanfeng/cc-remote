// Supplier choices shared by creation, details and the anchored quick switch.
import type { ProfileCatalog, Provider, SupplierProfile, Session } from '../../backend/types.ts';
import type { Backend } from '../../backend/backend.ts';
import { BackendError } from '../../backend/errors.ts';
import { t } from '../../lib/i18n.ts';
import type { PickerOption } from './options.ts';

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
export function supplierOptions(catalog: ProfileCatalog | undefined, app: Provider, resuming = false): PickerOption[] {
  return [
    { value: '', label: resuming ? t('Keep the session configuration', '沿用原会话配置') : defaultSupplierLabel(catalog, app) },
    { value: '@native', label: t('Native host configuration', '宿主原生配置') },
    ...profilesFor(catalog, app).map(p => ({ value: p.id, label: profileLabel(p) })),
  ];
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

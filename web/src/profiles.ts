import type { Provider } from './providers';

// A supplier profile is separate from Session.provider (the Claude/Codex engine).
// This catalog intentionally contains no credentials or raw settings.
export type SupplierProfile = { id: string; name: string; app: Provider };
export type ProfileCatalog = { schemaVersion?: number; capabilities?: { resumeSelection?: boolean; switchSupplier?: boolean }; source?: string; found?: boolean; unavailable?: {id: string; name: string; app: Provider; reason: string}[]; profiles: SupplierProfile[]; defaultProfiles: Partial<Record<Provider, string>> };

export function profilesFor(catalog: ProfileCatalog, app: Provider): SupplierProfile[] {
  return catalog.profiles.filter(profile => profile.app === app);
}

export function defaultProfile(catalog: ProfileCatalog, app: Provider): SupplierProfile | undefined {
  return profilesFor(catalog, app).find(profile => profile.id === catalog.defaultProfiles[app]);
}

export function profileBody(profileId: string, force = false) {
  return { profileId, ...(force ? { force: true } : {}) };
}

export function supportsProfileCatalog(catalog: ProfileCatalog): boolean {
 return catalog?.source === 'cc-switch' && catalog.schemaVersion === 1 && Array.isArray(catalog.profiles) && !!catalog.defaultProfiles && typeof catalog.defaultProfiles === 'object';
}

export const supplierOrderKey = (p: { app: string; id: string }) => `${p.app}:${p.id}`;

export function mergeSupplierProfiles<T extends { app: string; id: string; name: string; kind?: string }>(catalog: T[], local: T[], oauth: { id: string; name: string }[], hidden: boolean): T[] {
  const merged = new Map<string, T>();
  for (const p of catalog) if (!(hidden && p.id.startsWith('ccswitch:'))) merged.set(supplierOrderKey(p), p);
  for (const p of local) merged.set(supplierOrderKey(p), p);
  for (const p of oauth) {
    const entry = { ...p, app: 'claude', kind: 'oauth' } as T;
    merged.set(supplierOrderKey(entry), entry);
  }
  return [...merged.values()];
}

export function decodeSupplierOrder(raw: string | null): string[] {
  try {
    const value: unknown = JSON.parse(raw ?? 'null');
    return Array.isArray(value) ? [...new Set(value.filter((v): v is string => typeof v === 'string'))] : [];
  } catch { return []; }
}

/** Unknown/new entries retain server order at the end; never mutate the catalog. */
export function orderSuppliers<T extends { app: string; id: string }>(entries: T[], order: string[]): T[] {
  const ranks = new Map(order.map((id, index) => [id, index]));
  return [...entries].sort((a, b) => (ranks.get(supplierOrderKey(a)) ?? Infinity) - (ranks.get(supplierOrderKey(b)) ?? Infinity));
}

export function moveSupplier(entries: { app: string; id: string }[], order: string[], key: string, direction: -1 | 1): string[] {
  const keys = orderSuppliers(entries, order).map(supplierOrderKey);
  const index = keys.indexOf(key);
  const target = index + direction;
  if (index < 0 || target < 0 || target >= keys.length) return order;
  [keys[index], keys[target]] = [keys[target], keys[index]];
  return mergeSupplierOrder(keys, order);
}

export function mergeSupplierOrder(visible: string[], order: string[]): string[] {
  const keys = new Set(visible);
  return [...visible, ...order.filter(key => !keys.has(key))];
}

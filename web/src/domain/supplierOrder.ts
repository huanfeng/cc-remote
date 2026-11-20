export const supplierOrderKey = (p: { app: string; id: string }) => `${p.app}:${p.id}`;

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
  // Keep preferences for temporarily unavailable suppliers.
  return [...keys, ...order.filter(id => !keys.includes(id))];
}

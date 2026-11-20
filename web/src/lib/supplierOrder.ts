import { useSyncExternalStore } from 'react';
import { hostKey } from './hosts';
import { decodeSupplierOrder } from '../domain/supplierOrder';

const cache = new Map<string, string[]>();
const listeners = new Set<() => void>();
const read = (key: string) => {
  if (!cache.has(key)) {
    let raw = null;
    try { raw = localStorage.getItem(key); } catch { /* Storage is optional. */ }
    cache.set(key, decodeSupplierOrder(raw));
  }
  return cache.get(key)!;
};
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; };
if (typeof window !== 'undefined') window.addEventListener('storage', event => {
  if (event.key && !event.key.startsWith('ccn-supplier-order')) return;
  if (event.key) cache.delete(event.key); else cache.clear();
  listeners.forEach(listener => listener());
});

/** Client-only preference: works with old hosts without changing their API. */
export function useSupplierOrder() {
  const key = hostKey('ccn-supplier-order');
  const order = useSyncExternalStore(subscribe, () => read(key));
  const setOrder = (next: string[]) => {
    cache.set(key, next);
    try { localStorage.setItem(key, JSON.stringify(next)); } catch { /* Retain in memory. */ }
    listeners.forEach(listener => listener());
  };
  return { order, setOrder };
}

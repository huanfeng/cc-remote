// The host's model catalogue (GET /api/models), cached per provider and
// session for the page's lifetime; refresh asks the host to re-read it.
import { useCallback, useEffect, useRef, useState } from 'react';
import { useBackend, type Backend } from '@/backend/backend';
import type { ModelList, Provider } from '@/backend/types';

const cache = new WeakMap<Backend, Map<string, Promise<ModelList>>>();

function load(backend: Backend, provider: Provider, sessionId: string | undefined, refresh: boolean) {
  let byKey = cache.get(backend);
  if (!byKey) cache.set(backend, (byKey = new Map()));
  const key = `${provider}:${sessionId ?? ''}`;
  let p = byKey.get(key);
  if (!p || refresh) {
    p = backend.models({ provider, ...(sessionId ? { sessionId } : {}), ...(refresh ? { refresh: true } : {}) });
    byKey.set(key, p);
    p.catch(() => { if (byKey!.get(key) === p) byKey!.delete(key); });
  }
  return p;
}

export function useModels(provider: Provider, sessionId?: string, enabled = true) {
  const backend = useBackend();
  const [list, setList] = useState<ModelList>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const generation = useRef(0);

  const fetch = useCallback((refresh: boolean) => {
    const g = ++generation.current;
    setLoading(true); setError('');
    load(backend, provider, sessionId, refresh)
      .then(l => { if (g === generation.current) setList(l); })
      .catch(e => { if (g === generation.current) setError(e instanceof Error ? e.message : String(e)); })
      .finally(() => { if (g === generation.current) setLoading(false); });
  }, [backend, provider, sessionId]);

  useEffect(() => {
    setList(undefined);
    if (enabled) fetch(false);
    return () => { generation.current++; };
  }, [enabled, fetch]);

  return { list, loading, error, refresh: () => fetch(true) };
}

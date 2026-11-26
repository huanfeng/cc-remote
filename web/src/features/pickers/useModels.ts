import { useCallback, useEffect, useRef, useState } from 'react';
import { useBackend, type Backend } from '@/backend/backend';
import type { ModelContext, ModelList, Provider } from '@/backend/types';
import { useApp } from '@/store/app';

// Share concurrent lookups. The host caches the catalog itself and reads
// current launch preferences on each lookup.
const pending = new WeakMap<Backend, Map<string, Promise<ModelList>>>();

function load(backend: Backend, key: string, opts: Parameters<Backend['models']>[0]) {
  let requests = pending.get(backend);
  if (!requests) pending.set(backend, (requests = new Map()));
  const active = requests;
  const existing = active.get(key);
  if (existing && !opts.refresh) return existing;
  const result = backend.models(opts);
  active.set(key, result);
  const clear = () => { if (active.get(key) === result) active.delete(key); };
  void result.then(clear, clear);
  return result;
}

export function useModels(provider: Provider, { sessionId, profileId, cwd, mode }: ModelContext = {}, enabled = true) {
  const backend = useBackend();
  const sessionProfile = useApp(s => sessionId ? s.sessions.find(x => x.id === sessionId)?.profileId : undefined);
  const key = JSON.stringify([provider, sessionId, sessionProfile, profileId, cwd, mode]);
  const [state, setState] = useState<{ backend: Backend; key: string; list?: ModelList; loading?: boolean; error?: string }>();
  const generation = useRef(0);

  const fetch = useCallback((refresh: boolean) => {
    const g = ++generation.current;
    setState(old => ({ backend, key, list: old?.key === key && old.backend === backend ? old.list : undefined, loading: true }));
    load(backend, key, { provider, sessionId, profileId, cwd, mode, refresh }).then(
      list => { if (g === generation.current) setState({ backend, key, list }); },
      error => { if (g === generation.current) setState({ backend, key, error: error instanceof Error ? error.message : String(error) }); },
    );
  }, [backend, key, provider, sessionId, profileId, cwd, mode]);

  useEffect(() => {
    if (enabled) fetch(false);
    return () => { generation.current++; };
  }, [enabled, fetch]);

  const current = state?.key === key && state.backend === backend ? state : undefined;
  return { list: current?.list, loading: current?.loading, error: current?.error, refresh: () => fetch(true) };
}

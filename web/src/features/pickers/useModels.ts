// Cache the host's model catalogue per provider, session, and Codex account.
import { useCallback, useEffect, useRef, useState } from 'react';
import { useBackend, type Backend } from '@/backend/backend';
import type { ModelList, Provider } from '@/backend/types';
import { useApp } from '@/store/app';
import { isCodexAccount } from './supplierSwitch';

const cache = new WeakMap<Backend, Map<string, Promise<ModelList>>>();

function load(backend: Backend, provider: Provider, sessionId: string | undefined, codexAccountId: string | undefined, refresh: boolean) {
  let byKey = cache.get(backend);
  if (!byKey) cache.set(backend, (byKey = new Map()));
  const key = JSON.stringify([provider, sessionId, codexAccountId]);
  let p = byKey.get(key);
  if (!p || refresh) {
    p = backend.models({ provider, ...(sessionId ? { sessionId } : { codexAccountId }), ...(refresh ? { refresh: true } : {}) });
    byKey.set(key, p);
    p.catch(() => { if (byKey!.get(key) === p) byKey!.delete(key); });
  }
  return p;
}

export function useModels(provider: Provider, sessionId?: string, enabled = true, selectedAccount?: string) {
  const backend = useBackend();
  const sessionProfile = useApp(s => provider === 'codex' && sessionId ? s.sessions.find(x => x.id === sessionId)?.profileId : undefined);
  const codexAccountId = sessionId ? isCodexAccount(sessionProfile) ? sessionProfile : undefined : selectedAccount;
  const [list, setList] = useState<ModelList>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const generation = useRef(0);

  const fetch = useCallback((refresh: boolean) => {
    const g = ++generation.current;
    setLoading(true); setError('');
    load(backend, provider, sessionId, codexAccountId, refresh)
      .then(l => { if (g === generation.current) setList(l); })
      .catch(e => { if (g === generation.current) setError(e instanceof Error ? e.message : String(e)); })
      .finally(() => { if (g === generation.current) setLoading(false); });
  }, [backend, provider, sessionId, codexAccountId]);

  useEffect(() => {
    setList(undefined);
    if (enabled) fetch(false);
    return () => { generation.current++; };
  }, [enabled, fetch]);

  return { list, loading, error, refresh: () => fetch(true) };
}

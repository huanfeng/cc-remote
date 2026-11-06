import { useEffect, useState } from 'react';
import { useBackend } from '@/backend/backend';
import type { NativeSession, Provider } from '@/backend/types';

/**
 * Terminal history for one provider, fetched when shown. The list is stored
 * with the provider it belongs to, so after switching provider the old list is
 * never shown (or clicked) as if it were the new one's.
 */
export function useNativeSessions(provider: Provider) {
  const backend = useBackend();
  const [state, setState] = useState<{ provider: Provider; list?: NativeSession[]; error?: string }>({ provider });
  useEffect(() => {
    let live = true;
    setState(s => (s.provider === provider ? { ...s, error: undefined } : { provider }));
    backend.nativeSessions(provider)
      .then(list => { if (live) setState({ provider, list }); })
      .catch(e => { if (live) setState({ provider, error: e instanceof Error ? e.message : String(e) }); });
    return () => { live = false; };
  }, [backend, provider]);
  const current = state.provider === provider ? state : { provider };
  return { list: current.list, error: current.error, loading: !current.list && !current.error };
}

import { useEffect, useState } from 'react';
import { useBackend } from '@/backend/backend';
import type { ProfileUsageReply, Provider } from '@/backend/types';

/** One read of the host's cache per open picker, never one provider probe per row. */
export function useSupplierUsage(provider: Provider, enabled = true) {
  const backend = useBackend();
  const [reading, setReading] = useState<{ backend: typeof backend; provider: Provider; value: ProfileUsageReply | null | undefined }>();
  useEffect(() => {
    if (!enabled || provider !== 'claude' || !backend.profileUsage) return;
    let live = true;
    setReading({ backend, provider, value: undefined });
    backend.profileUsage(provider).then(value => { if (live) setReading({ backend, provider, value }); }, () => {
      if (live) setReading({ backend, provider, value: { profiles: {} } });
    });
    return () => { live = false; };
  }, [backend, provider, enabled]);
  if (provider !== 'claude' || !backend.profileUsage) return null;
  return reading?.backend === backend && reading.provider === provider ? reading.value : undefined;
}

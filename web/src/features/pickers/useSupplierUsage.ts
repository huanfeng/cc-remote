import { useEffect, useState } from 'react';
import { useBackend } from '@/backend/backend';
import type { ProfileUsageReply, Provider } from '@/backend/types';

/** One read of the host's cache per open picker, never one provider probe per row. */
export function useSupplierUsage(provider: Provider, enabled = true) {
  const backend = useBackend();
  const [reading, setReading] = useState<{ backend: typeof backend; provider: Provider; value: ProfileUsageReply | null | undefined }>();
  const [, tick] = useState(0);
  useEffect(() => {
    if (!enabled || provider !== 'claude') return;
    // Advance relative timestamps without fetching or probing the provider again.
    const timer = window.setInterval(() => tick(value => value + 1), 30_000);
    return () => window.clearInterval(timer);
  }, [enabled, provider]);
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

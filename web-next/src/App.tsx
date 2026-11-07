// Picks the backend and gates the app on being signed in. `?mock` (dev server,
// or a build with VITE_MOCK=1) runs the in-memory demo host. Otherwise the page
// talks to a real host: the one that served it (same origin), or the current
// host of the standalone page / desktop app. The token comes from storage;
// a 401 at any time signs the page out (connection.ts).
import { useEffect, useState } from 'react';
import { Tooltip as TooltipPrimitive } from 'radix-ui';
import { WifiOff } from 'lucide-react';
import { BackendContext, type Backend } from '@/backend/backend';
import { BackendError } from '@/backend/errors';
import { HttpBackend } from '@/backend/http/httpBackend';
import { useApp } from '@/store/app';
import { releaseMissing } from '@/store/transcripts';
import { STANDALONE, apiBase, connectError, hostKey, relocateHost, useHosts } from '@/lib/hosts';
import { applyIdentity } from '@/lib/identity';
import { useApplyTheme } from '@/lib/theme';
import { useIsMobile } from '@/lib/hooks';
import { lang, t } from '@/lib/i18n';
import { trackVisualViewport } from '@/domain/viewport';
import { toast } from '@/components/ui/toast';
import { DesktopShell } from '@/app/DesktopShell';
import { MobileShell } from '@/app/MobileShell';
import { Surfaces } from '@/features/dialogs/Surfaces';
import { useGlobalShortcuts } from '@/app/shortcuts';
import { useDrafts } from '@/features/composer/drafts';
import { useRequestDrafts } from '@/features/requests/drafts';
import { syncDraftStore } from '@/lib/draftSync';
import { useConnection } from '@/features/auth/connection';
import { ConnectingScreen, LoginScreen } from '@/features/auth/LoginScreen';
import { PlainHttpBanner } from '@/features/auth/PlainHttpBanner';
import { StandaloneHome } from '@/features/standalone/StandaloneHome';

const env = import.meta.env;
const mockMode = (env.DEV || env.VITE_MOCK === '1') && typeof location !== 'undefined' && new URLSearchParams(location.search).has('mock');

const RETRY_MS = 10_000;

export function App() {
  useApplyTheme();
  useEffect(() => trackVisualViewport(), []);
  useEffect(() => scopeDraftsToHost(), []);
  return mockMode ? <MockApp /> : <HostApp />;
}

/** The demo: MockBackend and its saved-host list, loaded on demand so builds without the flag leave them out. */
function MockApp() {
  const [backend, setBackend] = useState<Backend>();
  useEffect(() => {
    let live = true;
    void Promise.all([import('@/backend/mock/mockBackend'), import('@/backend/mock/fixtures')]).then(([m, f]) => {
      if (!live) return;
      useHosts.getState().set({ hosts: f.demoHosts, currentId: f.demoHosts[0].id });
      setBackend(new m.MockBackend());
    });
    return () => { live = false; };
  }, []);
  return backend ? <Connected backend={backend} /> : null;
}

function HostApp() {
  const token = useConnection(s => s.token);
  const phase = useConnection(s => s.phase);
  const error = useConnection(s => s.error);
  const attempt = useConnection(s => s.attempt);
  const [backend, setBackend] = useState<HttpBackend>();

  // One backend per token: a new sign-in starts from nothing cached.
  useEffect(() => {
    if (!token) { setBackend(undefined); return; }
    const b = new HttpBackend({ baseUrl: apiBase, token, lang, onUnauthorized: () => useConnection.getState().unauthorized() });
    setBackend(b);
    return () => b.dispose();
  }, [token]);

  // Checks the token with /api/info. A host that does not answer (restarting, a
  // tunnel moving) is tried again every 10 s; a refused token is not.
  useEffect(() => {
    if (!backend) return;
    let live = true;
    let timer = 0;
    backend.info().then(info => {
      if (!live) return;
      useApp.getState().setInfo(info);
      useConnection.getState().connected();
    }, (e: unknown) => {
      if (!live) return;
      const status = e instanceof BackendError ? e.status : 0;
      if (status === 401) { useConnection.getState().unauthorized(); return; }
      // No answer, or a proxy in front of the host saying it is down: both pass, so try again.
      const unreachable = !status || status === 502 || status === 503 || status === 504;
      const show = (message: string) => useConnection.getState().failed(message, unreachable);
      show(unreachable ? connectError() : e instanceof Error ? e.message : String(e));
      if (!unreachable) return;
      // A host found through an entry may have moved (a tunnel's port changed), or the LAN address
      // may have stopped answering: ask again; the page reopens there when it moved.
      if (STANDALONE) void relocateHost().then(moved => { if (live && typeof moved === 'string') show(moved); });
      timer = window.setTimeout(() => useConnection.getState().retry(), RETRY_MS);
    });
    return () => { live = false; window.clearTimeout(timer); };
  }, [backend, attempt]);

  if (phase === 'ready' && backend) return <Connected backend={backend} />;
  if (STANDALONE) return <StandaloneHome />;
  if (phase === 'connecting' && !error) return <ConnectingScreen />;
  return <LoginScreen />;
}

/** The app proper, against a backend that answered. */
function Connected({ backend }: { backend: Backend }) {
  const info = useApp(s => s.info);
  // The session list poll failed: shown until the next poll answers.
  const [offline, setOffline] = useState('');

  useEffect(() => {
    let live = true;
    setOffline('');
    if (!useApp.getState().info) {
      backend.info()
        .then(i => { if (live) useApp.getState().setInfo(i); })
        .catch(e => { if (live) toast(t(`Could not reach the host: ${e instanceof Error ? e.message : e}`, `无法连接宿主：${e instanceof Error ? e.message : e}`)); });
    }
    const stop = backend.watchSessions(s => {
      useApp.getState().setSessions(s);
      setOffline('');
      // Sessions gone from the host (deleted here or elsewhere) release their cache and draft.
      releaseMissing(new Set(s.map(x => x.id)));
    }, e => {
      if (!live) return;
      setOffline(e.status ? e.message : connectError());
      // The list stopped answering: on the standalone page the host may have moved (LAN, tunnel port).
      if (!STANDALONE || e.status) return;
      void relocateHost().then(moved => { if (live && typeof moved === 'string') toast(moved); });
    });
    return () => { live = false; stop(); };
  }, [backend]);

  // The host's name, colour and icon: tab title, favicon, --host-color.
  useEffect(() => {
    if (info?.name && info.color) applyIdentity({ name: info.name, color: info.color, icon: info.icon || '' });
  }, [info?.name, info?.color, info?.icon]);

  return (
    <BackendContext.Provider value={backend}>
      <TooltipPrimitive.Provider delayDuration={400} skipDelayDuration={200}>
        <div className="flex h-full min-h-0 flex-col">
          <PlainHttpBanner />
          {offline && (
            <div role="status" className="flex shrink-0 items-center gap-2 border-b border-border/60 bg-destructive-surface px-3 py-1.5 text-ui-sm text-destructive-surface-foreground">
              <WifiOff className="size-4 shrink-0" />
              <span className="min-w-0 flex-1 truncate" title={offline}>{offline}</span>
              <span className="shrink-0 text-ui-xs opacity-80">{t('Retrying…', '正在重试…')}</span>
            </div>
          )}
          <div className="min-h-0 flex-1"><Shell /></div>
        </div>
        <Surfaces />
      </TooltipPrimitive.Provider>
    </BackendContext.Provider>
  );
}

function Shell() {
  const mobile = useIsMobile();
  useGlobalShortcuts();
  return mobile ? <MobileShell /> : <DesktopShell />;
}

/**
 * Composer and request-card drafts per host (a standalone page's hosts never see
 * each other's), kept in step with the page's other tabs, removals included.
 */
function scopeDraftsToHost() {
  const stops = [syncDraftStore(useDrafts, hostKey('ccn-drafts')), syncDraftStore(useRequestDrafts, hostKey('ccn-request-drafts'))];
  return () => { for (const stop of stops) stop(); };
}

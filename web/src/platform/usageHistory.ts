import { invoke } from '@tauri-apps/api/core';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { DESKTOP, DESKTOP_TAB } from './desktop';
import { requestDesktopHost } from './desktopTabs';
import { checkHost, connectError, rememberHost } from '@/lib/hosts';
import { lang, t } from '@/lib/i18n';
import { toast } from '@/components/ui/toast';
import { usageHistoryHash } from '@/domain/usageHistoryRoute';

type HistoryRequest = { host?: string; dataDir?: string; account?: string };
let installed = false;

/** Install once for the app lifetime, including React StrictMode's effect replay. */
export function installUsageHistoryNavigation() {
  if (!DESKTOP || DESKTOP_TAB || installed || getCurrentWindow().label !== 'main') return;
  installed = true;
  let pending = false;
  let running = false;
  const drain = async () => {
    pending = true;
    if (running) return;
    running = true;
    try {
      while (pending) {
        pending = false;
        const request = await invoke<HistoryRequest | null>('take_usage_history_request');
        if (!request) continue;
        const connection = await invoke<{ url: string; token: string; account?: string }>('usage_history_connection', { request, lang });
        const info = await checkHost(connection.url, connection.token);
        if (!info.ok) throw new Error(connectError(info.status));
        const host = rememberHost({ url: connection.url, token: connection.token, name: info.name, color: info.color, icon: info.icon });
        const hash = usageHistoryHash(connection.account);
        // The shell never becomes a host page. Credentials may have changed,
        // so an existing tab must rebuild its backend before opening history.
        requestDesktopHost(host.id, hash, true);
      }
    } catch (error) {
      toast(t('Could not open usage history: ', '无法打开用量历史：') + (error instanceof Error ? error.message : String(error)));
    } finally { running = false; }
  };
  void getCurrentWindow().listen('usage-history-requested', () => { void drain(); })
    .then(() => drain())
    .catch(() => { installed = false; toast(t('Could not listen for tray requests', '无法接收托盘请求')); });
}

// The bearer token travels in clear text when the host is reached over plain
// http from another machine. A standalone page also checks its own address: its
// storage holds every host's token. Dismissed for the rest of this tab's life.
import { useState } from 'react';
import { TriangleAlert, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { STANDALONE, apiBase, hostKey } from '@/lib/hosts';
import { t } from '@/lib/i18n';

const KEY = () => hostKey('cc-plain-http-dismissed');

function plainAddress(): string | undefined {
  const places = [new URL(apiBase || location.href), ...(STANDALONE ? [new URL(location.href)] : [])];
  return places.find(where => {
    const host = where.hostname.replace(/^\[|\]$/g, '');
    return where.protocol === 'http:' && !(host === 'localhost' || host === '::1' || /^127\./.test(host));
  })?.host;
}

export function PlainHttpBanner() {
  const [address] = useState(plainAddress);
  const [shown, setShown] = useState(() => {
    try { if (sessionStorage.getItem(KEY()) === '1') return false; } catch { /* storage unavailable */ }
    return !!address;
  });
  if (!shown || !address) return null;
  const dismiss = () => {
    setShown(false);
    try { sessionStorage.setItem(KEY(), '1'); } catch { /* storage unavailable: shown again after a reload */ }
  };
  return (
    <div role="status" className="flex shrink-0 items-start gap-2 border-b border-border/60 bg-warning-surface px-3 py-2 pt-safe text-ui-sm text-warning-surface-foreground">
      <TriangleAlert className="mt-0.5 size-4 shrink-0" />
      <p className="min-w-0 flex-1 leading-5">
        {t(`You are connected over plain HTTP (${address}); the access token and session content travel the network unencrypted. Use an HTTPS reverse proxy or a VPN instead.`,
          `当前通过明文 HTTP 访问（${address}），访问令牌和会话内容会在网络中明文传输。建议改用 HTTPS 反向代理或 VPN。`)}
      </p>
      <Button variant="ghost" size="icon-sm" className="max-md:size-9" aria-label={t('Dismiss plain HTTP warning', '关闭明文访问提示')} onClick={dismiss}>
        <X className="size-3.5" />
      </Button>
    </div>
  );
}

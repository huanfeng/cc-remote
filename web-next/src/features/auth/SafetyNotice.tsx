// The access token lets Claude Code run any command on the host's computer, so
// before the first one is entered the page says where it goes and who could
// read it. Confirmed once per browser (and page address): `cc-safety-ack:v1`.
import { useState } from 'react';
import { ShieldAlert } from 'lucide-react';
import { Checkbox } from '@/components/ui/checkbox';
import { cn } from '@/components/lib/utils';
import { STANDALONE } from '@/lib/hosts';
import { t } from '@/lib/i18n';

const KEY = 'cc-safety-ack:v1';
const OFFICIAL = 'https://huanfeng.github.io/cc-remote/';
const REPO = 'https://github.com/huanfeng/cc-remote';

const acknowledged = () => { try { return localStorage.getItem(KEY) === '1'; } catch { return false; } };

/** Whether the notice was confirmed (the setter remembers it on this device), and
 * whether to show it: it stays on the page it was confirmed on, until the page changes. */
export function useSafetyAck(): { ack: boolean; show: boolean; setAck: (value: boolean) => void } {
  const [ack, set] = useState(acknowledged);
  const [show] = useState(() => !ack);
  const setAck = (value: boolean) => {
    set(value);
    try { if (value) localStorage.setItem(KEY, '1'); else localStorage.removeItem(KEY); } catch { /* private window: asked again next time */ }
  };
  return { ack, show, setAck };
}

const official = () => (location.origin + location.pathname).startsWith(OFFICIAL);
/** Served over plain http from another machine: the token crosses the network in clear. */
export const plainHttpPage = () => location.protocol === 'http:' && !/^(localhost|127\.|\[::1\]$)/.test(location.hostname);

/** The notice with its "I understand" box; the caller keeps its own action disabled until checked. */
export function SafetyNotice({ checked, onChange }: { checked: boolean; onChange: (value: boolean) => void }) {
  const items: { text: string; tone?: 'ok' | 'warn'; link?: boolean }[] = [
    { text: t('An access token gives full control of the host: through it, Claude Code can read files and run any command on that computer.', '访问令牌等同于宿主电脑的控制权：凭它可以让 Claude Code 读取文件、执行任意命令。') },
    ...(STANDALONE ? [
      { text: t('CC Remote is open source. This page is static: it talks only to the hosts you add and sends nothing anywhere else.', 'CC Remote 完全开源。本页面是纯静态前端，只和你添加的宿主直接通信，不会把数据发送到其它地方。'), link: true },
      { text: t('But whoever deploys a copy controls its code: a modified copy can quietly record the tokens you enter. Use only the official page or one you deployed yourself, and check the address bar.', '但页面代码由部署者决定：被改动过的副本可以悄悄记下你输入的令牌。请只使用官方实例或你自己部署的版本，并核对地址栏。') },
      official()
        ? { text: t(`This is the official page (${OFFICIAL}).`, `当前是官方实例（${OFFICIAL}）。`), tone: 'ok' as const }
        : { text: t(`This page (${location.origin}) is not the official one (${OFFICIAL}); continue only if you trust whoever deployed it.`, `当前页面（${location.origin}）不是官方实例（${OFFICIAL}），请确认部署者可信再继续。`), tone: 'warn' as const },
    ] : [
      { text: t('This page is served by your cc-remote host. Make sure the address is your own host, and never enter the token on a page someone else sent you.', '本页面由你的 cc-remote 宿主提供。请确认地址是你自己的宿主，不要在别人发来的链接或页面里输入令牌。') },
      ...(plainHttpPage() ? [{ text: t('This connection is plain HTTP: others on the same network can read the token. Use it only on a network you trust, or reach the host over HTTPS.', '当前是未加密的 HTTP 连接，同一网络中的其他人可以截获令牌。请只在可信网络中使用，或通过 HTTPS 访问宿主。'), tone: 'warn' as const }] : []),
    ]),
  ];
  return (
    <div role="note" className="rounded-xl border border-border/70 bg-surface px-4 py-3">
      <div className="flex items-center gap-2 text-ui-base font-medium">
        <ShieldAlert className="size-4 text-warning" />
        {t('Before you enter a token', '输入令牌前请确认')}
      </div>
      <ul className="mt-2 flex list-disc flex-col gap-1.5 pl-5 text-ui-sm leading-5 text-foreground-subtle">
        {items.map(item => (
          <li key={item.text} className={cn(item.tone === 'warn' && 'text-warning', item.tone === 'ok' && 'text-success')}>
            {item.text}
            {item.link && <> <a href={REPO} target="_blank" rel="noreferrer" className="text-icon-blue underline-offset-2 hover:underline">{t('Source', '源码')}</a></>}
          </li>
        ))}
      </ul>
      <label className="mt-3 flex cursor-pointer items-center gap-2 text-ui-sm font-medium">
        <Checkbox checked={checked} onCheckedChange={v => onChange(v === true)} className="max-md:size-5" />
        {t('I understand, and I trust this page', '我已了解以上风险，并确认此页面可信')}
      </label>
    </div>
  );
}

// The standalone page while it is not connected: what the page is, with a
// button to add a host, or the current host's connection state. It never opens
// the add form by itself: on an unsteady network the page would sit on a form
// asking for the token again. An unreachable host is tried again every 10 s.
import { useState } from 'react';
import { ArrowRight, Pencil, Plus, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { cn } from '@/components/lib/utils';
import { useIsMobile } from '@/lib/hooks';
import { currentHost, loadHosts, shownAddress, shownColor, shownIcon, shownName, switchHost } from '@/lib/hosts';
import { t } from '@/lib/i18n';
import { ErrorText } from '@/features/dialogs/common';
import { HostGlyph } from '@/features/settings/parts';
import { AuthFrame } from '@/features/auth/AuthFrame';
import { SafetyNotice, useSafetyAck } from '@/features/auth/SafetyNotice';
import { useConnection } from '@/features/auth/connection';
import { HostFormDialog } from './HostForm';
import { AllowHint, HideAddressesButton, useHiddenAddresses } from './parts';
import { DESKTOP } from '@/platform/desktop';
import { LocalServicePanel } from '@/features/desktop/LocalService';

/** The desktop app's card for the service on this computer. */
function ThisComputer() {
  return (
    <div className="rounded-xl border border-border/70 bg-surface px-4 py-3">
      <div className="mb-2 text-ui-sm font-medium">{t('This computer', '本机服务')}</div>
      <LocalServicePanel connectLabel={t('Connect to this computer', '连接本机宿主')} />
    </div>
  );
}

export function StandaloneHome() {
  const [form, setForm] = useState<'' | 'add' | 'edit'>('');
  const safety = useSafetyAck();
  const [hidden, setHidden] = useHiddenAddresses();
  const mobile = useIsMobile();
  const phase = useConnection(s => s.phase);
  const error = useConnection(s => s.error);
  const retrying = useConnection(s => s.retrying);
  const token = useConnection(s => s.token);
  const retry = useConnection(s => s.retry);
  const host = currentHost;
  const others = loadHosts().hosts.filter(x => x.id !== host?.id);
  const big = cn(mobile && 'h-10');

  const dialog = (
    <HostFormDialog open={!!form} onOpenChange={open => { if (!open) setForm(''); }} editing={form === 'edit' ? host : undefined} mobile={mobile} />
  );

  if (!host) {
    return (
      <AuthFrame wide>
        <div className="flex flex-col gap-5">
          <div className="pr-20">
            <HostGlyph icon="code" color="#293c2e" className="size-11 rounded-xl [&_svg]:size-5" />
            <h1 className="mt-3 text-ui-xl font-semibold tracking-tight">CC Remote</h1>
            <p className="mt-1 text-ui-sm leading-5 text-foreground-subtle">
              {DESKTOP
                ? t('Use Claude Code and Codex on your computers: follow sessions, send messages, answer approvals and questions. Connect to the cc-remote host on this computer, or add hosts elsewhere.', '使用你电脑上的 Claude Code 与 Codex：查看会话、发送消息、回答审批和提问。可以连接本机的 cc-remote 宿主，也可以添加其它电脑上的宿主。')
                : t('Use Claude Code on your computer from this browser: follow sessions, send messages, answer approvals and questions. This page is only the frontend; it keeps the hosts you add and their tokens on this device.', '在这个浏览器里使用你电脑上的 Claude Code：查看会话、发送消息、回答审批和提问。此页面只是前端，你添加的宿主和令牌只保存在本设备。')}
            </p>
          </div>
          {DESKTOP && <ThisComputer />}
          {!DESKTOP && <ol className="flex flex-col gap-2 text-ui-sm">
            {[
              t('Run a cc-remote host on the computer with Claude Code.', '在装有 Claude Code 的电脑上运行 cc-remote 宿主。'),
              t('Let this page in on the host (below).', '在宿主上允许本页面访问（见下方）。'),
              t('Add the host here with its address and access token.', '在这里添加宿主，填写地址和访问令牌。'),
            ].map((step, i) => (
              <li key={step} className="flex items-start gap-2.5">
                <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-surface text-ui-xs font-medium text-foreground-subtle">{i + 1}</span>
                <span className="leading-5">{step}</span>
              </li>
            ))}
          </ol>}
          {safety.show && <SafetyNotice checked={safety.ack} onChange={safety.setAck} />}
          <Button size="lg" variant={DESKTOP ? 'outline' : 'default'} className={cn('w-full', big)} disabled={!safety.ack} onClick={() => setForm('add')}>
            {DESKTOP ? t('Add a host by address', '按地址添加宿主') : t('Add a host', '添加宿主')}<ArrowRight className="size-4" />
          </Button>
          {!DESKTOP && <AllowHint />}
        </div>
        {dialog}
      </AuthFrame>
    );
  }

  const connecting = phase === 'connecting' && !error;
  // A host whose token was refused (or cleared): only editing it helps.
  const noToken = !token;
  return (
    <AuthFrame wide>
      <div className="flex flex-col gap-5">
        <span className="text-ui-xs font-medium text-foreground-subtlest">CC Remote</span>
        <div className="flex items-center gap-3">
          <HostGlyph icon={shownIcon(host) || 'code'} color={shownColor(host)} className="size-10 rounded-xl" />
          <div className="min-w-0 flex-1">
            <div className="truncate text-ui-lg font-semibold">{shownName(host)}</div>
            <div className="truncate text-ui-sm text-foreground-subtlest">{shownAddress(host.entry || host.url, hidden)}</div>
          </div>
          <HideAddressesButton hidden={hidden} set={setHidden} />
        </div>

        {connecting ? (
          <p role="status" className="flex items-center gap-2 text-ui-sm text-foreground-subtle"><Spinner className="size-3.5" />{t('Connecting…', '正在连接…')}</p>
        ) : (
          <div className="flex flex-col gap-1.5">
            <ErrorText>{error || (noToken ? t('Signed out of this host. Edit it to enter its access token.', '已退出此宿主。编辑宿主以重新输入访问令牌。') : '')}</ErrorText>
            {retrying && <p className="text-ui-xs text-foreground-subtlest">{t('Trying again every 10 seconds; the page opens by itself once the host answers.', '每 10 秒自动重试，宿主恢复后自动进入。')}</p>}
          </div>
        )}

        <div className="flex flex-wrap gap-2">
          {!noToken && (
            <Button size="lg" className={big} disabled={phase === 'connecting'} onClick={retry}>
              {phase === 'connecting' ? <Spinner className="size-3.5" /> : <RefreshCw className="size-4" />}{t('Try again now', '立即重试')}
            </Button>
          )}
          <Button variant={noToken ? 'default' : 'outline'} size="lg" className={big} onClick={() => setForm('edit')}>
            <Pencil className="size-4" />{t('Edit host', '编辑宿主')}
          </Button>
        </div>

        {DESKTOP && <ThisComputer />}

        {others.length > 0 && (
          <div className="flex flex-col gap-1">
            <span className="text-ui-xs font-medium text-foreground-subtlest">{t('Other hosts', '其他宿主')}</span>
            {others.map(x => (
              <button key={x.id} type="button" onClick={() => switchHost(x.id)}
                className="flex items-center gap-2.5 rounded-lg px-2 py-1.5 text-left hover:bg-surface-hover max-md:py-2.5">
                <HostGlyph icon={shownIcon(x) || 'code'} color={shownColor(x)} className="size-6 rounded-md [&_svg]:size-3.5" />
                <span className="min-w-0 flex-1 truncate text-ui-base">{shownName(x)}</span>
                <span className="max-w-[45%] truncate text-ui-xs text-foreground-subtlest">{shownAddress(x.entry || x.url, hidden)}</span>
              </button>
            ))}
          </div>
        )}

        <Button variant="ghost" size="lg" className={cn('self-start', big)} onClick={() => setForm('add')}>
          <Plus className="size-4" />{t('Add host', '添加宿主')}
        </Button>
      </div>
      {dialog}
    </AuthFrame>
  );
}

// The desktop app's view of the cc-remote service on this computer: whether it
// is installed and running, start / stop / restart, and "connect" (adds it as a
// host with the token from its data directory, then opens it). Shown on the
// home screen before a host is added, and as the "This computer" settings section.
import { useCallback, useEffect, useState } from 'react';
import { Check, Copy, Play, PlugZap, RefreshCw, RotateCw, Square } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { cn } from '@/components/lib/utils';
import { copyText } from '@/domain/clipboard';
import { checkHost, connectError, currentHost, findHost, rememberHost, reopen, updateHost } from '@/lib/hosts';
import { t } from '@/lib/i18n';
import { ErrorText, errorMessage } from '@/features/dialogs/common';
import { INSTALL_COMMAND, localServiceAction, localServiceStatus, localServiceToken, type LocalService, type ServiceAction } from '@/platform/localService';

export function useLocalService() {
  const [service, setService] = useState<LocalService>();
  const [busy, setBusy] = useState<ServiceAction | 'connect' | 'status' | ''>('status');
  const [error, setError] = useState('');

  const refresh = useCallback(async () => {
    setBusy('status');
    try { setService(await localServiceStatus()); setError(''); } catch (e) { setError(errorMessage(e)); } finally { setBusy(''); }
  }, []);
  useEffect(() => { void refresh(); }, [refresh]);

  const act = async (action: ServiceAction) => {
    setBusy(action);
    setError('');
    try { await localServiceAction(action); } catch (e) { setError(errorMessage(e)); }
    try { setService(await localServiceStatus()); } catch { /* the error above says enough */ }
    setBusy('');
  };

  /** Adds the local host (or refreshes its token) and opens it. */
  const connect = async () => {
    if (!service?.available || !service.url) return;
    setBusy('connect');
    setError('');
    try {
      const url = service.url.replace(/\/+$/, '');
      const token = await localServiceToken();
      const reply = await checkHost(url, token);
      if (!reply.ok) throw new Error(connectError(reply.status));
      const old = findHost(url);
      if (old) {
        updateHost(old.id, { token });
        if (old.id !== currentHost?.id) reopen(rememberHost({ ...old, token }).id);
        return;
      }
      reopen(rememberHost({ url, token, name: reply.name, color: reply.color, icon: reply.icon }).id);
    } catch (e) {
      setError(errorMessage(e));
      setBusy('');
    }
  };

  return { service, busy, error, refresh, act, connect };
}

function StateDot({ tone }: { tone: 'ok' | 'off' | 'none' }) {
  return <span className={cn('size-2 shrink-0 rounded-full', tone === 'ok' ? 'bg-success' : tone === 'off' ? 'bg-warning' : 'bg-foreground-subtlest')} />;
}

function InstallHint() {
  const [copied, setCopied] = useState(false);
  const copy = async () => { try { await copyText(INSTALL_COMMAND); setCopied(true); window.setTimeout(() => setCopied(false), 1600); } catch { /* clipboard refused */ } };
  return (
    <div className="flex flex-col gap-1.5 text-ui-xs leading-5 text-foreground-subtlest">
      <span>{t('Install it in a terminal on this computer, then refresh:', '在本机终端运行以下命令安装，然后刷新：')}</span>
      <button type="button" onClick={() => void copy()} title={t('Copy', '复制')}
        className="flex items-start gap-2 rounded-lg border border-border/70 bg-surface px-2.5 py-1.5 text-left hover:bg-surface-hover">
        <code className="min-w-0 flex-1 break-all font-mono text-ui-xs text-foreground">{INSTALL_COMMAND}</code>
        <span className="flex shrink-0 items-center gap-1 text-foreground-subtle">
          {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
          {copied ? t('Copied', '已复制') : t('Copy', '复制')}
        </span>
      </button>
    </div>
  );
}

/** The local service: state line, actions, and what to do when it is missing. */
export function LocalServicePanel({ className, connectLabel }: { className?: string; connectLabel?: string }) {
  const { service, busy, error, refresh, act, connect } = useLocalService();
  const loading = busy === 'status' && !service;
  const version = service?.available ? service.status?.version || service.config?.version : undefined;
  const isCurrent = !!(service?.available && service.url && currentHost && findHost(service.url.replace(/\/+$/, ''))?.id === currentHost.id);

  let tone: 'ok' | 'off' | 'none' = 'none';
  let line = t('Checking…', '正在检查…');
  if (service && !service.available) line = t('cc-remote is not installed on this computer', '本机未安装 cc-remote');
  else if (service?.available && !service.installed) { line = t('cc-remote is here, but its background service is not installed', '已有 cc-remote，但未安装后台服务'); tone = 'off'; }
  else if (service?.available && service.running) { line = t(`Running at ${service.url}`, `运行中：${service.url}`); tone = 'ok'; }
  else if (service?.available) { line = t('Installed, not running', '已安装，未运行'); tone = 'off'; }

  const icon = (action: ServiceAction | 'connect', Icon: typeof Play) => (busy === action ? <Spinner className="size-3.5" /> : <Icon className="size-3.5" />);
  return (
    <div className={cn('flex flex-col gap-3', className)}>
      <div className="flex items-center gap-2.5">
        {loading ? <Spinner className="size-3.5" /> : <StateDot tone={tone} />}
        <div className="min-w-0 flex-1">
          <div className="truncate text-ui-sm text-foreground" title={line}>{line}</div>
          {version && <div className="text-ui-xs text-foreground-subtlest">{t(`Version ${version}`, `版本 ${version}`)}</div>}
        </div>
        <Button variant="ghost" size="icon-sm" aria-label={t('Refresh', '刷新')} title={t('Refresh', '刷新')} disabled={!!busy} onClick={() => void refresh()}>
          <RefreshCw className={cn('size-3.5', busy === 'status' && 'animate-spin')} />
        </Button>
      </div>
      {service?.available && service.installed && (
        <div className="flex flex-wrap gap-2">
          {service.running ? (
            <>
              {!isCurrent && (
                <Button disabled={!!busy} onClick={() => void connect()}>
                  {icon('connect', PlugZap)}{connectLabel ?? t('Connect', '连接')}
                </Button>
              )}
              <Button variant="outline" disabled={!!busy} onClick={() => void act('restart')}>{icon('restart', RotateCw)}{t('Restart', '重启')}</Button>
              <Button variant="outline" disabled={!!busy} onClick={() => void act('stop')}>{icon('stop', Square)}{t('Stop', '停止')}</Button>
            </>
          ) : (
            <Button disabled={!!busy} onClick={() => void act('start')}>{icon('start', Play)}{t('Start', '启动')}</Button>
          )}
        </div>
      )}
      {service?.available && !service.installed && (
        <p className="text-ui-xs leading-5 text-foreground-subtlest">
          {t('Run "cc-remote service install" in a terminal to install the service, then refresh.', '在终端运行 “cc-remote service install” 安装后台服务，然后刷新。')}
        </p>
      )}
      {service && !service.available && <InstallHint />}
      {service?.available && service.statusError && <p className="text-ui-xs text-warning">{service.statusError}</p>}
      <ErrorText>{error}</ErrorText>
    </div>
  );
}

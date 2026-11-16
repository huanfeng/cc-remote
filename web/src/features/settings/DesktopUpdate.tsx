import { useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { GroupCard, Row, SurfaceShell } from '@/features/dialogs/SurfaceShell';
import { ConfirmDialog } from '@/features/dialogs/common';
import { t } from '@/lib/i18n';
import { DESKTOP, DESKTOP_TAB } from '@/platform/desktop';
import { checkDesktopUpdate, installDesktopUpdate, openDesktopAbout, subscribeDesktopAbout, subscribeDesktopUpdate, useDesktopUpdate } from '@/platform/desktopUpdate';
import { GroupTitle, HostGlyph } from './parts';
import { useIsMobile } from '@/lib/hooks';

function UpdateControls() {
  const { status, connectionError } = useDesktopUpdate();
  const [confirm, setConfirm] = useState(false);
  const busy = status && ['checking', 'downloading', 'installing'].includes(status.phase);
  const available = !!status?.version;
  let message = status?.phase === 'updated' ? t('You have the latest desktop version.', '桌面端已是最新版本。')
    : status?.phase === 'checking' ? t('Checking for updates…', '正在检查更新…')
    : status?.phase === 'installing' ? t('Installing and restarting…', '正在安装并重启…')
    : status?.phase === 'downloading' ? (status.total ? `${t('Downloading', '正在下载')} ${Math.min(100, Math.floor(status.downloaded / status.total * 100))}%` : t('Downloading…', '正在下载…'))
    : available ? t(`Desktop ${status.version} is available.`, `桌面端 ${status.version} 可更新。`) : t('Checks automatically at startup and every six hours.', '启动后自动检查，此后每六小时检查一次。');
  if (connectionError || status?.error) message = connectionError || status?.error || message;
  return <div className="space-y-3">
    <p className="text-ui-sm text-muted-foreground" role="status">{message}</p>
    {status?.notes && <p className="max-h-40 overflow-auto whitespace-pre-wrap text-ui-sm">{status.notes}</p>}
    <div className="flex gap-2">
      <Button size="sm" variant="outline" disabled={!!busy} onClick={() => { void checkDesktopUpdate(); }}>{t('Check for updates', '检查更新')}</Button>
      {available && <Button size="sm" disabled={!!busy} onClick={() => setConfirm(true)}>{t('Download and install', '下载并安装')}</Button>}
    </div>
    <ConfirmDialog open={confirm} onOpenChange={setConfirm} title={t('Install desktop update?', '安装桌面端更新？')}
      description={t('The desktop app will close and restart during installation. The host service keeps running.', '安装时桌面应用将退出并重启，宿主后台继续运行。')}
      confirmLabel={t('Install and restart', '安装并重启')} onConfirm={() => { setConfirm(false); void installDesktopUpdate(); }} />
  </div>;
}

export function DesktopAboutContent() {
  const status = useDesktopUpdate(s => s.status);
  if (!DESKTOP) return null;
  const release = status?.version ?? status?.currentVersion;
  return <div>
    <div className="mb-5 flex items-center gap-4">
      <HostGlyph icon="logo" color="#293c2e" className="size-14 rounded-xl" />
      <div><h2 className="text-ui-xl font-semibold">CC Remote</h2>
        <p className="mt-1 select-text text-ui-sm text-foreground-subtle">{t('Desktop version', '桌面端版本')} {status?.currentVersion ? `v${status.currentVersion}` : '…'}</p>
      </div>
    </div>
    <p className="text-ui-sm leading-5 text-foreground-subtle">{t('Claude Code and Codex sessions across your computers.', '连接各台电脑上的 Claude Code 与 Codex 会话。')}</p>
    <GroupTitle>{t('Updates', '更新')}</GroupTitle><GroupCard>
      <Row label={t('Desktop app and built-in tray', '桌面客户端与内置托盘')} stack><UpdateControls /></Row>
    </GroupCard>
    <p className="mt-3 text-ui-sm text-foreground-subtle">{t('The host service has its own version and updates separately.', '宿主后台有独立的版本号，需要单独更新。')}</p>
    <div className="mt-5 flex flex-wrap gap-x-5 gap-y-2 text-ui-sm">
      <a className="text-primary underline underline-offset-4" href={release ? `https://github.com/huanfeng/cc-remote/releases/tag/v${encodeURIComponent(release)}` : 'https://github.com/huanfeng/cc-remote/releases'} target="_blank" rel="noreferrer">{status?.version ? t('New version release notes', '新版本发布说明') : t('Release notes', '版本发布说明')}</a>
      <a className="text-primary underline underline-offset-4" href="https://github.com/huanfeng/cc-remote" target="_blank" rel="noreferrer">{t('GitHub project', 'GitHub 项目')}</a>
    </div>
  </div>;
}

export function DesktopAboutDialog() {
  const open = useDesktopUpdate(s => s.aboutOpen);
  const mobile = useIsMobile();
  useEffect(() => subscribeDesktopAbout(), []);
  if (!DESKTOP) return null;
  return <SurfaceShell open={open} onOpenChange={aboutOpen => useDesktopUpdate.setState({ aboutOpen })} mobile={mobile} title={t('About & updates', '关于与更新')}>
    <DesktopAboutContent />
  </SurfaceShell>;
}

/** Lives outside host connection screens, including when no host is configured. */
export function DesktopUpdateNotice() {
  const status = useDesktopUpdate(s => s.status);
  const aboutOpen = useDesktopUpdate(s => s.aboutOpen);
  const [dismissed, setDismissed] = useState<string | null>(null);
  useEffect(() => subscribeDesktopUpdate(), []);
  const noticeKey = status?.version ?? (status?.phase === 'error' ? `error-${status.revision}` : null);
  if (!DESKTOP || DESKTOP_TAB || aboutOpen || !status || !noticeKey || (dismissed === noticeKey && status.phase !== 'downloading' && status.phase !== 'installing')) return null;
  return <aside aria-label={t('Desktop update', '桌面端更新')} className="fixed bottom-4 right-4 z-[100] w-96 max-w-[calc(100vw-2rem)] rounded-xl border bg-card p-4 shadow-lg">
    <div className="mb-3 flex items-center justify-between gap-4"><strong>{t('Desktop update', '桌面端更新')}</strong>
      <Button size="sm" variant="ghost" onClick={() => setDismissed(noticeKey)}>{t('Later', '稍后')}</Button></div>
    <p className="mb-3 text-ui-sm text-muted-foreground" role="status">{status.error ?? (status.phase === 'downloading' ? t('Downloading desktop update…', '正在下载桌面端更新…') : status.phase === 'installing' ? t('Installing and restarting…', '正在安装并重启…') : t(`Desktop ${status.version} is available.`, `桌面端 ${status.version} 可更新。`))}</p>
    <Button size="sm" onClick={openDesktopAbout}>{t('About & updates', '关于与更新')}</Button>
  </aside>;
}

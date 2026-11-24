import { useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { GroupCard, Row, SurfaceShell } from '@/features/dialogs/SurfaceShell';
import { ConfirmDialog } from '@/features/dialogs/common';
import { t } from '@/lib/i18n';
import { DESKTOP, DESKTOP_TAB } from '@/platform/desktop';
import {
  checkCoreUpdate, checkDesktopUpdate, installCoreUpdate, installDesktopUpdate, openDesktopAbout,
  refreshCoreUpdate, subscribeCoreUpdate, subscribeDesktopAbout, subscribeDesktopUpdate, useCoreUpdate, useDesktopUpdate,
} from '@/platform/desktopUpdate';
import { GroupTitle, HostGlyph } from './parts';
import { useIsMobile } from '@/lib/hooks';

interface ComponentUpdate {
  id: string;
  name: string;
  current?: string | null;
  message: string;
  notes?: string | null;
  busy: boolean;
  checkLabel?: string;
  check(): void;
  install?: { label: string; run(): void };
}

function displayVersion(version: string): string {
  return /^\d/.test(version) ? `v${version}` : version;
}

function ComponentUpdateRow({ component, installing }: { component: ComponentUpdate; installing: boolean }) {
  return <Row label={component.name} stack description={component.current && <span className="select-text tabular-nums">{displayVersion(component.current)}</span>}>
    <div className="space-y-2.5">
      <p className="break-words text-ui-sm text-muted-foreground" role="status">{component.message}</p>
      {component.notes && <p className="max-h-40 overflow-auto whitespace-pre-wrap text-ui-sm">{component.notes}</p>}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={component.busy} onClick={component.check}>{component.checkLabel ?? t('Check for updates', '检查更新')}</Button>
        {component.install && <Button size="sm" disabled={component.busy || installing} onClick={component.install.run}>{component.install.label}</Button>}
      </div>
    </div>
  </Row>;
}

export function DesktopAboutContent() {
  const { status, connectionError } = useDesktopUpdate();
  const { status: core, connectionError: coreConnectionError } = useCoreUpdate();
  const [confirm, setConfirm] = useState(false);
  useEffect(() => subscribeCoreUpdate(), []);
  if (!DESKTOP) return null;

  const desktopInstalling = status?.phase === 'downloading' || status?.phase === 'installing';
  const coreInstalling = core?.phase === 'installing';
  const desktopMessage = status?.phase === 'updated' ? t('You have the latest desktop version.', '桌面端已是最新版本。')
    : status?.phase === 'checking' ? t('Checking for updates…', '正在检查更新…')
    : status?.phase === 'installing' ? t('Installing and restarting…', '正在安装并重启…')
    : status?.phase === 'downloading' ? (status.total ? `${t('Downloading', '正在下载')} ${Math.min(100, Math.floor(status.downloaded / status.total * 100))}%` : t('Downloading…', '正在下载…'))
    : status?.version ? t(`Desktop ${displayVersion(status.version)} is available.`, `桌面端 ${displayVersion(status.version)} 可更新。`)
    : t('Checks automatically at startup and every six hours.', '启动后自动检查，此后每六小时检查一次。');
  const coreVersion = core?.version ? displayVersion(core.version) : '';
  const installedCore = core?.installedVersion ? displayVersion(core.installedVersion) : '';
  const coreChanged = installedCore && core?.currentVersion && displayVersion(core.currentVersion) !== installedCore;
  const coreMessages = {
    idle: t('Check for updates to the service installed on this computer.', '检查此电脑上安装的 Core 服务是否有更新。'),
    unavailable: t('Core is not installed on this computer.', '本机尚未安装 Core。'),
    unmanaged: t('Core must be installed and run as a local service to update here.', '请将 Core 安装为本机后台服务后在此更新。'),
    checking: t('Checking for updates…', '正在检查更新…'),
    available: core?.running
      ? t(`${coreVersion} is available. Core restarts when sessions are idle. Local connections may briefly drop.`, `${coreVersion} 可更新。Core 将在会话空闲后重启，本机连接可能短暂中断。`)
      : t(`${coreVersion} is available. Updating also starts the local Core service.`, `${coreVersion} 可更新。更新后会启动本机 Core 服务。`),
    installing: t('Downloading, verifying and installing Core…', '正在下载、校验并安装 Core…'),
    waiting: t('Installed. Core will restart when sessions are idle.', '已安装，Core 将在会话空闲后重启。'),
    restarting: t('Waiting for the updated Core service to come online…', '正在等待新版 Core 服务恢复连接…'),
    updated: coreChanged ? t('The latest version is installed and takes effect when Core restarts.', '已安装最新版本，Core 重启后生效。')
      : t('Local Core is up to date.', '本机 Core 已是最新版本。'),
    error: t('Could not update local Core.', '本机 Core 更新失败。'),
  };
  const detectCore = !core || core.phase === 'unavailable' || core.phase === 'unmanaged';
  const components: ComponentUpdate[] = [
    {
      id: 'desktop', name: t('Desktop app and built-in tray', '桌面客户端与内置托盘'),
      current: status?.currentVersion, notes: status?.notes, message: connectionError || status?.error || desktopMessage,
      busy: desktopInstalling || status?.phase === 'checking', check: () => { void checkDesktopUpdate(); },
      install: status?.version ? { label: t('Download and install', '下载并安装'), run: () => setConfirm(true) } : undefined,
    },
    {
      id: 'core', name: t('Local Core', '本机 Core'), current: core?.currentVersion ?? core?.installedVersion,
      message: coreConnectionError || core?.error || (core ? coreMessages[core.phase] : t('Detecting the local Core service…', '正在检测本机 Core 服务…')),
      notes: coreChanged ? t(`Installed version ${installedCore}`, `已安装版本 ${installedCore}`) : undefined,
      busy: (!core && !coreConnectionError) || coreInstalling || core?.phase === 'checking' || core?.phase === 'waiting' || core?.phase === 'restarting',
      checkLabel: detectCore ? t('Detect Core', '检测 Core') : undefined,
      check: () => { void (detectCore ? refreshCoreUpdate() : checkCoreUpdate()); },
      install: core?.version && (core.phase === 'available' || core.phase === 'error') ? {
        label: t('Update Core', '更新 Core'), run: () => { if (core.version) void installCoreUpdate(core.version); },
      } : undefined,
    },
  ];

  const release = status?.version ?? status?.currentVersion;
  return <div>
    <div className="mb-5 flex items-center gap-4">
      <HostGlyph icon="logo" color="#293c2e" className="size-14 rounded-xl" />
      <div><h2 className="text-ui-xl font-semibold">CC Remote</h2>
        <p className="mt-1 text-ui-sm text-foreground-subtle">{t('Claude Code and Codex sessions across your computers.', '连接各台电脑上的 Claude Code 与 Codex 会话。')}</p>
      </div>
    </div>
    <GroupTitle>{t('Component updates', '组件更新')}</GroupTitle><GroupCard>
      {components.map(component => <ComponentUpdateRow key={component.id} component={component} installing={desktopInstalling || coreInstalling} />)}
    </GroupCard>
    <div className="mt-5 flex flex-wrap gap-x-5 gap-y-2 text-ui-sm">
      <a className="text-primary underline underline-offset-4" href={release ? `https://github.com/huanfeng/cc-remote/releases/tag/v${encodeURIComponent(release)}` : 'https://github.com/huanfeng/cc-remote/releases'} target="_blank" rel="noreferrer">{status?.version ? t('New version release notes', '新版本发布说明') : t('Release notes', '版本发布说明')}</a>
      <a className="text-primary underline underline-offset-4" href="https://github.com/huanfeng/cc-remote" target="_blank" rel="noreferrer">{t('GitHub project', 'GitHub 项目')}</a>
    </div>
    <ConfirmDialog open={confirm} onOpenChange={setConfirm} title={t('Install desktop update?', '安装桌面端更新？')}
      description={t('The desktop app will close and restart during installation. The host service keeps running.', '安装时桌面应用将退出并重启，宿主后台继续运行。')}
      confirmLabel={t('Install and restart', '安装并重启')} confirmDisabled={!status?.version || desktopInstalling || coreInstalling}
      onConfirm={() => { setConfirm(false); void installDesktopUpdate(); }} />
  </div>;
}

export function DesktopAboutDialog() {
  const open = useDesktopUpdate(s => s.aboutOpen);
  const mobile = useIsMobile();
  useEffect(() => subscribeDesktopAbout(), []);
  if (!DESKTOP) return null;
  return <SurfaceShell open={open} onOpenChange={aboutOpen => useDesktopUpdate.setState({ aboutOpen })} mobile={mobile} title={t('About & updates', '关于与更新')}>
    {open && <DesktopAboutContent />}
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

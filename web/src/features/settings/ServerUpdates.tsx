import { useEffect, useState } from 'react';
import { BackendError, useBackend, type Backend } from '@/backend/backend';
import type { HostInfo, ServerComponentUpdate, ServerUpdateHistoryEntry, ServerUpdateSnapshot, UpdateComponentId } from '@/backend/types';
import { ConfirmDialog, ErrorText, errorMessage } from '@/features/dialogs/common';
import { GroupCard, Row } from '@/features/dialogs/SurfaceShell';
import { apiBase, currentHost } from '@/lib/hosts';
import { locale, t } from '@/lib/i18n';
import { ComponentUpdateRow, displayVersion, ReleaseNotesLink } from './ComponentUpdateRow';
import { GroupTitle } from './parts';

const names: Record<UpdateComponentId, string> = { core: 'CC Remote Core', claude: 'Claude Code', codex: 'Codex' };

function messageFor(component: ServerComponentUpdate): string {
  if (component.message) return component.message;
  const version = component.version ? displayVersion(component.version) : '';
  switch (component.phase) {
    case 'idle': return t('Check for updates on this server.', '检查此服务器上是否有更新。');
    case 'checking': return t('Checking for updates…', '正在检查更新…');
    case 'available': return t(`${version} is available.`, `${version} 可更新。`);
    case 'ready': return t('The component will check and update using its configured channel.', '组件将按已配置的渠道检查并更新。');
    case 'current': return t('Already up to date.', '已是最新版本。');
    case 'installing': return t('Updating on the server…', '正在服务器上更新…');
    case 'waiting': return t('Waiting for the server to become idle…', '正在等待服务器空闲…');
    case 'updated': return t('Update completed.', '更新完成。');
    case 'error': return t('Update failed. Check for updates to try again.', '更新失败。请重新检查更新后重试。');
    case 'unavailable': return t('This installation cannot be updated here.', '当前安装方式暂不支持在此更新。');
  }
}

const historyDate = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' });

function UpdateHistory({ entries }: { entries: ServerUpdateHistoryEntry[] }) {
  if (!entries.length) return null;
  return <details className="mt-3 rounded-xl border border-border/70 bg-card px-4 py-3">
    <summary className="cursor-pointer text-ui-sm font-medium">{t('Update history', '更新记录')}</summary>
    <ul className="mt-2 max-h-64 divide-y divide-border/60 overflow-auto">
      {entries.map(entry => {
        const date = new Date(entry.at);
        const versions = entry.from && entry.version && entry.from !== entry.version
          ? `${displayVersion(entry.from)} → ${displayVersion(entry.version)}`
          : displayVersion(entry.version || entry.from || '');
        const status = entry.status === 'updated' ? t('Updated', '已更新')
          : entry.status === 'current' ? t('Already up to date', '已是最新版本') : t('Failed', '失败');
        return <li key={entry.id} className="space-y-1 py-2 text-ui-sm">
          <div className="flex flex-wrap items-baseline gap-x-2">
            <span className="font-medium">{names[entry.component] ?? entry.component}</span>
            {versions && <span className="tabular-nums text-foreground-subtle">{versions}</span>}
            <span>{status}</span>
            <time className="ml-auto whitespace-nowrap text-foreground-subtle" dateTime={entry.at}>{Number.isNaN(date.getTime()) ? entry.at : historyDate.format(date)}</time>
          </div>
          {entry.message && <p className="break-words text-foreground-subtle">{entry.message}</p>}
          <ReleaseNotesLink url={entry.notesUrl} />
        </li>;
      })}
    </ul>
  </details>;
}

type UpdateAPI = NonNullable<Backend['componentUpdates']>;
type InstallTarget = { id: UpdateComponentId; version?: string };
const canInstall = (component: ServerComponentUpdate) => component.phase === 'ready' || component.phase === 'available' && !!component.version;

function ServerUpdatesContent({ api, info, host }: { api?: UpdateAPI; info?: HostInfo | null; host: string }) {
  const [snapshot, setSnapshot] = useState<ServerUpdateSnapshot>();
  const [connectionError, setConnectionError] = useState('');
  const [actionError, setActionError] = useState('');
  const [pending, setPending] = useState(false);
  const [confirmation, setConfirmation] = useState<InstallTarget>();

  useEffect(() => api?.watch(value => {
    if (value) setSnapshot(value);
    setConnectionError(value ? '' : t('This server does not support component updates.', '此服务器版本不支持组件更新。'));
  }, e => setConnectionError(errorMessage(e))), [api]);

  const run = async (id: UpdateComponentId, action: 'check' | 'install', version?: string) => {
    if (!api || pending) return;
    setPending(true);
    setActionError('');
    try {
      setSnapshot(await (action === 'check' ? api.check(id) : api.install(id, version)));
      setConnectionError('');
    } catch (e) {
      if (e instanceof BackendError && [0, 502, 504].includes(e.status)) setConnectionError(errorMessage(e));
      else setActionError(errorMessage(e));
    } finally {
      setPending(false);
    }
  };

  const busy = pending || !!snapshot?.components.some(row => row.phase === 'checking' || row.phase === 'installing' || row.phase === 'waiting');
  const candidate = confirmation && snapshot?.components.find(row => row.id === confirmation.id && row.version === confirmation.version);
  const confirmName = confirmation ? names[confirmation.id] ?? confirmation.id : '';
  const current: Record<UpdateComponentId, string | undefined> = { core: info?.version, claude: info?.claudeVersion, codex: info?.codexVersion };
  const fallback = [
    { id: 'core' as const, value: current.core ? displayVersion(current.core) : t('Not reported', '未报告') },
    { id: 'claude' as const, value: info?.claudeAvailable === false ? t('Unavailable', '不可用') : current.claude || t('Installed', '已安装') },
    ...(info?.codexAvailable !== undefined ? [{ id: 'codex' as const, value: info.codexAvailable ? current.codex || t('Installed', '已安装') : t('Unavailable', '不可用') }] : []),
  ];

  return <>
    <GroupTitle>{t('Component updates', '组件更新')}</GroupTitle>
    <p className="mb-2 break-words px-1 text-ui-sm text-foreground-subtle">{host}</p>
    <GroupCard>
      {snapshot ? snapshot.components.map(component => <ComponentUpdateRow key={component.id} component={{
        id: component.id, name: names[component.id] ?? component.id,
        current: component.currentVersion || (component.phase === 'unavailable' ? undefined : current[component.id]),
        message: messageFor(component), notes: component.notes, notesUrl: component.notesUrl,
        busy: busy || !!connectionError,
        check: () => { void run(component.id, 'check'); },
        install: canInstall(component)
          ? { label: t('Update', '一键更新'), run: () => setConfirmation({ id: component.id, version: component.version }) } : undefined,
      }} />) : fallback.map(row => <Row key={row.id} label={names[row.id]} control={<span className="text-ui-sm tabular-nums text-foreground-subtle">{row.value}</span>} />)}
    </GroupCard>
    {!api && <p className="mt-2 px-1 text-ui-sm text-foreground-subtle">{t('Component updates are unavailable.', '当前无法使用组件更新。')}</p>}
    {api && !snapshot && !connectionError && <p className="mt-2 px-1 text-ui-sm text-foreground-subtle" role="status">{t('Loading update status…', '正在读取更新状态…')}</p>}
    {connectionError && <p className="mt-2 px-1 text-ui-sm text-foreground-subtle" role="status">{connectionError}</p>}
    {actionError && <div className="mt-2"><ErrorText>{actionError}</ErrorText></div>}
    {snapshot && <UpdateHistory entries={snapshot.history} />}
    <ConfirmDialog open={!!confirmation} onOpenChange={open => { if (!open) setConfirmation(undefined); }}
      title={t(`Update ${confirmName}?`, `更新 ${confirmName}？`)}
      description={confirmation?.id === 'core'
        ? t(`Target server: ${host}. Core will restart when sessions are idle. The desktop app stays open.`, `目标服务器：${host}。会话空闲后 Core 将重启，桌面客户端会保持打开。`)
        : t(`Target server: ${host}.${confirmation?.version ? ` Install ${displayVersion(confirmation.version)}.` : ''}`, `目标服务器：${host}。${confirmation?.version ? `将安装 ${displayVersion(confirmation.version)}。` : ''}`)}
      confirmLabel={t('Update on this server', '更新此服务器')}
      confirmDisabled={busy || !!connectionError || !candidate || !canInstall(candidate)}
      onConfirm={() => {
        if (!confirmation) return;
        setConfirmation(undefined);
        void run(confirmation.id, 'install', confirmation.version);
      }} />
  </>;
}

export function ServerUpdates({ info }: { info?: HostInfo | null }) {
  const backend = useBackend();
  const address = backend.kind === 'http' ? apiBase || globalThis.location?.host || '' : '';
  const name = currentHost?.alias || info?.name || currentHost?.name;
  const host = (name && address && name !== address ? `${name} (${address})` : name || address) || t('Connected server', '当前服务器');
  return <ServerUpdatesContent key={address} api={backend.componentUpdates} info={info} host={host} />;
}

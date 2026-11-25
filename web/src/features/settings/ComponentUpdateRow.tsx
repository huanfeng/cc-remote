import { Button } from '@/components/ui/button';
import { Row } from '@/features/dialogs/SurfaceShell';
import { t } from '@/lib/i18n';

export interface ComponentUpdate {
  id: string;
  name: string;
  current?: string | null;
  message: string;
  notes?: string | null;
  notesUrl?: string | null;
  busy: boolean;
  checkLabel?: string;
  check(): void;
  install?: { label: string; run(): void };
}

export function displayVersion(version: string): string {
  return /^\d/.test(version) ? `v${version}` : version;
}

export function ReleaseNotesLink({ url }: { url?: string | null }) {
  if (!url) return null;
  try {
    const link = new URL(url);
    if (link.protocol === 'https:' && !link.username && !link.password) {
      return <a className="text-ui-sm text-primary underline underline-offset-4" href={link.href} target="_blank" rel="noreferrer">{t('Release notes', '版本发布说明')}</a>;
    }
  } catch { /* Invalid upstream URL. */ }
  return null;
}

function ReleaseNotes({ notes, url }: { notes?: string | null; url?: string | null }) {
  if (!notes?.trim()) return <ReleaseNotesLink url={url} />;
  return <details className="rounded-lg border border-border/60 px-3 py-2 text-ui-sm">
    <summary className="cursor-pointer font-medium text-foreground">{t('Changelog', '更新日志')}</summary>
    <div className="mt-2 space-y-2">
      <p className="max-h-48 select-text overflow-auto whitespace-pre-wrap break-words">{notes}</p>
      <ReleaseNotesLink url={url} />
    </div>
  </details>;
}

export function ComponentUpdateRow({ component, installing }: { component: ComponentUpdate; installing?: boolean }) {
  return <Row label={component.name} stack description={component.current && <span className="select-text tabular-nums">{displayVersion(component.current)}</span>}>
    <div className="space-y-2.5">
      <p className="break-words text-ui-sm text-muted-foreground" role="status">{component.message}</p>
      <ReleaseNotes notes={component.notes} url={component.notesUrl} />
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={component.busy} onClick={component.check}>{component.checkLabel ?? t('Check for updates', '检查更新')}</Button>
        {component.install && <Button size="sm" disabled={component.busy || installing} onClick={component.install.run}>{component.install.label}</Button>}
      </div>
    </div>
  </Row>;
}

import { useEffect, useState } from 'react';
import { useBackend } from '@/backend/backend';
import type { UploadLimit } from '@/backend/types';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ErrorText, errorMessage } from '@/features/dialogs/common';
import { GroupCard, Row } from '@/features/dialogs/SurfaceShell';
import { t } from '@/lib/i18n';
import { GroupTitle } from './parts';

export function UploadLimitSetting() {
  const backend = useBackend();
  const [policy, setPolicy] = useState<UploadLimit>();
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    let live = true;
    setPolicy(undefined);
    setError('');
    setSaved(false);
    backend.uploadLimit().then(value => {
      if (!live || !value) return;
      setPolicy(value);
      setDraft(String(value.limitMiB));
    }, e => { if (live) setError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend]);

  if (!policy && !error) return null;
  const label = t('Maximum size per file (MiB)', '单文件上传上限（MiB）');
  const limit = Number(draft);
  const valid = !!policy && Number.isInteger(limit) && limit >= 1 && limit <= policy.maxMiB;

  return <>
    <GroupTitle>{t('Attachments', '附件')}</GroupTitle>
    <GroupCard>
      {policy && <form onSubmit={async e => {
        e.preventDefault();
        if (!valid || busy || limit === policy.limitMiB) return;
        setBusy(true); setError(''); setSaved(false);
        try {
          const updated = await backend.setUploadLimit(limit);
          setPolicy(updated);
          setDraft(String(updated.limitMiB));
          setSaved(true);
        } catch (e) {
          setError(errorMessage(e));
        } finally {
          setBusy(false);
        }
      }}>
        <Row label={label} description={t(`Applies to new uploads on this server immediately. Enter 1–${policy.maxMiB} MiB.`, `保存后立即应用于此服务器的新上传。可设置 1–${policy.maxMiB} MiB。`)} control={
          <div className="flex items-center gap-2">
            <Input className="w-24" aria-label={label} type="number" min={1} max={policy.maxMiB} step={1} required value={draft}
              disabled={busy} onChange={e => { setDraft(e.target.value); setSaved(false); }} />
            <Button type="submit" disabled={!valid || busy || limit === policy.limitMiB}>{busy ? t('Saving…', '保存中…') : t('Save', '保存')}</Button>
          </div>
        } />
      </form>}
      {saved && <p role="status" className="px-4 pb-3 text-ui-sm text-success">{t('Upload limit saved.', '上传上限已保存。')}</p>}
      {error && <div className="px-4 py-3"><ErrorText>{error}</ErrorText></div>}
    </GroupCard>
  </>;
}

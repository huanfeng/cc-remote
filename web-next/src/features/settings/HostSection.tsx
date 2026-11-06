// This host: versions, a link to the host status, and its identity (name,
// colour, icon) shown in the tab title, favicon and sidebar of every page.
import { useEffect, useState } from 'react';
import { Activity, Check, ChevronRight, Plus } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Spinner } from '@/components/ui/spinner';
import { cn } from '@/components/lib/utils';
import { useBackend } from '@/backend/backend';
import type { HostIdentity } from '@/backend/types';
import { useApp } from '@/store/app';
import { t } from '@/lib/i18n';
import { GroupCard, Row } from '@/features/dialogs/SurfaceShell';
import { ErrorText, errorMessage } from '@/features/dialogs/common';
import { GroupTitle, HOST_ICONS, HostGlyph, SectionHeader, useReportDirty } from './parts';

type Look = { name: string; color: string; icon: string };
const MAX_ICON = 3;

export function HostSection({ mobile }: { mobile: boolean }) {
  const backend = useBackend();
  const info = useApp(s => s.info);
  const [identity, setIdentity] = useState<HostIdentity>();
  const [draft, setDraft] = useState<Look>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  useEffect(() => {
    let live = true;
    backend.identity().then(v => { if (live) { setIdentity(v); setDraft({ name: v.name, color: v.color, icon: v.icon }); } }, e => { if (live) setError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend]);
  const dirty = !!identity && !!draft && (draft.name !== identity.name || draft.color !== identity.color || draft.icon !== identity.icon);
  useReportDirty('host', dirty);

  const save = async () => {
    if (!draft) return;
    setBusy(true); setError(''); setNotice('');
    try {
      const v = await backend.saveIdentity({ name: draft.name.trim(), color: draft.color, icon: draft.icon });
      setIdentity(v); setDraft({ name: v.name, color: v.color, icon: v.icon });
      const app = useApp.getState();
      if (app.info) app.setInfo({ ...app.info, name: v.shownName, color: v.shownColor, icon: v.icon });
      setNotice(t('Saved: the tab title, icon and logo of every page on this host change.', '已保存：此宿主所有页面的标签页标题、图标和标志都会更新。'));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const shownColor = draft?.color || identity?.defaultColor || '';
  const versions: { name: string; value: string }[] = [
    { name: 'CC Remote', value: info?.version ? `v${info.version}` : t('Not reported', '未报告') },
    { name: 'Claude Code', value: info?.claudeAvailable === false ? t('Unavailable', '不可用') : info?.claudeVersion || t('Installed', '已安装') },
    ...(info?.codexAvailable !== undefined ? [{ name: 'Codex', value: info.codexAvailable ? info.codexVersion || t('Installed', '已安装') : t('Unavailable', '不可用') }] : []),
  ];

  return (
    <div>
      {!mobile && <SectionHeader title={t('This host', '本宿主')} description={t('How this host appears on every page, and what it runs.', '此宿主在各页面中的外观，以及它运行的组件。')} />}

      <GroupTitle>{t('Versions', '版本')}</GroupTitle>
      <GroupCard>
        {versions.map(v => <Row key={v.name} label={v.name} control={<span className="text-ui-sm tabular-nums text-foreground-subtle">{v.value}</span>} className="py-2.5" />)}
        {info?.restartPending && <Row label={t('Restart pending', '等待重启')} description={t('An update is installed and applies when the host restarts.', '更新已安装，宿主重启后生效。')} />}
        <button type="button" onClick={() => useApp.getState().open({ type: 'status' })}
          className="flex w-full items-center gap-2 border-t border-border/60 px-4 py-3 text-left text-ui-base hover:bg-surface-hover">
          <Activity className="size-4 text-foreground-subtle" />
          <span className="flex-1">{t('Host status', '宿主状态')}<span className="ml-2 text-ui-sm text-foreground-subtlest">{t('Service, diagnostics and usage', '服务、诊断与用量')}</span></span>
          <ChevronRight className="size-4 text-foreground-subtlest" />
        </button>
      </GroupCard>

      <GroupTitle>{t('Identity', '身份')}</GroupTitle>
      {!identity || !draft ? (
        error ? <ErrorText>{error}</ErrorText> : <div className="flex h-24 items-center justify-center"><Spinner className="size-4 text-foreground-subtle" /></div>
      ) : (
        <>
          <GroupCard>
            <div className="flex items-center gap-3 px-4 py-3">
              <HostGlyph icon={draft.icon} color={shownColor} className="size-10 rounded-xl" />
              <div className="min-w-0">
                <div className="truncate text-ui-lg font-semibold">{draft.name.trim() || identity.defaultName}</div>
                <div className="text-ui-xs text-foreground-subtlest">{t('Preview', '预览')}</div>
              </div>
            </div>
            <Row
              label={t('Name', '名称')}
              description={t(`Empty uses the computer name (${identity.defaultName}).`, `留空使用计算机名（${identity.defaultName}）。`)}
              control={<Input size="lg" value={draft.name} maxLength={40} placeholder={identity.defaultName} onChange={e => setDraft({ ...draft, name: e.target.value })}
                className={cn('w-56 max-sm:w-full', mobile && 'h-10 text-mobile-input-safe')} />}
            />
            <Row label={t('Colour', '颜色')} stack>
              <ColorChoices value={draft.color} palette={identity.palette} auto={identity.defaultColor} onChange={color => setDraft({ ...draft, color })} />
            </Row>
            <Row label={t('Icon', '图标')} description={t('A preset, or a few characters or an emoji of your own.', '预设图标，或者几个字符、一个 emoji。')} stack>
              <IconChoices value={draft.icon} color={shownColor} onChange={icon => setDraft({ ...draft, icon })} mobile={mobile} />
            </Row>
          </GroupCard>
          <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
            {notice && !dirty && <p role="status" className="mr-auto text-ui-sm text-success">{notice}</p>}
            <Button variant="ghost" size="lg" disabled={!dirty || busy} onClick={() => setDraft({ name: identity.name, color: identity.color, icon: identity.icon })}>{t('Revert', '还原')}</Button>
            <Button size="lg" disabled={!dirty || busy} onClick={() => void save()}>
              {busy && <Spinner className="size-3.5" />}
              {busy ? t('Saving…', '保存中…') : t('Save', '保存')}
            </Button>
          </div>
          {error && <div className="mt-2"><ErrorText>{error}</ErrorText></div>}
        </>
      )}
    </div>
  );
}

/** Colour swatches are the host's own data (hex), drawn as inline styles. */
function ColorChoices({ value, palette, auto, onChange }: { value: string; palette: { key: string; hex: string }[]; auto: string; onChange: (v: string) => void }) {
  const chosen = value.toLowerCase();
  const custom = !!chosen && !palette.some(p => p.hex.toLowerCase() === chosen);
  const swatch = 'relative flex size-8 items-center justify-center rounded-full ring-offset-2 ring-offset-card transition-shadow max-md:size-9';
  return (
    <div role="radiogroup" aria-label={t('Colour', '颜色')} className="flex flex-wrap items-center gap-2">
      <button type="button" role="radio" aria-checked={!chosen} title={t('Automatic', '自动')} onClick={() => onChange('')}
        className={cn(swatch, 'h-8 w-auto rounded-full border border-border px-3 text-ui-xs text-foreground-subtle max-md:h-9', !chosen && 'ring-2 ring-foreground/60')}>
        <span className="mr-1.5 size-3 rounded-full" style={{ background: auto }} />{t('Auto', '自动')}
      </button>
      {palette.map(p => {
        const on = chosen === p.hex.toLowerCase();
        return (
          <button key={p.key} type="button" role="radio" aria-checked={on} aria-label={p.hex} title={p.hex} onClick={() => onChange(p.hex.toLowerCase())}
            className={cn(swatch, on && 'ring-2 ring-foreground/60')} style={{ background: p.hex }}>
            {on && <Check className="size-3.5 text-white" />}
          </button>
        );
      })}
      <label title={t('Any colour', '自定义颜色')} className={cn(swatch, 'cursor-pointer border border-dashed border-border-hover', custom && 'border-transparent ring-2 ring-foreground/60')} style={custom ? { background: chosen } : undefined}>
        <input type="color" className="sr-only" value={chosen || auto} onChange={e => onChange(e.target.value.toLowerCase())} aria-label={t('Any colour', '自定义颜色')} />
        {custom ? <Check className="size-3.5 text-white" /> : <Plus className="size-3.5 text-foreground-subtle" />}
      </label>
    </div>
  );
}

function IconChoices({ value, color, onChange, mobile }: { value: string; color: string; onChange: (v: string) => void; mobile: boolean }) {
  const text = value && !HOST_ICONS[value] ? value : '';
  return (
    <div className="flex flex-col gap-3">
      <div role="radiogroup" aria-label={t('Icon', '图标')} className="flex flex-wrap gap-1.5">
        {Object.entries(HOST_ICONS).map(([key, { icon: Icon, label }]) => {
          const on = (value || 'laptop') === key;
          return (
            <button key={key} type="button" role="radio" aria-checked={on} aria-label={label()} title={label()} onClick={() => onChange(key)}
              className={cn('flex size-9 items-center justify-center rounded-lg border transition-colors max-md:size-10', on ? 'border-transparent text-white' : 'border-border/70 text-foreground-subtle hover:bg-surface-hover hover:text-foreground')}
              style={on ? { background: color } : undefined}>
              <Icon className="size-4" />
            </button>
          );
        })}
      </div>
      <div className="flex items-center gap-2">
        <Input size="lg" value={text} placeholder={t('Or text: P1, 家, 🚀', '或文字：P1、家、🚀')} aria-label={t('Icon text', '图标文字')}
          onChange={e => onChange(Array.from(e.target.value.trim()).slice(0, MAX_ICON).join(''))}
          className={cn('w-48 max-sm:flex-1', mobile && 'h-10 text-mobile-input-safe')} />
        {text && <HostGlyph icon={text} color={color} />}
      </div>
    </div>
  );
}

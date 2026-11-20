import { useEffect, useRef, useState } from 'react';
import { Reorder } from 'motion/react';
import { useBackend, BackendError, type StatuslineSettings, type StatuslineOptions, type StatuslineStyle } from '@/backend/backend';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Spinner } from '@/components/ui/spinner';
import { GroupCard, Row, SurfaceShell } from '@/features/dialogs/SurfaceShell';
import { ConfirmDialog, ErrorText, errorMessage, SimpleSelect } from '@/features/dialogs/common';
import { t } from '@/lib/i18n';
import { GroupTitle, useReportDirty } from './parts';
import { SettingsReorderRow } from './SettingsReorderRow';

import { statuslineSegments as segments, colorThemes, colorPresets, defaultStatuslineOptions as defaults, statuslineExample, statuslineColor, statuslineOrder, orderStatusline, toggleStatusline, moveStatusline } from './statuslineAppearance';
function segmentLabel(key: string): string {
  const labels: Record<string, string> = {
    provider: t('Provider', '供应商'), model: t('Model', '模型'),
    effort: t('Effort', '思考强度'), context: t('Context', '上下文用量'),
    input: t('Input tokens', '输入 Token'), output: t('Output tokens', '输出 Token'),
    cost: t('Session cost', '会话费用'), fiveHour: t('5-hour limit', '5 小时配额'),
    sevenDay: t('7-day limit', '7 天配额'), directory: t('Directory', '目录'),
    duration: t('Duration', '会话时长'),
  };
  return labels[key] ?? key;
}
const examplePercentages: Record<string, number> = { context: 32, fiveHour: 18, sevenDay: 24 };

function previewSegment(key: string, options: StatuslineOptions) {
  const percentage = examplePercentages[key];
  let color = statuslineColor(key, options.colors);
  let text = statuslineExample(key, options.shortLabels);
  if (percentage !== undefined) {
    if (percentage >= options.critical) color = options.colors.critical ?? '#ef4444';
    else if (percentage >= options.warning) color = options.colors.warning ?? '#f59e0b';
    if (options.bars) {
      const filled = Math.floor(percentage / 10);
      text += ` [${'█'.repeat(filled)}${'░'.repeat(10 - filled)}]`;
    }
  }
  return { text, color };
}

function StatuslinePreview({ style, options }: { style: StatuslineStyle; options: StatuslineOptions }) {
  const lineBreak = options.segments.filter(key => ['provider', 'model', 'effort'].includes(key)).length;
  return <>{options.segments.map((key, index) => {
    const segment = previewSegment(key, options);
    const separator = index === 0 ? '' : style === 'multiline' && index === lineBreak ? '\n' : options.separator;
    return (
      <span key={key} style={style === 'color' ? { color: segment.color } : undefined}>
        {separator}{segment.text}
      </span>
    );
  })}</>;
}

const stableOptions = (o: StatuslineOptions) => JSON.stringify({ ...o, colors: Object.fromEntries(Object.entries(o.colors ?? {}).sort()) });

function StatuslineColorPicker({ label, value, disabled, onChange }: { label: string; value: string; disabled: boolean; onChange: (value: string) => void }) {
  return <Popover>
    <PopoverTrigger asChild><Button variant="outline" disabled={disabled} aria-label={label} className="size-8 p-1.5"><span className="size-full rounded border border-foreground/20" style={{ backgroundColor: value }} /></Button></PopoverTrigger>
    <PopoverContent className="w-60" align="end">
      <p className="text-ui-sm font-medium">{label}</p>
      <div className="grid grid-cols-6 gap-1.5">{colorPresets.map(color => <button key={color.value} type="button" disabled={disabled} aria-label={t(color.en, color.zh)} title={t(color.en, color.zh)} aria-pressed={value.toLowerCase() === color.value} className="size-8 rounded-md border-2 border-transparent outline-offset-2 aria-pressed:border-foreground focus-visible:outline-2 focus-visible:outline-ring" style={{ backgroundColor: color.value }} onClick={() => onChange(color.value)} />)}</div>
      <label className="flex items-center justify-between gap-2 text-ui-sm">{t('Custom color', '自定义颜色')}<input type="color" aria-label={t(`Custom ${label}`, `自定义${label}`)} disabled={disabled} value={value} onChange={e => onChange(e.target.value)} /></label>
    </PopoverContent>
  </Popover>;
}

export function StatuslineSection({ mobile = false }: { mobile?: boolean }) {
  const [open, setOpen] = useState(false);
  const [discard, setDiscard] = useState(false);
  const [previewOrder, setPreviewOrder] = useState<string[] | null>(null);
  const pendingOrder = useRef<string[] | null>(null);
  const backend = useBackend();
  const [saved, setSaved] = useState<StatuslineSettings | null>();
  const [style, setStyle] = useState<StatuslineStyle>('compact');
  const [options, setOptions] = useState<StatuslineOptions>(defaults);
  const rowOrder = previewOrder ?? statuslineOrder(options);
  const finishReorder = (cancelled: boolean) => {
    const next = pendingOrder.current;
    pendingOrder.current = null;
    setPreviewOrder(null);
    if (!cancelled && next) setOptions(current => orderStatusline(current, next));
  };
  const dirty = !!saved && (style !== (saved.style || 'compact') || stableOptions(options) !== stableOptions(saved.options ?? defaults()));
  useReportDirty('statusline', dirty);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [version, setVersion] = useState(0);
  useEffect(() => {
    let live = true;
    pendingOrder.current = null;
    setPreviewOrder(null);
    setError(''); setSaved(undefined);
    (backend.statusline?.() ?? Promise.resolve(null)).then(v => {
      if (!live) return;
      setSaved(v); if (v?.style) setStyle(v.style); setOptions(v?.options ?? defaults());
    }, e => { if (live) setError(errorMessage(e)); });
    return () => { live = false; };
  }, [backend, version]);
  const apply = async (remove: boolean) => {
    if (!saved) return;
    setBusy(true); setError(''); setNotice('');
    try {
      const next = remove
        ? await backend.removeStatusline!({ revision: saved.revision })
        : await backend.saveStatusline!({ style, revision: saved.revision, ...(saved.capabilities?.customize ? { options } : {}) });
      setSaved(next); setStyle(next.style || 'compact'); setOptions(next.options ?? defaults());
      setNotice(remove ? t('Uninstalled. The previous status line was restored.', '已卸载，并恢复安装前的状态栏配置。') : t('Installed in Claude Code user settings.', '已配置到 Claude Code 用户设置。'));
    } catch (e) {
      setError(e instanceof BackendError && e.status === 409
        ? t('Settings changed elsewhere. Reload before trying again.', '配置已在别处修改，请刷新后重试。') : errorMessage(e));
    } finally { setBusy(false); }
  };
  const close = () => {
    if (busy) return;
    finishReorder(true);
    if (dirty) setDiscard(true); else setOpen(false);
  };
  return <>
    <GroupCard className="mt-4"><Row label={t('Claude Code status line', 'Claude Code 状态栏')} description={saved?.installed ? t('Configured', '已配置') : t('Customize elements, colors and order.', '配置显示项目、配色和顺序。')} control={<Button variant="outline" onClick={() => setOpen(true)}>{t('Configure', '配置')}</Button>} /></GroupCard>
    <SurfaceShell open={open} mobile={mobile} onOpenChange={value => { if (!value) close(); }} className="h-[min(86vh,820px)] sm:max-w-3xl" title={t('Claude Code status line', 'Claude Code 状态栏')}>
    <GroupTitle>{t('Claude Code status line', 'Claude Code 状态栏')}</GroupTitle>
    <GroupCard>
      <div className="px-4 py-3 text-ui-sm text-foreground-subtle">
        {t('Show the provider, model, effort, context usage, session cost and 5-hour / 7-day limits in Claude Code. Missing information is hidden.', '在 Claude Code 中显示 Provider、模型、思考强度、上下文用量、会话费用及 5 小时 / 7 天配额。不存在的信息自动隐藏。')}
      </div>
      {saved === null ? <p className="px-4 pb-3 text-ui-sm text-foreground-subtle">{t('Upgrade this host to configure the status line.', '请升级此宿主以配置状态栏。')}</p>
        : saved === undefined ? !error && <div className="p-4"><Spinner className="size-4" /></div>
        : <>
          <Row label={t('Style', '风格')} control={<SimpleSelect value={style} onChange={v => setStyle(v as StatuslineStyle)} disabled={busy} ariaLabel={t('Status line style', '状态栏风格')} options={[
            { value: 'compact', label: t('Compact', '简洁') },
            { value: 'color', label: t('Color', '彩色') },
            { value: 'multiline', label: t('Multiline', '多行') },
          ]} />} />
          {saved.capabilities?.customize && <div className="space-y-3 px-4 pb-4">
            <div className="flex flex-wrap gap-2">
              <Button variant="ghost" disabled={busy} onClick={() => { setOptions(defaults()); setStyle('compact'); }}>{t('Reset to defaults', '恢复默认')}</Button>
              {['minimal','focused','full'].map(preset => <Button key={preset} variant="outline" disabled={busy} onClick={() => setOptions(o => ({ ...o, order: statuslineOrder(o), segments: statuslineOrder(o).filter(key => (preset === 'minimal' ? ['provider','model','context'] : preset === 'focused' ? ['provider','model','effort','context','cost','fiveHour','sevenDay'] : segments).includes(key)) }))}>{preset === 'minimal' ? t('Minimal','精简') : preset === 'focused' ? t('Focused','专注') : t('Full','完整')}</Button>)}</div>
            <p className="text-ui-xs text-foreground-subtle">{t('Choose elements, order and individual colors. Colors apply to the Color style.', '选择显示项目、顺序及各项颜色。颜色仅在彩色风格生效。')}</p>
            <div className="space-y-2">
              <p className="text-ui-sm font-medium">{t('Color themes', '配色预设')}</p>
              <div className="flex flex-wrap gap-2">{colorThemes.map(theme => <Button key={theme.id} variant="outline" disabled={busy} onClick={() => { setOptions(o => ({ ...o, colors: { ...theme.colors } })); setStyle('color'); }}><span aria-hidden="true" className="flex gap-0.5">{['provider','model','effort','context','cost'].map(key => <span key={key} className="size-2 rounded-full" style={{ backgroundColor: theme.colors[key] }} />)}</span>{t(theme.en, theme.zh)}</Button>)}</div>
              <p className="text-ui-xs text-foreground-subtle">{t('Themes replace colors and select Color style; elements and layout stay as configured. Use a color button below for quick individual choices.', '应用预设会替换配色并切换为彩色风格，保留显示项目和布局。点击下方色块可快速选择单项颜色。')}</p>
            </div>
            <Reorder.Group axis="y" values={rowOrder} className="list-none"
              onReorder={next => { pendingOrder.current = next; setPreviewOrder(next); }}>
              {rowOrder.map(key => (
                <SettingsReorderRow key={key} id={key} name={segmentLabel(key)} disabled={busy}
                  className="rounded-md text-ui-sm"
                  onMove={direction => setOptions(current => {
                    const order = statuslineOrder(current);
                    return moveStatusline(current, key, order[order.indexOf(key) + direction]);
                  })}
                  onDragEnd={finishReorder}>
                <label className="flex min-w-0 flex-1 items-center gap-2">
                  <input
                    type="checkbox" disabled={busy} checked={options.segments.includes(key)}
                    onChange={event => setOptions(current => toggleStatusline(current, key, event.target.checked))}
                  />
                  {segmentLabel(key)}
                </label>
                <StatuslineColorPicker disabled={busy} label={t(`${segmentLabel(key)} color`, `${segmentLabel(key)}颜色`)} value={statuslineColor(key, options.colors)} onChange={value => setOptions(current => ({ ...current, colors: { ...current.colors, [key]: value } }))} />
                </SettingsReorderRow>
              ))}
            </Reorder.Group>
            <label className="flex gap-2 text-ui-sm"><input type="checkbox" disabled={busy || !saved.capabilities?.shortLabels} checked={!!options.shortLabels} onChange={e => setOptions(o => ({ ...o, shortLabels: e.target.checked }))} />{t('Abbreviated labels', '类型名简写')}</label>
            <p className="text-ui-xs text-foreground-subtle">{saved.capabilities?.shortLabels ? t('P provider · M model · E effort · C context · I input · O output · D directory · T duration. Cost keeps only $. 5h / 7d stay unchanged.', 'P 供应商 · M 模型 · E 思考强度 · C 上下文 · I 输入 · O 输出 · D 目录 · T 时长；费用仅保留 $，5h / 7d 不变。') : t('Upgrade this host to abbreviate labels.', '升级此宿主后可使用类型名简写。')}</p>
            <label className="flex gap-2 text-ui-sm"><input type="checkbox" disabled={busy} checked={options.bars} onChange={e => setOptions(o => ({ ...o, bars:e.target.checked }))} />{t('Usage progress bars','用量进度条')}</label>
            <div className="flex flex-wrap gap-3">{['normal','warning','critical'].map(k => <label key={k} className="flex items-center gap-2 text-ui-sm">{k === 'normal' ? t('Default','默认') : k === 'warning' ? t('Warning','警告') : t('Critical','危险')}<StatuslineColorPicker disabled={busy} label={k === 'normal' ? t('Default color', '默认颜色') : k === 'warning' ? t('Warning color', '警告颜色') : t('Critical color', '危险颜色')} value={options.colors[k] ?? defaults().colors[k]} onChange={value => setOptions(o => ({ ...o, colors: { ...o.colors, [k]: value } }))} /></label>)}</div>
            <div className="flex flex-wrap gap-3">{(['warning','critical'] as const).map(k => <label key={k} className="text-ui-sm">{k === 'warning' ? t('Warning %','警告阈值 %') : t('Critical %','危险阈值 %')} <input className="w-16 rounded border border-border bg-surface px-2 py-1" type="number" min={0} max={100} disabled={busy} value={options[k]} onChange={e => setOptions(o => ({...o,[k]:Number(e.target.value)}))} /></label>)}</div>
            <label className="flex items-center gap-2 text-ui-sm">{t('Separator','分隔符')}<input className="w-28 rounded border border-border bg-surface px-2 py-1" maxLength={20} disabled={busy} value={options.separator} onChange={e => setOptions(o => ({ ...o,separator:e.target.value }))} /></label>
            {options.warning >= options.critical && <ErrorText>{t('Warning must be below critical.', '警告阈值必须低于危险阈值。')}</ErrorText>}
          </div>}
          <div className={`mx-4 mb-3 whitespace-pre-wrap rounded-md p-3 font-mono text-ui-xs ${style === 'color' ? 'bg-slate-950 text-slate-100' : 'bg-surface'}`} aria-label={t('Example preview', '示例预览')}>
            <StatuslinePreview
              style={style}
              options={saved.capabilities?.customize ? (previewOrder ? orderStatusline(options, previewOrder) : options) : defaults()}
            />
          </div>
          <p className="px-4 pb-3 text-ui-xs text-foreground-subtlest">{t('Example data. Limits are shown only when Claude Code supplies them. Any existing status line is saved for uninstall.', '预览为示例数据。配额仅在 Claude Code 提供时显示；安装前的状态栏会保留以便卸载恢复。')}</p>
          <div className="flex flex-wrap justify-end gap-2 px-4 pb-3">
            {saved.installed && <Button variant="outline" disabled={busy || !backend.removeStatusline} onClick={() => void apply(true)}>{t('Uninstall', '卸载')}</Button>}
            <Button disabled={busy || !backend.saveStatusline || (saved.capabilities?.customize && options.warning >= options.critical)} onClick={() => void apply(false)}>{busy && <Spinner className="size-3.5" />}{saved.installed ? t('Apply style', '应用风格') : t('One-click setup', '一键配置')}</Button>
          </div>
        </>}
    </GroupCard>
    {notice && <p role="status" className="mt-2 text-ui-sm text-success">{notice}</p>}
    {error && <div className="mt-2"><ErrorText>{error}</ErrorText><Button variant="ghost" disabled={busy} onClick={() => setVersion(v => v + 1)}>{t('Reload', '刷新')}</Button></div>}
    </SurfaceShell>
    <ConfirmDialog open={discard} onOpenChange={setDiscard} title={t('Discard changes?', '放弃修改？')} description={t('Unsaved status line changes will be lost.', '未保存的状态栏修改将丢失。')} confirmLabel={t('Discard', '放弃')} onConfirm={() => { setStyle(saved?.style || 'compact'); setOptions(saved?.options ?? defaults()); setDiscard(false); setOpen(false); }} />
  </>;
}

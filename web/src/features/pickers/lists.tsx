// The list bodies shared by the quick switch (header chips, /model) and the
// standalone pickers (new session dialog).
import { useState } from 'react';
import { Command as CommandPrimitive } from 'cmdk';
import { RefreshCw, Search } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { cn } from '@/components/lib/utils';
import type { Provider } from '@/backend/types';
import { t } from '@/lib/i18n';
import { providerName } from '@/domain/providers';
import { EFFORT_NOTES, groupModels, modelEfforts, type PickerOption } from './options';
import { PickerGroup, PickerItem, PickerList } from './PickerSurface';
import { useModels } from './useModels';

const SEARCH_AT = 8;

export function ModelList({ provider, sessionId, value, onPick, inheritLabel, mobile }: {
  provider: Provider; sessionId?: string; value: string; onPick(value: string): void; inheritLabel?: string; mobile?: boolean;
}) {
  const { list, loading, error, refresh } = useModels(provider, sessionId);
  const [query, setQuery] = useState('');
  const models = list?.models ?? [];
  const groups = groupModels(models, query);
  const known = models.some(m => m.id === value);
  const searchable = models.length > SEARCH_AT;
  const trimmed = query.trim();

  return (
    <div className="flex flex-col">
      <div className="flex items-center gap-1 px-2 pb-1 pt-1">
        <span className="flex-1 text-ui-sm font-medium text-foreground-subtle">{t('Model', '模型')}</span>
        <Button
          variant="ghost" size={mobile ? 'icon-lg' : 'icon-sm'} className={cn(mobile && 'size-9')} aria-label={t('Refresh model list', '刷新模型列表')}
          disabled={loading} onClick={refresh}
        >
          <RefreshCw className={cn('size-3.5', loading && 'animate-spin')} />
        </Button>
      </div>
      <PickerList
        label={t('Switch model', '切换模型')}
        search={searchable ? (
          <div className="mx-1 mb-1 flex h-8 items-center gap-2 rounded-md border border-border/60 px-2">
            <Search className="size-3.5 shrink-0 text-foreground-subtlest" />
            <CommandPrimitive.Input
              value={query}
              onValueChange={setQuery}
              placeholder={t('Search or type a model ID', '搜索或输入模型 ID')}
              className={cn('min-w-0 flex-1 bg-transparent text-ui-base outline-none placeholder:text-foreground-subtlest', mobile && 'text-mobile-input-safe')}
            />
          </div>
        ) : undefined}
      >
        {inheritLabel !== undefined && !trimmed && (
          <PickerGroup>
            <PickerItem mobile={mobile} option={{ value: '', label: inheritLabel || t(`Use default (${providerName(provider)} settings)`, `跟随默认（${providerName(provider)} 配置）`) }} selected={value === ''} onSelect={onPick} />
          </PickerGroup>
        )}
        {groups.map(g => (
          <PickerGroup key={g.key} heading={g.label}>
            {g.items.map(m => (
              <PickerItem key={`${g.key}:${m.id}`} id={`${g.key}:${m.id}`} mobile={mobile} option={{ value: m.id, label: m.label, description: m.description ?? (m.label !== m.id ? m.id : undefined) }} selected={m.id === value} onSelect={onPick} />
            ))}
          </PickerGroup>
        ))}
        {value && !known && !trimmed && list && (
          <PickerGroup heading={t('Current', '当前')}>
            <PickerItem id="current" mobile={mobile} option={{ value, label: value }} selected onSelect={onPick} />
          </PickerGroup>
        )}
        {trimmed && !models.some(m => m.id === trimmed) && (
          <PickerGroup heading={t('Custom', '自定义')}>
            <PickerItem id="custom" mobile={mobile} option={{ value: trimmed, label: t(`Use "${trimmed}"`, `使用「${trimmed}」`) }} selected={false} onSelect={onPick} />
          </PickerGroup>
        )}
        {!list && loading && <div className="px-2 py-3 text-ui-sm text-foreground-subtle">{t('Loading…', '正在读取…')}</div>}
        {list?.warnings?.map(w => <div key={w} className="px-2 py-1 text-ui-sm text-warning">{w}</div>)}
        {error && <div className="px-2 py-1 text-ui-sm text-destructive">{error}</div>}
      </PickerList>
    </div>
  );
}

export function EffortList({ provider, sessionId, model, value, onPick, inheritLabel, mobile }: {
  provider: Provider; sessionId?: string; model?: string; value: string; onPick(value: string): void; inheritLabel?: string; mobile?: boolean;
}) {
  // Codex models advertise their own efforts; Claude uses the generic list.
  const { list } = useModels(provider, sessionId, provider === 'codex');
  const levels = modelEfforts(list?.models, model ?? '', provider);
  const options: PickerOption[] = levels.map(l => ({ value: l, label: l, description: EFFORT_NOTES[l] }));
  if (value && !levels.includes(value)) options.push({ value, label: value });
  return (
    <div className="flex flex-col">
      <div className="px-2 pb-1 pt-1.5 text-ui-sm font-medium text-foreground-subtle">{t('Reasoning effort', '推理强度')}</div>
      <PickerList label={t('Switch effort', '切换推理强度')}>
        {inheritLabel !== undefined && (
          <PickerGroup>
            <PickerItem mobile={mobile} option={{ value: '', label: inheritLabel || t(`Use default (${providerName(provider)} settings)`, `跟随默认（${providerName(provider)} 配置）`) }} selected={value === ''} onSelect={onPick} />
          </PickerGroup>
        )}
        <PickerGroup>
          {options.map(o => <PickerItem key={o.value} mobile={mobile} option={o} selected={o.value === value} onSelect={onPick} />)}
        </PickerGroup>
      </PickerList>
    </div>
  );
}

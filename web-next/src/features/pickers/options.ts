// Option catalogues for the model / effort / permission pickers. Pure (relative
// imports only) so node tests can load them.
import { t } from '../../lib/i18n.ts';
import { codexPermissions, dangerousPermission, providerEfforts, type Provider } from '../../domain/providers.ts';

export interface PickerOption {
  value: string;
  label: string;
  description?: string;
  tag?: string;
  tone?: 'danger' | 'warn';
}

type Model = { id: string; label: string; source: string; description?: string; efforts?: string[]; isDefault?: boolean };

export const EFFORT_NOTES: Record<string, string> = {
  none: t('No reasoning', '不启用推理'),
  minimal: t('Minimal reasoning', '最少推理'),
  low: t('Faster, cheaper', '更快、更省'),
  medium: t('Balanced', '均衡'),
  high: t('Thinks deeper', '更深入思考'),
  xhigh: t('Extra-high thinking', '高强度思考'),
  max: t('Maximum reasoning budget', '最大推理预算'),
  ultra: t('Longest reasoning', '最长推理'),
};

/** Model groups by the source of their entries, in display order. */
export const MODEL_GROUPS: { key: string; label: string; sources: string[] }[] = [
  { key: 'alias', label: t('Aliases', '别名'), sources: ['alias'] },
  { key: 'recent', label: t('Recently used', '最近使用'), sources: ['recent'] },
  { key: 'api', label: 'API', sources: ['api'] },
  { key: 'codex', label: 'Codex', sources: ['codex'] },
  { key: 'settings', label: t('Settings / environment', '设置 / 环境'), sources: ['settings', 'env'] },
];

export function groupModels<M extends Model>(models: readonly M[], query = ''): { key: string; label: string; items: M[] }[] {
  const q = query.trim().toLowerCase();
  const match = (m: M) => !q || m.id.toLowerCase().includes(q) || m.label.toLowerCase().includes(q);
  return MODEL_GROUPS
    .map(g => ({ key: g.key, label: g.label, items: models.filter(m => g.sources.includes(m.source) && match(m)) }))
    .filter(g => g.items.length > 0);
}

/** The selected model's advertised efforts (Codex) win over the provider's generic list. */
export function modelEfforts(models: readonly Model[] | undefined, model: string, provider: Provider): string[] {
  const selected = models?.find(m => m.id === model) || (!model ? models?.find(m => m.isDefault) : undefined);
  return provider === 'codex' && selected?.efforts?.length ? selected.efforts : providerEfforts(provider);
}

const CLAUDE_MODES: [string, string, string][] = [
  ['default', t('Default', '默认审批'), t('Ask for each approval', '每次都询问')],
  ['plan', t('Plan', '规划模式'), t('Plan only, no changes', '只规划不修改')],
  ['acceptEdits', t('Accept edits', '自动接受编辑'), t('Auto-accept file edits', '自动接受文件修改')],
  ['auto', t('Automatic', '自动审批'), t('Claude decides which calls to approve', 'Claude 自行判断并批准部分调用')],
  ['dontAsk', t('Do not ask', '不再询问'), t('Deny what is not allowed in advance', '未预先允许的直接拒绝')],
  ['bypassPermissions', t('Bypass approvals', '跳过审批'), t('Skip all approvals', '跳过所有审批')],
];

const CODEX_MODES: Record<string, [string, string]> = {
  'read-only': [t('Read only', '只读'), t('Read files; ask before writing', '可读文件，写入前询问')],
  'workspace-write': [t('Workspace write', '工作区可写'), t('Edit inside the workspace', '可修改工作区内的文件')],
  'danger-full-access': [t('Full access', '完全访问'), t('No sandbox restrictions', '不受沙箱限制')],
};

/** Permission modes for a provider. An unknown saved mode stays selectable. */
export function permissionOptions(provider: Provider, current = ''): PickerOption[] {
  const options: PickerOption[] = provider === 'codex'
    ? codexPermissions.map(value => ({
      value, label: CODEX_MODES[value][0], description: CODEX_MODES[value][1],
      ...(dangerousPermission(value, provider) ? { tag: t('Dangerous', '危险'), tone: 'danger' as const } : {}),
    }))
    : CLAUDE_MODES.map(([value, label, description]) => ({
      value, label, description,
      ...(value === 'bypassPermissions' ? { tag: t('Dangerous', '危险'), tone: 'danger' as const }
        : dangerousPermission(value, provider) ? { tag: t('Caution', '谨慎'), tone: 'warn' as const } : {}),
    }));
  if (current && !options.some(o => o.value === current)) options.push({ value: current, label: current, description: t('Existing native setting', '现有原生配置') });
  return options;
}

export function permissionLabel(mode: string | undefined, provider: Provider): string {
  if (!mode) return t('Default', '跟随默认');
  return permissionOptions(provider, mode).find(o => o.value === mode)?.label ?? mode;
}

/** What a dangerous mode lets the agent do, for the confirmation. */
export function dangerDetail(mode: string, provider: Provider): string {
  if (provider === 'codex') return t('Codex can access files and run commands without sandbox restrictions.', 'Codex 可以不受沙箱限制地访问文件和执行命令。');
  if (mode === 'bypassPermissions') return t('Claude will skip all tool approvals and can edit files and run commands directly.', 'Claude 将跳过所有工具审批，可以直接修改文件和执行命令。');
  if (mode === 'auto') return t('Claude will automatically decide and approve some tool calls.', 'Claude 将自动判断并批准部分工具调用。');
  return t('Tools not allowed in advance are denied without asking.', '未预先允许的工具会被直接拒绝，不再询问。');
}

/** Short model name for chips: "claude-opus-5-5" → "Opus 5.5" when no label is known. */
export function shortModel(model: string | undefined): string {
  if (!model) return '';
  const m = model.match(/^claude-([a-z]+)-(\d+)-(\d+)/i);
  if (m) return `${m[1][0].toUpperCase()}${m[1].slice(1)} ${m[2]}.${m[3]}`;
  if (/^[a-z]+$/.test(model)) return model[0].toUpperCase() + model.slice(1);
  return model;
}

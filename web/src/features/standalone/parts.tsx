// Pieces the standalone home, the host form and the host switcher share.
import { useState, useSyncExternalStore } from 'react';
import { Check, Copy, Eye, EyeOff, Plus, Shuffle } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { cn } from '@/components/lib/utils';
import { copyText } from '@/domain/clipboard';
import { PALETTE, addressesHidden, allowCommand, hideAddresses, onHiddenAddresses } from '@/lib/hosts';
import { t } from '@/lib/i18n';
import { HOST_ICONS, HostGlyph } from '@/features/settings/parts';

/** Whether host addresses are hidden on this device (for a screen shown or shared), and the switch. */
export function useHiddenAddresses(): [boolean, (on: boolean) => void] {
  return [useSyncExternalStore(onHiddenAddresses, addressesHidden), hideAddresses];
}

export function HideAddressesButton({ hidden, set, className }: { hidden: boolean; set: (on: boolean) => void; className?: string }) {
  const label = hidden ? t('Show addresses', '显示地址') : t('Hide addresses', '隐藏地址');
  return (
    <Button variant="ghost" size="icon-md" aria-pressed={hidden} aria-label={label} className={cn('max-md:size-9', className)}
      title={t(`${label} (on this device, before showing or sharing the screen)`, `${label}（本设备，展示或分享屏幕前用）`)} onClick={() => set(!hidden)}>
      {hidden ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
    </Button>
  );
}

/** How to let this page into a host: the command, copied on click. */
export function AllowHint() {
  const [copied, setCopied] = useState(false);
  const command = allowCommand();
  const copy = async () => {
    try { await copyText(command); setCopied(true); window.setTimeout(() => setCopied(false), 1600); } catch { /* clipboard refused */ }
  };
  return (
    <div className="flex flex-col gap-1.5 text-ui-xs leading-5 text-foreground-subtlest">
      <span>{t('Each host must allow this page. On the host, run (applies at once):', '每个宿主需允许本页面，在宿主上运行（立即生效）：')}</span>
      <button type="button" onClick={() => void copy()} title={t('Copy', '复制')}
        className="group flex items-start gap-2 rounded-lg border border-border/70 bg-surface px-2.5 py-1.5 text-left hover:bg-surface-hover">
        <code className="min-w-0 flex-1 break-all font-mono text-ui-xs text-foreground">{command}</code>
        <span className="flex shrink-0 items-center gap-1 text-foreground-subtle">
          {copied ? <Check className="size-3.5 text-success" /> : <Copy className="size-3.5" />}
          {copied ? t('Copied', '已复制') : t('Copy', '复制')}
        </span>
      </button>
      <span>{t('It replaces the frontends the host allows: list them all, separated by commas. A host started by hand takes --allow-origin instead.', '它会替换宿主允许的前端列表，有多个时用逗号一起列出。手动前台运行的宿主改用 --allow-origin 启动参数。')}</span>
    </div>
  );
}

/** The palette, any colour (the browser's picker) and, with random, a colour no other
 * host shows. value is #rrggbb, or '' for the host's own. Host colours are user data. */
export function ColorChoices({ value, onChange, auto, random }: { value: string; onChange: (color: string) => void; auto: string; random: () => string }) {
  const chosen = value.toLowerCase();
  const custom = !!chosen && !PALETTE.includes(chosen);
  const swatch = 'relative flex size-7 items-center justify-center rounded-full ring-offset-2 ring-offset-card max-md:size-9';
  return (
    <div role="radiogroup" aria-label={t('Colour', '颜色')} className="flex flex-wrap items-center gap-1.5">
      <button type="button" role="radio" aria-checked={!chosen} title={t("The host's own colour", '宿主自己的颜色')} onClick={() => onChange('')}
        className={cn(swatch, 'w-auto gap-1.5 border border-border px-2.5 text-ui-xs text-foreground-subtle', !chosen && 'ring-2 ring-foreground/60')}>
        <span className="size-3 rounded-full" style={{ background: auto }} />{t('Host', '宿主')}
      </button>
      {PALETTE.map(c => (
        <button key={c} type="button" role="radio" aria-checked={chosen === c} aria-label={c} title={c} onClick={() => onChange(c)}
          className={cn(swatch, chosen === c && 'ring-2 ring-foreground/60')} style={{ background: c }}>
          {chosen === c && <Check className="size-3.5 text-white" />}
        </button>
      ))}
      <label title={t('Any colour', '自定义颜色')} className={cn(swatch, 'cursor-pointer border border-dashed border-border-hover', custom && 'border-transparent ring-2 ring-foreground/60')} style={custom ? { background: chosen } : undefined}>
        <input type="color" className="sr-only" value={chosen || auto} onChange={e => onChange(e.target.value.toLowerCase())} aria-label={t('Any colour', '自定义颜色')} />
        {custom ? <Check className="size-3.5 text-white" /> : <Plus className="size-3.5 text-foreground-subtle" />}
      </label>
      <Button type="button" variant="ghost" size="sm" className="max-md:h-9" title={t('A colour no other host shows', '挑一个其他宿主没有用的颜色')} onClick={() => onChange(random())}>
        <Shuffle className="size-3.5" />{t('Random', '随机')}
      </Button>
    </div>
  );
}

const MAX_ICON = 8;

/** The preset glyphs and a few characters or an emoji of one's own; '' is the host's own. */
export function IconChoices({ value, onChange, color, auto, mobile }: { value: string; onChange: (icon: string) => void; color: string; auto: string; mobile: boolean }) {
  const text = value && !HOST_ICONS[value] ? value : '';
  const item = 'flex size-8 items-center justify-center rounded-lg border transition-colors max-md:size-10';
  return (
    <div className="flex flex-col gap-2">
      <div role="radiogroup" aria-label={t('Icon', '图标')} className="flex flex-wrap gap-1.5">
        <button type="button" role="radio" aria-checked={!value} title={t("The host's own icon", '宿主自己的图标')} onClick={() => onChange('')}
          className={cn(item, 'w-auto gap-1.5 px-1.5 text-ui-xs text-foreground-subtle', !value ? 'border-foreground/50' : 'border-border/70 hover:bg-surface-hover')}>
          <HostGlyph icon={auto || 'code'} color={color} className="size-5 rounded-md [&_svg]:size-3" />{t('Host', '宿主')}
        </button>
        {Object.entries(HOST_ICONS).map(([key, { icon: Icon, label }]) => {
          const on = value === key;
          return (
            <button key={key} type="button" role="radio" aria-checked={on} aria-label={label()} title={label()} onClick={() => onChange(key)}
              className={cn(item, on ? 'border-transparent text-white' : 'border-border/70 text-foreground-subtle hover:bg-surface-hover hover:text-foreground')}
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

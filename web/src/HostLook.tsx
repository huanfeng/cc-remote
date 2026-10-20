// Choosing how a host looks: its colour and icon, in the host's settings and, over
// them, in the standalone page's host editor.
import { PALETTE } from './hosts';
import { ICONS, MAX_ICON_LENGTH, iconURL, isPreset } from './hostIcon';
import { t } from './i18n';

/** What the first choice stands for: the default, or the host's own setting. */
type Auto = { label: string; title: string; color: string; icon?: string };

/** The palette, any colour (the browser's picker) and, with random, a colour no other
 * host shows. value is #rrggbb, or '' for auto. */
export function ColorChoices({ value, onChange, auto, random }: { value: string; onChange: (color: string) => void; auto: Auto; random?: () => string }) {
  const chosen = value.toLowerCase();
  const custom = !!chosen && !PALETTE.includes(chosen);
  return <div className="host-colors" role="radiogroup" aria-label={t('Color', '颜色')}>
    <button type="button" role="radio" aria-checked={!chosen} className={`host-color auto${!chosen ? ' chosen' : ''}`} style={{ background: auto.color }} title={auto.title} onClick={() => onChange('')}>{auto.label}</button>
    {PALETTE.map(c => <button key={c} type="button" role="radio" aria-checked={chosen === c} aria-label={c} title={c} className={`host-color${chosen === c ? ' chosen' : ''}`} style={{ background: c }} onClick={() => onChange(c)} />)}
    <label className={`host-color custom${custom ? ' chosen' : ''}`} style={custom ? { background: chosen } : undefined} title={t('Any color', '自定义颜色')}>
      <input type="color" value={chosen || auto.color} onChange={event => onChange(event.target.value.toLowerCase())} aria-label={t('Any color', '自定义颜色')} />
      <span aria-hidden="true">{custom ? '' : '+'}</span>
    </label>
    {random && <button type="button" className="host-color auto random" title={t('A color no other host shows', '挑一个其他宿主没有用的颜色')} onClick={() => onChange(random())}>{t('Random', '随机')}</button>}
  </div>;
}

const ICON_LABELS: Record<string, () => string> = {
  code: () => t('Logo', '标志'), server: () => t('Server', '服务器'), laptop: () => t('Laptop', '笔记本'), desktop: () => t('Desktop', '台式机'),
  home: () => t('Home', '家'), cloud: () => t('Cloud', '云'), terminal: () => t('Terminal', '终端'), database: () => t('Database', '数据库'),
  flask: () => t('Lab', '实验'), globe: () => t('Globe', '网络'), package: () => t('Package', '包'), shield: () => t('Shield', '盾牌'),
  bolt: () => t('Bolt', '闪电'), star: () => t('Star', '星'), rocket: () => t('Rocket', '火箭'), game: () => t('Game', '游戏'),
};

/** The preset glyphs, drawn in color, and a few characters or an emoji of one's own.
 * value is a preset name, the text, or '' for auto; withLogo offers the logo as a choice
 * (when auto is something else). */
export function IconChoices({ value, onChange, color, auto, withLogo }: { value: string; onChange: (icon: string) => void; color: string; auto: Auto; withLogo?: boolean }) {
  const text = value && !isPreset(value) ? value : '';
  return <>
    <div className="host-icons" role="radiogroup" aria-label={t('Icon', '图标')}>
      <button type="button" role="radio" aria-checked={!value} className={`host-icon-choice auto${!value ? ' chosen' : ''}`} title={auto.title} onClick={() => onChange('')}><img src={iconURL(color, auto.icon)} alt="" /><span>{auto.label}</span></button>
      {ICONS.filter(icon => withLogo || icon !== 'code').map(icon => <button key={icon} type="button" role="radio" aria-checked={value === icon} aria-label={ICON_LABELS[icon]?.() || icon} title={ICON_LABELS[icon]?.() || icon} className={`host-icon-choice${value === icon ? ' chosen' : ''}`} onClick={() => onChange(icon)}><img src={iconURL(color, icon)} alt="" /></button>)}
    </div>
    <label>{t('Or a few characters or an emoji', '或使用几个字符、一个 emoji')}<span className="host-icon-text">{text && <img src={iconURL(color, text)} alt="" />}<input value={text} placeholder={t('Such as P1, 家 or 🚀', '例如 P1、家、🚀')} onChange={event => onChange(Array.from(event.target.value.trim()).slice(0, MAX_ICON_LENGTH).join(''))} /></span></label>
  </>;
}

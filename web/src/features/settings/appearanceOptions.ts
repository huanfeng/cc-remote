import type { Palette, TerminalPalette, ThemePref } from '@/domain/preferences';
import { t } from '@/lib/i18n';

export const appThemes = ['system', 'light', 'dark'] as const;
export const appThemeLabels: Record<ThemePref, string> = {
  system: t('System', '跟随系统'),
  light: t('Light', '浅色'),
  dark: t('Dark', '深色'),
};

export const appPalettes = ['graphite', 'default'] as const;
export const appPaletteLabels: Record<Palette, string> = {
  graphite: t('Graphite', '石墨'),
  default: t('Classic', '经典'),
};

export const terminalPaletteLabels: Record<TerminalPalette, string> = {
  dark: t('Dark · default', '深色 · 默认'),
  light: t('Light', '浅色'),
  ocean: t('Ocean', '深海蓝'),
  warm: t('Warm', '暖棕'),
  app: t('Follow app theme', '跟随应用主题'),
};

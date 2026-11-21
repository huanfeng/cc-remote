import type { ReactElement } from 'react';
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem,
  DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { terminalPalettes } from '@/domain/preferences';
import { t } from '@/lib/i18n';
import { usePrefs } from '@/lib/prefs';
import { appPalettes, appPaletteLabels, appThemes, appThemeLabels, terminalPaletteLabels } from './appearanceOptions';

export function AppearanceMenu({ trigger, mobile }: { trigger: ReactElement; mobile: boolean }) {
  const theme = usePrefs(s => s.theme);
  const palette = usePrefs(s => s.palette);
  const terminalPalette = usePrefs(s => s.terminalPalette);
  const set = usePrefs(s => s.set);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="end" sideOffset={6} className="w-48">
        <AppearanceChoices label={t('App theme', '应用主题')} value={theme} values={appThemes} labels={appThemeLabels} mobile={mobile} onChange={theme => set({ theme })} />
        <DropdownMenuSeparator />
        <AppearanceChoices label={t('App colours', '应用配色')} value={palette} values={appPalettes} labels={appPaletteLabels} mobile={mobile} onChange={palette => set({ palette })} />
        <DropdownMenuSeparator />
        <AppearanceChoices label={t('Terminal colours', '终端配色')} value={terminalPalette} values={terminalPalettes} labels={terminalPaletteLabels} mobile={mobile} onChange={terminalPalette => set({ terminalPalette })} />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AppearanceChoices<T extends string>({ label, value, values, labels, mobile, onChange }: {
  label: string;
  value: T;
  values: readonly T[];
  labels: Record<T, string>;
  mobile: boolean;
  onChange(value: T): void;
}) {
  return (
    <DropdownMenuRadioGroup aria-label={label} value={value}>
      <DropdownMenuLabel className="py-0.5 text-ui-xs">{label}</DropdownMenuLabel>
      {values.map(option => (
        <DropdownMenuRadioItem key={option} value={option} className={mobile ? 'min-h-10' : undefined}
          onSelect={event => {
            // Keep the menu available for previewing or changing another group.
            event.preventDefault();
            if (option !== value) onChange(option);
          }}>
          {labels[option]}
        </DropdownMenuRadioItem>
      ))}
    </DropdownMenuRadioGroup>
  );
}

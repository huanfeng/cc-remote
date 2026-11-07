// The pages before a host is open (sign-in, the standalone home): one card in
// the middle of the window, the language switch in its corner, and the
// frontend's version below on the standalone page.
import type { ReactNode } from 'react';
import { cn } from '@/components/lib/utils';
import { FRONTEND_VERSION, STANDALONE } from '@/lib/hosts';
import { hostLang, lang, setLang, t, type Lang } from '@/lib/i18n';
import { HostGlyph } from '@/features/settings/parts';

export function AuthFrame({ children, wide }: { children: ReactNode; wide?: boolean }) {
  return (
    <main className="flex h-full min-h-0 flex-col overflow-y-auto bg-background-win-alt pt-safe pb-safe text-foreground">
      <div className="m-auto flex w-full flex-col items-center gap-3 px-4 py-8">
        <div className={cn('relative w-full rounded-2xl border border-border/70 bg-card p-6 shadow-sm max-sm:p-5', wide ? 'max-w-[520px]' : 'max-w-[420px]')}>
          <LangSwitch className="absolute right-4 top-4" />
          {children}
        </div>
        {STANDALONE && <small className="text-ui-xs text-foreground-subtlest">{t(`Frontend ${FRONTEND_VERSION}`, `前端版本 ${FRONTEND_VERSION}`)}</small>}
      </div>
    </main>
  );
}

/** The host's glyph, name and a line under it: the head of the sign-in card. */
export function AuthHeading({ name, color, icon, children }: { name: string; color: string; icon: string; children?: ReactNode }) {
  return (
    <div className="flex flex-col items-start gap-3 pr-20">
      <HostGlyph icon={icon || 'code'} color={color} className="size-11 rounded-xl [&_svg]:size-5" />
      <div className="min-w-0">
        <h1 className="truncate text-ui-xl font-semibold tracking-tight">{name}</h1>
        {children && <p className="mt-1 text-ui-sm leading-5 text-foreground-subtle">{children}</p>}
      </div>
    </div>
  );
}

/** 中文 / EN: the page's language, saved in this browser (the page reloads). */
export function LangSwitch({ className }: { className?: string }) {
  const choose = (value: Lang) => {
    if (value === lang) return;
    // Back to following the host when the choice is what it serves anyway.
    setLang(value === hostLang ? '' : value);
  };
  return (
    <div role="radiogroup" aria-label={t('Language', '语言')} className={cn('flex items-center rounded-lg bg-surface p-0.5', className)}>
      {(['zh', 'en'] as const).map(v => (
        <button key={v} type="button" role="radio" aria-checked={lang === v} onClick={() => choose(v)}
          className={cn('h-6 rounded-md px-2 text-ui-xs font-medium transition-colors max-md:h-8 max-md:px-2.5', lang === v ? 'bg-card text-foreground shadow-xs' : 'text-foreground-subtle hover:text-foreground')}>
          {v === 'zh' ? '中文' : 'EN'}
        </button>
      ))}
    </div>
  );
}

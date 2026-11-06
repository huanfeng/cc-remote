// Shared settings building blocks: the dirty-state context the page uses for
// its unsaved-change guard, section headers and the host glyphs.
import { createContext, useContext, useEffect, type ReactNode } from 'react';
import {
  Cloud, Code2, Database, FlaskConical, Gamepad2, Globe, Home, Laptop, Monitor, Package, Rocket, Server, Shield, Star, SquareTerminal, Zap,
  type LucideIcon,
} from 'lucide-react';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';

/**
 * Sections report unsaved edits here; leaving the section or the page asks
 * first. guard(run) runs run at once, or after the user agrees to discard.
 */
export const DirtyContext = createContext<{ report: (key: string, dirty: boolean) => void; guard: (run: () => void) => void }>({
  report: () => {},
  guard: run => run(),
});

export function useReportDirty(key: string, dirty: boolean) {
  const { report } = useContext(DirtyContext);
  useEffect(() => { report(key, dirty); }, [report, key, dirty]);
  useEffect(() => () => report(key, false), [report, key]);
}

export const useGuard = () => useContext(DirtyContext).guard;

export function SectionHeader({ title, description, actions }: { title: string; description?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-4 flex items-start gap-3">
      <div className="min-w-0 flex-1">
        <h2 className="text-ui-xl font-semibold tracking-tight">{title}</h2>
        {description && <p className="mt-1 text-ui-sm leading-5 text-foreground-subtle">{description}</p>}
      </div>
      {actions}
    </div>
  );
}

export function GroupTitle({ children }: { children: ReactNode }) {
  return <h3 className="mb-2 mt-6 px-1 text-ui-sm font-medium text-foreground-subtle first:mt-0">{children}</h3>;
}

export const HOST_ICONS: Record<string, { icon: LucideIcon; label: () => string }> = {
  laptop: { icon: Laptop, label: () => t('Laptop', '笔记本') },
  desktop: { icon: Monitor, label: () => t('Desktop', '台式机') },
  server: { icon: Server, label: () => t('Server', '服务器') },
  cloud: { icon: Cloud, label: () => t('Cloud', '云') },
  home: { icon: Home, label: () => t('Home', '家') },
  terminal: { icon: SquareTerminal, label: () => t('Terminal', '终端') },
  code: { icon: Code2, label: () => t('Code', '代码') },
  database: { icon: Database, label: () => t('Database', '数据库') },
  flask: { icon: FlaskConical, label: () => t('Lab', '实验') },
  globe: { icon: Globe, label: () => t('Globe', '网络') },
  package: { icon: Package, label: () => t('Package', '包') },
  shield: { icon: Shield, label: () => t('Shield', '盾牌') },
  bolt: { icon: Zap, label: () => t('Bolt', '闪电') },
  star: { icon: Star, label: () => t('Star', '星') },
  rocket: { icon: Rocket, label: () => t('Rocket', '火箭') },
  game: { icon: Gamepad2, label: () => t('Game', '游戏') },
};

/** The host's glyph on its colour: a preset icon, or a few characters / an emoji. */
export function HostGlyph({ icon, color, className }: { icon: string; color: string; className?: string }) {
  const preset = HOST_ICONS[icon || 'laptop'];
  // The host colour is user data, not a theme colour.
  return (
    <span className={cn('flex size-8 shrink-0 items-center justify-center rounded-lg text-white', className)} style={{ background: color }}>
      {preset ? <preset.icon className="size-4" /> : <span className="text-ui-sm font-semibold leading-none">{icon}</span>}
    </span>
  );
}

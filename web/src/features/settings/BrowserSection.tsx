// This browser: language, theme and palette, send key, notifications. All of
// it is stored in this browser only and applies at once.
import { useState, useSyncExternalStore } from 'react';
import { Check } from 'lucide-react';
import { Switch } from '@/components/ui/switch';
import { cn } from '@/components/lib/utils';
import { usePrefs, type ThemePref } from '@/lib/prefs';
import { useSystemDark } from '@/lib/theme';
import { hostLang, langOverride, setLang, t } from '@/lib/i18n';
import { notifySupported } from '@/domain/notify';
import { GroupCard, Row } from '@/features/dialogs/SurfaceShell';
import { SimpleSelect } from '@/features/dialogs/common';
import { GroupTitle, SectionHeader } from './parts';

export function BrowserSection({ mobile }: { mobile: boolean }) {
  const set = usePrefs(s => s.set);
  const theme = usePrefs(s => s.theme);
  const palette = usePrefs(s => s.palette);
  const sendKey = usePrefs(s => s.sendKey);
  const [language] = useState(langOverride());
  return (
    <div>
      {!mobile && <SectionHeader title={t('This browser', '本浏览器')} description={t('Stored in this browser only.', '只保存在此浏览器中。')} />}

      <GroupTitle>{t('Appearance', '外观')}</GroupTitle>
      <GroupCard>
        <Row label={t('Theme', '主题')} stack>
          <div role="radiogroup" aria-label={t('Theme', '主题')} className="grid grid-cols-3 gap-2 sm:max-w-md">
            {(['system', 'light', 'dark'] as ThemePref[]).map(v => (
              <ThemeCard key={v} value={v} active={theme === v} onClick={() => set({ theme: v })} />
            ))}
          </div>
        </Row>
        <Row
          label={t('Colour palette', '配色')}
          control={<SimpleSelect value={palette} onChange={v => set({ palette: v === 'default' ? 'default' : 'graphite' })} ariaLabel={t('Colour palette', '配色')}
            options={[{ value: 'graphite', label: t('Graphite', '石墨') }, { value: 'default', label: t('Classic', '经典') }]} />}
        />
        <Row
          label={t('Language', '语言')}
          description={t('Changing it reloads the page.', '切换后会重新加载页面。')}
          control={<SimpleSelect value={language} onChange={v => setLang(v === 'zh' || v === 'en' ? v : '')} ariaLabel={t('Language', '语言')}
            options={[
              { value: '', label: t(`Follow host (${hostLang === 'zh' ? '中文' : 'English'})`, `跟随宿主（${hostLang === 'zh' ? '中文' : 'English'}）`) },
              { value: 'zh', label: '中文' },
              { value: 'en', label: 'English' },
            ]} />}
        />
      </GroupCard>

      <GroupTitle>{t('Input', '输入')}</GroupTitle>
      <GroupCard>
        <Row
          label={t('Send with', '发送键')}
          description={sendKey === 'enter' ? t('Shift+Enter starts a new line.', 'Shift+Enter 换行。') : t('Enter starts a new line.', 'Enter 换行。')}
          control={<SimpleSelect value={sendKey} onChange={v => set({ sendKey: v === 'mod-enter' ? 'mod-enter' : 'enter' })} ariaLabel={t('Send with', '发送键')}
            options={[{ value: 'enter', label: 'Enter' }, { value: 'mod-enter', label: 'Ctrl / ⌘ + Enter' }]} />}
        />
      </GroupCard>

      <GroupTitle>{t('Notifications', '通知')}</GroupTitle>
      <Notifications />
    </div>
  );
}

function subscribePermission(cb: () => void) {
  // Permission changes have no event in every browser; re-check on focus.
  window.addEventListener('focus', cb);
  return () => window.removeEventListener('focus', cb);
}
const permissionNow = () => (typeof Notification === 'undefined' ? 'denied' : Notification.permission);

function Notifications() {
  const set = usePrefs(s => s.set);
  const n = usePrefs(s => s.notify);
  const supported = notifySupported();
  const permission = useSyncExternalStore(subscribePermission, permissionNow, () => 'default' as NotificationPermission);
  const [asked, setAsked] = useState<NotificationPermission>();
  const effective = asked ?? permission;
  const blocked = supported && effective === 'denied';

  const toggle = async (on: boolean) => {
    if (!on) { set({ notify: { ...n, on: false } }); return; }
    let p = permissionNow();
    if (p === 'default') { p = await Notification.requestPermission(); setAsked(p); }
    set({ notify: { ...n, on: p === 'granted' } });
  };

  const items: { key: 'waiting' | 'done' | 'failed'; label: string }[] = [
    { key: 'waiting', label: t('Needs my answer: a permission, a question or a plan', '需要我操作：权限审批、提问、计划确认') },
    { key: 'done', label: t('A turn finished', '一轮执行完成') },
    { key: 'failed', label: t('A turn failed or Claude Code exited', '执行失败或 Claude Code 退出') },
  ];

  return (
    <GroupCard>
      <Row
        label={t('System notifications', '系统通知')}
        description={!supported
          ? t('This page cannot show notifications: browsers allow them only over HTTPS (or on localhost).', '此页面无法发送通知：浏览器只允许 HTTPS（或 localhost）页面发通知。')
          : blocked
            ? <span className="text-warning">{t('The browser blocks notifications for this site; allow them in the site settings, then reload.', '浏览器已禁止此网站的通知；请在网站设置里允许通知，然后刷新页面。')}</span>
            : t('While this page is open, never for the session you are looking at.', '页面打开期间提醒，正在查看的会话不提醒。')}
        control={<Switch checked={n.on && supported && !blocked} disabled={!supported || blocked} onCheckedChange={v => void toggle(v)} aria-label={t('System notifications', '系统通知')} />}
      />
      {items.map(item => (
        <Row
          key={item.key}
          label={<span className={cn('font-normal', !n.on && 'text-foreground-subtle')}>{item.label}</span>}
          control={<Switch size="sm" checked={n[item.key]} disabled={!n.on} onCheckedChange={v => set({ notify: { ...n, [item.key]: v } })} aria-label={item.label} />}
          className="py-2.5 pl-8"
        />
      ))}
    </GroupCard>
  );
}

/**
 * A miniature of the app in light or dark. A preview has to show the other
 * mode regardless of the current one, so it is drawn in fixed neutral greys
 * rather than the (current-mode) theme tokens.
 */
function ThemeCard({ value, active, onClick }: { value: ThemePref; active: boolean; onClick: () => void }) {
  const systemDark = useSystemDark();
  const label = value === 'system' ? t('System', '跟随系统') : value === 'light' ? t('Light', '浅色') : t('Dark', '深色');
  return (
    <button type="button" role="radio" aria-checked={active} onClick={onClick} className="group flex flex-col gap-1.5 text-left">
      <span className={cn('relative block aspect-[16/10] overflow-hidden rounded-lg border transition-shadow', active ? 'border-transparent ring-2 ring-foreground/70' : 'border-border group-hover:border-border-hover')}>
        {value === 'system' ? (
          <span className="absolute inset-0 flex">
            <span className="relative w-1/2 overflow-hidden"><Mini dark={false} /></span>
            <span className="relative w-1/2 overflow-hidden"><span className="absolute inset-y-0 right-0 w-[200%]"><Mini dark /></span></span>
          </span>
        ) : <Mini dark={value === 'dark'} />}
        {active && <span className="absolute right-1 top-1 flex size-4 items-center justify-center rounded-full bg-foreground text-background"><Check className="size-2.5" /></span>}
      </span>
      <span className="flex items-center gap-1 text-ui-sm text-foreground">
        {label}
        {value === 'system' && <span className="text-ui-xs text-foreground-subtlest">· {systemDark ? t('dark now', '当前深色') : t('light now', '当前浅色')}</span>}
      </span>
    </button>
  );
}

function Mini({ dark }: { dark: boolean }) {
  return (
    <span className={cn('absolute inset-0 flex', dark ? 'bg-neutral-900' : 'bg-white')}>
      <span className={cn('flex w-[32%] flex-col gap-1 p-1.5', dark ? 'bg-neutral-800' : 'bg-neutral-100')}>
        <span className={cn('h-1 w-3/4 rounded-full', dark ? 'bg-neutral-600' : 'bg-neutral-300')} />
        <span className={cn('h-1 w-1/2 rounded-full', dark ? 'bg-neutral-700' : 'bg-neutral-200')} />
        <span className={cn('h-1 w-2/3 rounded-full', dark ? 'bg-neutral-700' : 'bg-neutral-200')} />
      </span>
      <span className="flex flex-1 flex-col gap-1 p-1.5">
        <span className={cn('h-1 w-1/2 rounded-full', dark ? 'bg-neutral-600' : 'bg-neutral-300')} />
        <span className={cn('h-1 w-5/6 rounded-full', dark ? 'bg-neutral-700' : 'bg-neutral-200')} />
        <span className={cn('ml-auto mt-auto h-2.5 w-1/2 rounded', dark ? 'bg-neutral-700' : 'bg-neutral-200')} />
      </span>
    </span>
  );
}

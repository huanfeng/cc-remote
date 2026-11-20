// The host's name, colour and icon tell several hosts apart: the tab title and
// icon, --host-color, the sidebar and the login page. The host serves them in
// <html data-host-name data-host-color data-host-icon> before anyone signs in;
// a standalone page starts from what it remembered of the current host. Once
// connected, /api/info (and the settings) keep them current.
import { useSyncExternalStore } from 'react';
import { currentHost, updateCurrentHost } from './hosts';
import { iconSvg } from './hostIcon';
import { attentionIcon, attentionTitle, type AttentionCounts } from '@/domain/attention';
import { lang } from './i18n';

export type Identity = { name: string; color: string; icon: string };

const root = typeof document !== 'undefined' ? document.documentElement : undefined;
const BRAND = 'CC Remote';

const own: Identity = {
  name: currentHost?.name || root?.dataset.hostName || BRAND,
  color: currentHost?.color || root?.dataset.hostColor || '#293c2e',
  icon: currentHost?.icon ?? root?.dataset.hostIcon ?? '',
};
// The name, colour and icon this page gave the host (its alias, tint and emblem) win over the host's.
const shown = (id: Identity): Identity => ({ name: currentHost?.alias || id.name, color: currentHost?.tint || id.color, icon: currentHost?.emblem || id.icon });

let current = shown(own);
let attention: AttentionCounts = { waiting: 0, done: 0, total: 0 };
const listeners = new Set<() => void>();

function showTitle() {
  if (!root) return;
  document.title = attentionTitle(current.name === BRAND ? BRAND : `${current.name} · ${BRAND}`, attention, lang);
}

function showIcon() {
  if (!root) return;
  const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (icon) icon.href = `data:image/svg+xml,${encodeURIComponent(attentionIcon(iconSvg(current.color, current.icon), attention))}`;
}

/** Shows the host's identity (its own; this page's alias, tint and emblem win) in the
 * title, tab icon and --host-color, and remembers it for the standalone host list. */
export function applyIdentity(hostId: Identity) {
  if (!root) return;
  if (currentHost && (hostId.name !== currentHost.name || hostId.color !== currentHost.color || hostId.icon !== (currentHost.icon || ''))) {
    Object.assign(currentHost, { name: hostId.name, color: hostId.color, icon: hostId.icon || undefined });
    updateCurrentHost({ name: hostId.name, color: hostId.color, icon: hostId.icon || undefined });
  }
  const next = shown(hostId);
  const changed = next.name !== current.name || next.color !== current.color || next.icon !== current.icon;
  current = next;
  showTitle();
  root.style.setProperty('--host-color', next.color);
  // Drawn here (a standalone page's CSP loads no image from a host), as the host draws it.
  showIcon();
  if (changed) listeners.forEach(f => f());
}

/** Unread results and pending decisions are independent of popup-notification preferences. */
export function setTitleAttention(counts: AttentionCounts) {
  if (counts.waiting === attention.waiting && counts.done === attention.done) return;
  attention = counts;
  showTitle();
  showIcon();
}

const subscribe = (f: () => void) => { listeners.add(f); return () => { listeners.delete(f); }; };

/** The identity shown for the current host, updated as it changes. */
export const useIdentity = () => useSyncExternalStore(subscribe, () => current, () => current);

if (root) applyIdentity(own);

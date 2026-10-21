// The host's name, colour and icon tell several hosts apart: the tab title and
// icon, the sidebar logo and the login page. The host serves them in <html
// data-host-name data-host-color data-host-icon>; the settings change them while
// the page is open.
export type HostIdentity = { name: string; color: string; icon: string };

import { currentHost, updateCurrentHost } from './hosts';
import { iconURL } from './hostIcon';

const EVENT = 'cc-host-identity';
const root = typeof document !== 'undefined' ? document.documentElement : undefined;

// A standalone page starts from what it remembered of the current host.
const own: HostIdentity = { name: currentHost?.name || root?.dataset.hostName || 'CC Remote', color: currentHost?.color || root?.dataset.hostColor || '#293c2e', icon: currentHost?.icon ?? root?.dataset.hostIcon ?? '' };
// The name, colour and icon this page gave the host (its alias, tint and emblem) win over the host's.
const shown = (id: HostIdentity): HostIdentity => ({ name: currentHost?.alias || id.name, color: currentHost?.tint || id.color, icon: currentHost?.emblem || id.icon });
export const initialIdentity = shown(own);

/** Shows the host's identity (id: its own; the page's alias, tint and emblem win) in the
 * title, tab icon and --host-color, and tells listeners what is shown. */
export function applyIdentity(hostId: HostIdentity) {
  if (!root) return;
  if (currentHost && (hostId.name !== currentHost.name || hostId.color !== currentHost.color || hostId.icon !== (currentHost.icon || ''))) updateCurrentHost({ name: hostId.name, color: hostId.color, icon: hostId.icon || undefined });
  const id = shown(hostId);
  shownName = id.name;
  showTitle();
  root.style.setProperty('--host-color', id.color);
  const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  // Drawn here (a standalone page's CSP loads no image from a host), as the host draws it.
  if (icon) icon.href = iconURL(id.color, id.icon);
  window.dispatchEvent(new CustomEvent<HostIdentity>(EVENT, { detail: id }));
}

let shownName = '', badge = 0;
const showTitle = () => { document.title = (badge ? `(${badge}) ` : '') + (shownName === 'CC Remote' ? shownName : `${shownName} · CC Remote`); };

/** Puts the number of sessions waiting for an answer in front of the tab title. */
export function setTitleBadge(count: number) {
  if (count === badge) return;
  badge = count;
  showTitle();
}

/** Calls listener whenever the identity changes; returns the unsubscribe function. */
export function onIdentity(listener: (id: HostIdentity) => void) {
  const handler = (event: Event) => listener((event as CustomEvent<HostIdentity>).detail);
  window.addEventListener(EVENT, handler);
  return () => window.removeEventListener(EVENT, handler);
}

applyIdentity(own);

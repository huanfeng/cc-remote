// The host's name and colour tell several hosts apart: the tab title and icon,
// the sidebar logo and the login page. The host serves them in <html
// data-host-name data-host-color>; the settings change them while the page is open.
export type HostIdentity = { name: string; color: string };

import { STANDALONE, apiUrl, currentHost, updateCurrentHost } from './hosts';
import iconSvg from '../public/icon.svg?raw';

const EVENT = 'cc-host-identity';
const root = typeof document !== 'undefined' ? document.documentElement : undefined;

// A standalone page starts from what it remembered of the current host.
const own: HostIdentity = { name: currentHost?.name || root?.dataset.hostName || 'CC Remote', color: currentHost?.color || root?.dataset.hostColor || '#293c2e' };
// The name and colour this page gave the host (its alias and tint) win over the host's.
const shown = (id: HostIdentity): HostIdentity => ({ name: currentHost?.alias || id.name, color: currentHost?.tint || id.color });
export const initialIdentity = shown(own);

/** Shows the host's identity (id: its own; the page's alias and tint win) in the title,
 * tab icon and --host-color, and tells listeners what is shown. */
export function applyIdentity(hostId: HostIdentity) {
  if (!root) return;
  if (currentHost && (hostId.name !== currentHost.name || hostId.color !== currentHost.color)) updateCurrentHost({ name: hostId.name, color: hostId.color });
  const id = shown(hostId);
  document.title = id.name === 'CC Remote' ? id.name : `${id.name} · CC Remote`;
  root.style.setProperty('--host-color', id.color);
  const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  // The standalone page colours the icon itself: its CSP loads no image from a host.
  if (icon) icon.href = STANDALONE ? `data:image/svg+xml,${encodeURIComponent(iconSvg.replace('fill="#222c27"', `fill="${id.color}"`))}` : apiUrl(`/host-icon.svg?c=${id.color.replace('#', '')}`);
  window.dispatchEvent(new CustomEvent<HostIdentity>(EVENT, { detail: id }));
}

/** Calls listener whenever the identity changes; returns the unsubscribe function. */
export function onIdentity(listener: (id: HostIdentity) => void) {
  const handler = (event: Event) => listener((event as CustomEvent<HostIdentity>).detail);
  window.addEventListener(EVENT, handler);
  return () => window.removeEventListener(EVENT, handler);
}

applyIdentity(own);

// The host's name and colour tell several hosts apart: the tab title and icon,
// the sidebar logo and the login page. The host serves them in <html
// data-host-name data-host-color>; the settings change them while the page is open.
export type HostIdentity = { name: string; color: string };

const EVENT = 'cc-host-identity';
const root = typeof document !== 'undefined' ? document.documentElement : undefined;

export const initialIdentity: HostIdentity = { name: root?.dataset.hostName || 'CC Remote', color: root?.dataset.hostColor || '#293c2e' };

/** Shows the identity in the title, tab icon and --host-color, and tells listeners. */
export function applyIdentity(id: HostIdentity) {
  if (!root) return;
  document.title = `${id.name} · CC Remote`;
  root.style.setProperty('--host-color', id.color);
  const icon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (icon) icon.href = `/host-icon.svg?c=${id.color.replace('#', '')}`;
  window.dispatchEvent(new CustomEvent<HostIdentity>(EVENT, { detail: id }));
}

/** Calls listener whenever the identity changes; returns the unsubscribe function. */
export function onIdentity(listener: (id: HostIdentity) => void) {
  const handler = (event: Event) => listener((event as CustomEvent<HostIdentity>).detail);
  window.addEventListener(EVENT, handler);
  return () => window.removeEventListener(EVENT, handler);
}

applyIdentity(initialIdentity);

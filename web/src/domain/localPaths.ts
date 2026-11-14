/** Local OS actions are offered only for actual loopback hosts, never LAN/remote hosts. */
export function isLoopbackHost(hostUrl: string): boolean {
  if (/[\u0000-\u001f\u007f-\u009f]/.test(hostUrl)) return false;
  try {
    const url = new URL(hostUrl);
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return false;
    return url.hostname === 'localhost' || url.hostname === '[::1]' || /^127(?:\.\d{1,3}){3}$/.test(url.hostname);
  } catch { return false; }
}

export function isAbsoluteLocalPath(path: string): boolean {
  if (!path || /[\u0000-\u001f\u007f-\u009f]/.test(path) || path.startsWith('//') || path.startsWith('\\')) return false;
  if (/^[a-z]:[\\/]/i.test(path)) return !path.slice(2).includes(':');
  return path.startsWith('/');
}

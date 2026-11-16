/** Preserve the page's host identity and waiting badge in the native title. */
export function desktopWindowTitle(pageTitle: string, version?: string): string {
  const title = pageTitle.trim() || 'CC Remote';
  return version ? `${title} v${version}` : title;
}

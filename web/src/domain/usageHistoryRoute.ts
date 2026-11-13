/** A history link contains only the opaque account id, never credentials. */
export function usageHistoryHash(account?: string): string {
  return '#/usage-history' + (account ? `?account=${encodeURIComponent(account)}` : '');
}

export function isUsageHistory(hash: string): boolean {
  return hash === '#/usage-history' || hash.startsWith('#/usage-history?');
}

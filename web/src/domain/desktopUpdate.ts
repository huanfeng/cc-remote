/** A status request may finish after a newer event from another window. */
export function isCurrentUpdate(previous: { revision: number } | null, incoming: { revision: number }): boolean {
  return !previous || incoming.revision >= previous.revision;
}

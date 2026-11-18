// Keep shortcuts saved by earlier clients pointing to the same content.
export function settingsDestination<T extends string>(section: T | undefined): T | 'claude' | 'suppliers' | undefined {
  if (section === 'defaults') return 'claude';
  if (section === 'oauth') return 'suppliers';
  return section;
}

/** A tab's route journal, independent of browser history and other servers. */
export class DesktopTabHistory {
  entries: string[];
  index: number;

  constructor(hash: string, saved?: { entries?: unknown; index?: unknown } | null) {
    const valid = Array.isArray(saved?.entries) && saved.entries.length > 0 && saved.entries.length <= 100
      && saved.entries.every(value => typeof value === 'string' && (!value || value.startsWith('#')))
      && Number.isInteger(saved.index) && Number(saved.index) >= 0 && Number(saved.index) < saved.entries.length
      && saved.entries[Number(saved.index)] === hash;
    this.entries = valid ? saved!.entries as string[] : [hash];
    this.index = valid ? Number(saved!.index) : 0;
  }

  get canBack() { return this.index > 0; }
  get canForward() { return this.index + 1 < this.entries.length; }

  record(hash: string) {
    if (this.entries[this.index] === hash) return;
    this.entries = [...this.entries.slice(0, this.index + 1), hash].slice(-100);
    this.index = this.entries.length - 1;
  }

  move(direction: -1 | 1): string | undefined {
    const index = this.index + direction;
    if (index < 0 || index >= this.entries.length) return;
    this.index = index;
    return this.entries[index];
  }
}

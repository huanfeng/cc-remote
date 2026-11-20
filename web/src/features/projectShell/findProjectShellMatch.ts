import type { IBuffer } from '@xterm/xterm';

export type ShellSearchDirection = 'previous' | 'next';

interface SearchPosition {
  x: number;
  y: number;
}

interface SearchMatch extends SearchPosition {
  length: number;
}

export function findProjectShellMatch(
  buffer: IBuffer, cols: number, query: string, direction: ShellSearchDirection, anchor?: SearchPosition,
): SearchMatch | undefined {
  if (!query || !buffer.length) return;
  const expression = new RegExp(query.replace(/[.*+?^$(){}|[\]\\]/g, '\\$&'), 'gi');
  const previous = direction === 'previous';

  const groupStart = (row: number) => {
    while (row > 0 && buffer.getLine(row)?.isWrapped) row--;
    return row;
  };
  const groupEnd = (row: number) => {
    while (row + 1 < buffer.length && buffer.getLine(row + 1)?.isWrapped) row++;
    return row;
  };

  const searchGroup = (start: number, end: number, acrossAnchor: boolean): SearchMatch | undefined => {
    const rows: string[] = [];
    const offsets: number[] = [];
    let offset = 0;
    for (let row = start; row <= end; row++) {
      const line = buffer.getLine(row);
      if (!line) return;
      // A wide character wrapped to the next row leaves an empty cell behind.
      const wideWrap = row < end && buffer.getLine(row + 1)?.getCell(0)?.getWidth() === 2 && !line.getCell(cols - 1)?.getChars();
      const text = line.translateToString(row === end, 0, cols - (wideWrap ? 1 : 0));
      rows.push(text);
      offsets.push(offset);
      offset += text.length;
    }

    const text = rows.join('');
    const anchorOffset = anchor && anchor.y >= start && anchor.y <= end
      ? offsets[anchor.y - start] + (buffer.getLine(anchor.y)?.translateToString(false, 0, anchor.x).length ?? 0)
      : undefined;
    const lower = anchorOffset === undefined ? 0 : previous
      ? (acrossAnchor ? anchorOffset : 0)
      : (acrossAnchor ? 0 : anchorOffset + 1);
    const upper = anchorOffset === undefined ? Infinity : previous
      ? (acrossAnchor ? Infinity : anchorOffset)
      : (acrossAnchor ? anchorOffset + 1 : Infinity);

    expression.lastIndex = lower;
    let match: RegExpExecArray | null;
    let index = -1;
    while ((match = expression.exec(text)) && match.index < upper) {
      index = match.index;
      if (!previous) break;
      expression.lastIndex = index + 1;
    }
    if (index < 0) return;

    // Convert UTF-16 offsets into terminal columns only for the matching rows.
    const cell = buffer.getNullCell();
    const position = (character: number, exclusive: boolean): SearchPosition => {
      let row = start;
      while (row < end && offsets[row + 1 - start] <= character) row++;
      let remaining = character - offsets[row - start];
      const line = buffer.getLine(row);
      for (let x = 0; x < cols;) {
        const value = line?.getCell(x, cell);
        const width = value?.getWidth() || 1;
        const size = value?.getChars().length || 1;
        if (remaining < size) return { x: exclusive ? x + width : x, y: row };
        remaining -= size;
        x += width;
      }
      return { x: cols, y: row };
    };
    const first = position(index, false);
    const last = position(index + query.length - 1, true);
    return { ...first, length: (last.y - first.y) * cols + last.x - first.x };
  };

  const from = anchor?.y ?? (previous ? buffer.length - 1 : 0);
  const first = groupStart(from);
  const last = groupEnd(from);
  let match = searchGroup(first, last, false);
  if (match) return match;

  let row = previous ? first - 1 : last + 1;
  while (row !== (previous ? last : first)) {
    if (row < 0 || row >= buffer.length) row = previous ? buffer.length - 1 : 0;
    if (row === (previous ? last : first)) break;
    const start = previous ? groupStart(row) : row;
    const end = previous ? row : groupEnd(row);
    match = searchGroup(start, end, false);
    if (match) return match;
    row = previous ? start - 1 : end + 1;
  }

  if (anchor) return searchGroup(first, last, true);
}

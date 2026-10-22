import type { IBuffer, ILink, ILinkProvider, Terminal } from '@xterm/xterm';
import { splitPaths, type FileRef } from './filePaths';

// File paths on the terminal screen open in the file viewer, as they do in a
// chat: the screen holds cells, so each character is mapped back to its cell
// (a Chinese character takes two), and a path the terminal wrapped onto the
// next row is read across both.

type Cell = { x: number; y: number }; // 0-based column, 1-based row, as links take them

// Rows a wrapped line spans at most, for a provider that runs on every hover.
const MAX_ROWS = 16;

function lineText(buffer: IBuffer, first: number, last: number, cols: number) {
  let text = '';
  const cells: Cell[] = []; // the cell each UTF-16 unit of text starts in
  for (let row = first; row <= last; row++) {
    const line = buffer.getLine(row);
    if (!line) break;
    for (let x = 0; x < cols; x++) {
      const cell = line.getCell(x);
      if (!cell || cell.getWidth() === 0) continue; // the second half of a wide character
      let chars = cell.getChars();
      // A wide character that did not fit leaves the last cell of a wrapped row empty.
      if (!chars && x === cols - 1 && row < last) continue;
      chars ||= ' ';
      for (let i = 0; i < chars.length; i++) cells.push({ x, y: row + 1 });
      text += chars;
    }
  }
  return { text, cells };
}

/** Links the file paths in each line to open, unless open is absent (no session to read them from). */
export function filePathLinks(term: Terminal, open: () => ((ref: FileRef) => void) | undefined): ILinkProvider {
  return {
    provideLinks(y, callback) {
      const buffer = term.buffer.active;
      const opener = open();
      if (!opener || !buffer.getLine(y - 1)) { callback(undefined); return; }
      let first = y - 1, last = y - 1;
      while (first > 0 && y - first < MAX_ROWS && buffer.getLine(first)?.isWrapped) first--;
      while (last - y < MAX_ROWS && buffer.getLine(last + 1)?.isWrapped) last++;
      const { text, cells } = lineText(buffer, first, last, term.cols);
      const links: ILink[] = [];
      let index = 0;
      for (const part of splitPaths(text)) {
        if (typeof part === 'string') { index += part.length; continue; }
        const start = cells[index], end = cells[index + part.text.length - 1];
        index += part.text.length;
        if (start.y > y || end.y < y) continue; // on other rows of the line
        const width = buffer.getLine(end.y - 1)?.getCell(end.x)?.getWidth() || 1;
        links.push({ range: { start: { x: start.x + 1, y: start.y }, end: { x: end.x + width, y: end.y } }, text: part.text, activate: () => opener({ path: part.path, line: part.line }) });
      }
      callback(links.length ? links : undefined);
    },
  };
}

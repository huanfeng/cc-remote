export type SplitDirection = 'columns' | 'rows';

export interface PaneRect { x: number; y: number; width: number; height: number }

export interface SplitDivider {
  id: string;
  direction: SplitDirection;
  rect: PaneRect;
  ratio: number;
}

export const MIN_SPLIT_RATIO = 0.15;
export const MAX_SPLIT_RATIO = 0.85;

export function clampSplitRatio(ratio: number): number {
  return Number.isFinite(ratio) ? Math.max(MIN_SPLIT_RATIO, Math.min(MAX_SPLIT_RATIO, ratio)) : 0.5;
}

/** Compute positions without reparenting terminals when a split is added or closed. */
export function projectShellLayout(
  panes: { id: string; panePath?: string }[],
  legacyDirection: SplitDirection = 'columns',
  ratios: Readonly<Record<string, number>> = {},
) {
  const rects = new Map<string, PaneRect>();
  const dividers: SplitDivider[] = [];
  type Pane = (typeof panes)[number];

  const partition = (id: string, direction: SplitDirection, rect: PaneRect): [PaneRect, PaneRect] => {
    const ratio = clampSplitRatio(ratios[id] ?? 0.5);
    dividers.push({ id, direction, rect, ratio });
    if (direction === 'columns') {
      const width = rect.width * ratio;
      return [{ ...rect, width }, { ...rect, x: rect.x + width, width: rect.width - width }];
    }
    const height = rect.height * ratio;
    return [{ ...rect, height }, { ...rect, y: rect.y + height, height: rect.height - height }];
  };

  const placeLegacy = (group: Pane[], rect: PaneRect) => {
    const direction = legacyDirection;
    if (group.length === 2) {
      const [first, second] = group;
      const id = JSON.stringify(['legacy', direction, first.id, second.id]);
      const [before, after] = partition(id, direction, rect);
      rects.set(first.id, before);
      rects.set(second.id, after);
      return direction;
    }
    group.forEach(({ id }, index) => {
      const width = direction === 'columns' ? rect.width / group.length : rect.width;
      const height = direction === 'rows' ? rect.height / group.length : rect.height;
      rects.set(id, { x: rect.x + (direction === 'columns' ? width * index : 0), y: rect.y + (direction === 'rows' ? height * index : 0), width, height });
    });
    return group.length > 1 ? direction : undefined;
  };

  // The first leaf keeps its ID when it is split again, or when an ancestor
  // collapses and the server compacts the paths. Compare pane order by side.
  const firstLeaf = (group: Pane[]) => {
    const order = (pane: Pane) => (pane.panePath ?? '').replace(/[LU]/g, '0').replace(/[RD]/g, '1');
    return group.reduce((first, pane) => order(pane) < order(first) ? pane : first).id;
  };

  const place = (group: Pane[], rect: PaneRect, depth: number): SplitDirection | undefined => {
    if (group.length < 2) return placeLegacy(group, rect);
    const side = group[0].panePath?.[depth];
    const direction = side === 'L' || side === 'R' ? 'columns' : side === 'U' || side === 'D' ? 'rows' : undefined;
    if (!direction) return placeLegacy(group, rect);
    const [firstSide, secondSide] = direction === 'columns' ? ['L', 'R'] : ['U', 'D'];
    const first = group.filter(pane => pane.panePath?.[depth] === firstSide);
    const second = group.filter(pane => pane.panePath?.[depth] === secondSide);
    if (first.length + second.length !== group.length) return placeLegacy(group, rect);
    if (!first.length || !second.length) return place(group, rect, depth + 1);
    const id = JSON.stringify([firstLeaf(first), firstLeaf(second)]);
    const [before, after] = partition(id, direction, rect);
    place(first, before, depth + 1);
    place(second, after, depth + 1);
    return direction;
  };
  const direction = place(panes, { x: 0, y: 0, width: 100, height: 100 }, 0);
  return { rects, direction, dividers };
}

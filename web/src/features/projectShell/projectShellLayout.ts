export type SplitDirection = 'columns' | 'rows';

export interface PaneRect { x: number; y: number; width: number; height: number }

/** Compute positions without reparenting terminals when a split is added or closed. */
export function projectShellLayout(panes: { id: string; panePath?: string }[], legacyDirection: SplitDirection = 'columns') {
  const rects = new Map<string, PaneRect>();
  type Pane = (typeof panes)[number];
  const placeEvenly = (group: Pane[], rect: PaneRect, direction: SplitDirection) => {
    group.forEach(({ id }, index) => {
      const width = direction === 'columns' ? rect.width / group.length : rect.width;
      const height = direction === 'rows' ? rect.height / group.length : rect.height;
      rects.set(id, { x: rect.x + (direction === 'columns' ? width * index : 0), y: rect.y + (direction === 'rows' ? height * index : 0), width, height });
    });
    return group.length > 1 ? direction : undefined;
  };
  const place = (group: Pane[], rect: PaneRect, depth: number): SplitDirection | undefined => {
    if (group.length < 2) return placeEvenly(group, rect, legacyDirection);
    const side = group[0].panePath?.[depth];
    const direction = side === 'L' || side === 'R' ? 'columns' : side === 'U' || side === 'D' ? 'rows' : undefined;
    if (!direction) return placeEvenly(group, rect, legacyDirection);
    const [firstSide, secondSide] = direction === 'columns' ? ['L', 'R'] : ['U', 'D'];
    const first = group.filter(pane => pane.panePath?.[depth] === firstSide);
    const second = group.filter(pane => pane.panePath?.[depth] === secondSide);
    if (first.length + second.length !== group.length) return placeEvenly(group, rect, legacyDirection);
    if (!first.length || !second.length) return place(group, rect, depth + 1);
    const width = direction === 'columns' ? rect.width / 2 : rect.width;
    const height = direction === 'rows' ? rect.height / 2 : rect.height;
    place(first, { ...rect, width, height }, depth + 1);
    place(second, { x: rect.x + rect.width - width, y: rect.y + rect.height - height, width, height }, depth + 1);
    return direction;
  };
  const direction = place(panes, { x: 0, y: 0, width: 100, height: 100 }, 0);
  return { rects, direction };
}

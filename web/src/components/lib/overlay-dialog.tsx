import { createContext, useContext, useLayoutEffect, useRef, useState } from 'react';
import { overlayHistory } from '@/lib/overlayHistory';

export const OverlayDepth = createContext(0);

/** Register controlled and trigger-driven Radix roots, including nested dialogs. */
export function useOverlayDialog(props: { open?: boolean; defaultOpen?: boolean; onOpenChange?: (open: boolean) => void }) {
  const depth = useContext(OverlayDepth);
  const [localOpen, setLocalOpen] = useState(props.defaultOpen ?? false);
  const open = props.open ?? localOpen;
  const change = (next: boolean) => {
    if (props.open === undefined) setLocalOpen(next);
    props.onOpenChange?.(next);
  };
  const close = useRef(change);
  useLayoutEffect(() => { close.current = change; });
  useLayoutEffect(() => {
    if (open) return overlayHistory().add(() => close.current(false), depth);
  }, [open, depth]);
  return { open, onOpenChange: change, depth: depth + 1 };
}

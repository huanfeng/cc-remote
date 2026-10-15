import type { ReactNode } from 'react';

/** Stroke icons on a 16×16 grid; they inherit the text color and are hidden from assistive tech. */
function Svg({ size = 16, children }: { size?: number; children: ReactNode }) {
  return <svg className="svg-icon" viewBox="0 0 16 16" width={size} height={size} aria-hidden="true" focusable="false" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">{children}</svg>;
}
type P = { size?: number };

export const CloseIcon = ({ size }: P) => <Svg size={size}><path d="M4 4l8 8M12 4l-8 8" /></Svg>;
export const TrashIcon = ({ size }: P) => <Svg size={size}><path d="M2.5 4h11M6.5 4V2.5h3V4M4 4l.7 9.5h6.6L12 4M6.8 6.5v4.5M9.2 6.5v4.5" /></Svg>;
export const GearIcon = ({ size }: P) => <Svg size={size}><circle cx="8" cy="8" r="5.5" strokeWidth="2" strokeLinecap="butt" strokeDasharray="2.16 2.16" /><circle cx="8" cy="8" r="4.1" /><circle cx="8" cy="8" r="1.6" /></Svg>;
export const RefreshIcon = ({ size }: P) => <Svg size={size}><path d="M13 8a5 5 0 1 1-1.5-3.6M13 2.5v3h-3" /></Svg>;
export const MenuIcon = ({ size }: P) => <Svg size={size}><path d="M2.5 4h11M2.5 8h11M2.5 12h11" /></Svg>;
export const PlusIcon = ({ size }: P) => <Svg size={size}><path d="M8 3v10M3 8h10" /></Svg>;
export const TerminalIcon = ({ size }: P) => <Svg size={size}><rect x="1.75" y="2.75" width="12.5" height="10.5" rx="2" /><path d="M4.5 6.2 6.6 8l-2.1 1.8M8.3 10h3" /></Svg>;
export const LogoutIcon = ({ size }: P) => <Svg size={size}><path d="M6 2.5H3.5a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1H6M10.5 5l3 3-3 3M13.5 8H6" /></Svg>;
export const StatusIcon = ({ size }: P) => <Svg size={size}><path d="M1.5 8.5h3l1.7-4 3.2 7.5 1.8-3.5h3.3" /></Svg>;
export const ChevronDownIcon = ({ size }: P) => <Svg size={size}><path d="M4 6l4 4 4-4" /></Svg>;
export const CheckIcon = ({ size }: P) => <Svg size={size}><path d="M3.5 8.3 6.5 11.2 12.5 4.8" /></Svg>;

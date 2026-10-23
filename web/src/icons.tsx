import type { ReactNode } from 'react';

/** Stroke icons on a 16×16 grid; they inherit the text color and are hidden from assistive tech. */
function Svg({ size = 16, children }: { size?: number; children: ReactNode }) {
  return <svg className="svg-icon" viewBox="0 0 16 16" width={size} height={size} aria-hidden="true" focusable="false" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">{children}</svg>;
}
type P = { size?: number };

export const CloseIcon = ({ size }: P) => <Svg size={size}><path d="M4 4l8 8M12 4l-8 8" /></Svg>;
export const EditIcon = ({ size }: P) => <Svg size={size}><path d="M10.5 2.5l3 3L6 13H3v-3zM9 4l3 3" /></Svg>;
export const TrashIcon =({ size }: P) => <Svg size={size}><path d="M2.5 4h11M6.5 4V2.5h3V4M4 4l.7 9.5h6.6L12 4M6.8 6.5v4.5M9.2 6.5v4.5" /></Svg>;
export const GearIcon = ({ size }: P) => <Svg size={size}><circle cx="8" cy="8" r="5.5" strokeWidth="2" strokeLinecap="butt" strokeDasharray="2.16 2.16" /><circle cx="8" cy="8" r="4.1" /><circle cx="8" cy="8" r="1.6" /></Svg>;
export const RefreshIcon = ({ size }: P) => <Svg size={size}><path d="M13 8a5 5 0 1 1-1.5-3.6M13 2.5v3h-3" /></Svg>;
export const MenuIcon = ({ size }: P) => <Svg size={size}><path d="M2.5 4h11M2.5 8h11M2.5 12h11" /></Svg>;
export const PlusIcon = ({ size }: P) => <Svg size={size}><path d="M8 3v10M3 8h10" /></Svg>;
export const ChatIcon = ({ size }: P) => <Svg size={size}><path d="M2.5 4a1.5 1.5 0 0 1 1.5-1.5h8A1.5 1.5 0 0 1 13.5 4v5.5A1.5 1.5 0 0 1 12 11H7l-3 2.5V11a1.5 1.5 0 0 1-1.5-1.5z" /></Svg>;
export const TerminalIcon = ({ size }: P) => <Svg size={size}><rect x="1.75" y="2.75" width="12.5" height="10.5" rx="2" /><path d="M4.5 6.2 6.6 8l-2.1 1.8M8.3 10h3" /></Svg>;
export const LogoutIcon = ({ size }: P) => <Svg size={size}><path d="M6 2.5H3.5a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1H6M10.5 5l3 3-3 3M13.5 8H6" /></Svg>;
export const StatusIcon = ({ size }: P) => <Svg size={size}><path d="M1.5 8.5h3l1.7-4 3.2 7.5 1.8-3.5h3.3" /></Svg>;
export const ChevronDownIcon = ({ size }: P) => <Svg size={size}><path d="M4 6l4 4 4-4" /></Svg>;
export const CheckIcon = ({ size }: P) => <Svg size={size}><path d="M3.5 8.3 6.5 11.2 12.5 4.8" /></Svg>;
export const WrenchIcon = ({ size }: P) => <Svg size={size}><path d="M10.2 2.2a3.3 3.3 0 0 0-3.7 4.4L2.4 10.7a1.4 1.4 0 0 0 2 2l4.1-4.1a3.3 3.3 0 0 0 4.4-3.7l-2 2-1.8-.5-.5-1.8z" /></Svg>;
export const FileIcon = ({ size }: P) => <Svg size={size}><path d="M9 1.75H4.25a1 1 0 0 0-1 1v10.5a1 1 0 0 0 1 1h7.5a1 1 0 0 0 1-1V5.5zM9 1.75V5.5h3.75" /></Svg>;
export const SearchIcon = ({ size }: P) => <Svg size={size}><circle cx="7" cy="7" r="4.25" /><path d="m10.2 10.2 3.3 3.3" /></Svg>;
export const GlobeIcon = ({ size }: P) => <Svg size={size}><circle cx="8" cy="8" r="6" /><path d="M2 8h12M8 2c1.7 1.8 2.4 3.8 2.4 6S9.7 12.2 8 14C6.3 12.2 5.6 10.2 5.6 8S6.3 3.8 8 2z" /></Svg>;
export const AgentIcon = ({ size }: P) => <Svg size={size}><path d="M8 1.75v2.5M8 11.75v2.5M1.75 8h2.5M11.75 8h2.5M3.6 3.6l1.7 1.7M10.7 10.7l1.7 1.7M3.6 12.4l1.7-1.7M10.7 5.3l1.7-1.7" /></Svg>;
export const ListIcon = ({ size }: P) => <Svg size={size}><path d="M6 4h7.5M6 8h7.5M6 12h7.5M2.5 4h.5M2.5 8h.5M2.5 12h.5" /></Svg>;
export const QuestionIcon = ({ size }: P) => <Svg size={size}><circle cx="8" cy="8" r="6" /><path d="M6.3 6.2a1.8 1.8 0 1 1 2.4 1.7c-.5.2-.7.6-.7 1.1v.4M8 11.5h.01" /></Svg>;
/** Two stacked chevrons: expand / collapse every tool group. */
export const ExpandAllIcon = ({ size }: P) => <Svg size={size}><path d="M4.5 3.5 8 6.5l3.5-3M4.5 9.5 8 12.5l3.5-3" /></Svg>;
export const CollapseAllIcon = ({ size }: P) => <Svg size={size}><path d="M4.5 6.5 8 3.5l3.5 3M4.5 12.5 8 9.5l3.5 3" /></Svg>;
export const BackIcon = ({ size }: P) => <Svg size={size}><path d="M10 3.5L5.5 8l4.5 4.5" /></Svg>;
export const EyeIcon = ({ size }: P) => <Svg size={size}><path d="M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z" /><circle cx="8" cy="8" r="2" /></Svg>;
export const EyeOffIcon = ({ size }: P) => <Svg size={size}><path d="M6.6 3.65A6 6 0 0 1 8 3.5c4 0 6.5 4.5 6.5 4.5a11 11 0 0 1-1.9 2.4M10.1 11.9a6 6 0 0 1-2.1.6C4 12.5 1.5 8 1.5 8a11 11 0 0 1 2.6-3M6.6 6.6a2 2 0 0 0 2.8 2.8M2 2l12 12" /></Svg>;
export const ArrowDownIcon = ({ size }: P) => <Svg size={size}><path d="M8 2.5v10.5M3.5 8.5 8 13l4.5-4.5" /></Svg>;
export const DownloadIcon = ({ size }: P) => <Svg size={size}><path d="M8 2.5v8M4.5 7.5L8 11l3.5-3.5M3 13.5h10" /></Svg>;

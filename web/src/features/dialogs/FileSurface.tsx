// The 'file' surface: the files package's FileViewer, which owns its dialog
// and its back stack (opening another file while it is shown pushes onto it).
import { FileViewer } from '@/features/files/FileViewer';

export function FileSurface({ open, onOpenChange, sessionId, path, base }: {
  open: boolean; onOpenChange: (o: boolean) => void; mobile: boolean; sessionId: string; path: string; base?: string[];
}) {
  // FileViewer is always open while mounted; unmount at once on close instead of lingering.
  if (!open) return null;
  return <FileViewer sessionId={sessionId} path={path} base={base} onClose={() => onOpenChange(false)} />;
}

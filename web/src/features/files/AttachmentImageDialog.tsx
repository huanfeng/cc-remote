import { useEffect, useState, type ReactElement } from 'react';
import { PanelRightOpen, X } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import type { Attachment } from '@/backend/types';
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { Spinner } from '@/components/ui/spinner';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { ImageView } from './ImageView';
import { ViewerIconButton } from './ViewerIconButton';
import { fileDockStore } from './fileDockState';

export function AttachmentImageDialog({ attachment, dockKey, mobile, trigger }: {
  attachment: Attachment; dockKey: string; mobile: boolean; trigger: ReactElement;
}) {
  const backend = useBackend();
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent showCloseButton={false} aria-describedby={undefined}
        className="flex h-[85dvh] w-[calc(100%-1rem)] max-w-5xl flex-col gap-0 overflow-hidden rounded-xl p-0 sm:w-[calc(100%-2rem)]">
        <header className="flex h-14 shrink-0 items-center gap-1 border-b border-border/60 pr-2 pl-4">
          <DialogTitle className="min-w-0 flex-1 truncate" title={attachment.name}>{attachment.name}</DialogTitle>
          <ViewerIconButton label={t('Move to the file panel', '移到文件栏')} className={cn(mobile && 'size-9')} onClick={() => {
            fileDockStore(backend).getState().open(dockKey, { kind: 'attachment', attachment });
            setOpen(false);
          }}><PanelRightOpen /></ViewerIconButton>
          <ViewerIconButton label={t('Close', '关闭')} className={cn(mobile && 'size-9')} onClick={() => setOpen(false)}><X /></ViewerIconButton>
        </header>
        <div className="min-h-0 flex-1">
          <AttachmentImage attachment={attachment} />
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function AttachmentImage({ attachment }: { attachment: Attachment }) {
  const backend = useBackend();
  const url = attachment.url;
  const [resolved, setResolved] = useState<{ url: string; src?: string }>();
  // The backend shares the thumbnail request with this view and the dock.
  useEffect(() => {
    if (!url) return;
    let live = true;
    backend.assetUrl(url).then(src => { if (live) setResolved({ url, src }); }, () => { if (live) setResolved({ url }); });
    return () => { live = false; };
  }, [backend, url]);
  const current = resolved?.url === url ? resolved : undefined;
  const src = url && (url.startsWith('/') ? current?.src : url);
  if (src) return <ImageView src={src} alt={attachment.name} />;
  return (
    <div className="flex h-full items-center justify-center gap-2 text-ui-sm text-foreground-subtle">
      {!url || current
        ? t('Cannot open the image', '无法打开图片')
        : <><Spinner className="size-3.5" />{t('Loading…', '加载中…')}</>}
    </div>
  );
}

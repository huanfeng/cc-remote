// Derived from ZCode (Apache-2.0) components/ui/kbd.tsx; modified by cc-remote
// to render the keycap directly instead of through ControlShortcutBadge.
import { cn } from "@/components/lib/utils.js";

function Kbd({ className, ...props }: React.ComponentProps<"kbd">) {
  return (
    <kbd
      data-slot="kbd"
      className={cn(
        "pointer-events-none inline-flex h-5 min-w-5 select-none items-center justify-center gap-1 rounded-[5px] border border-border px-1 font-sans text-ui-xs text-foreground-subtle",
        className,
      )}
      {...props}
    />
  );
}

function KbdGroup({ className, ...props }: React.ComponentProps<"div">) {
  return <div data-slot="kbd-group" className={cn("inline-flex items-center gap-1", className)} {...props} />;
}

export { Kbd, KbdGroup };

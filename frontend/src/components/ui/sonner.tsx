import { useTheme } from "@/theme/ThemeProvider";
import { Toaster as Sonner, type ToasterProps } from "sonner";
import { CircleCheck, Info, AlertTriangle, OctagonX, Loader2, X } from "lucide-react";
import { cn } from "@/lib/utils";

export type ToastTone = "info" | "success" | "warning" | "error" | "loading";

/**
 * Shell / Toast (Figma 727:1682 — 5 tones × action boolean)
 *
 * - Box: 380 wide, hugs height, rounded-14, pl-4 pr-3 py-3, gap-3, items-start
 * - Material: --glass-float-bg (62%) + backdrop-blur-float + --glass-border + shadow-[0_12px_16px_rgba(0,0,0,0.45)]
 *   EXCEPTION: Toast has NO inset top highlight.
 *   EXCEPTION: Body text is --text-tertiary, not --text-secondary.
 *   EXCEPTION: Gap between title and body is 0 (--space-0).
 * - Tone glyph: 20px — the ONLY coloured element:
 *   - info: --status-info (#b2d5e5)
 *   - success: --status-success (#99e1d9)
 *   - warning: --status-warning (#f1fec8)
 *   - error: --status-danger (#d87886)
 *   - loading: --text-tertiary
 * - Close glyph: 16px
 * - Title: 13 Medium --text-primary (text-body-13-medium)
 * - Body: 12 Regular --text-tertiary (text-body-12)
 * - Action chip: off by default; 28 tall, --surface-raised + --border-subtle, r8, px-2, label 12 Medium (text-label-12)
 */

function ToneGlyph({ tone }: { tone: ToastTone }) {
  switch (tone) {
    case "info":
      return <Info className="size-5 shrink-0 text-status-info" />;
    case "success":
      return <CircleCheck className="size-5 shrink-0 text-status-success" />;
    case "warning":
      return <AlertTriangle className="size-5 shrink-0 text-status-warning" />;
    case "error":
      return <OctagonX className="size-5 shrink-0 text-status-danger" />;
    case "loading":
      return <Loader2 className="size-5 shrink-0 animate-spin text-text-tertiary" />;
  }
}

export type ToastSpecCardProps = {
  tone?: ToastTone;
  title: string;
  body?: string;
  actionLabel?: string;
  onAction?: () => void;
  onClose?: () => void;
  className?: string;
  id?: string;
};

export function ToastSpecCard({
  tone = "info",
  title,
  body,
  actionLabel,
  onAction,
  onClose,
  className,
  id,
}: ToastSpecCardProps) {
  return (
    <div
      id={id}
      data-slot="toast"
      data-tone={tone}
      className={cn(
        "relative flex w-95 items-start gap-3 rounded-float bg-glass-float p-3 pl-4 backdrop-blur-float border border-glass-border shadow-toast select-none",
        className
      )}
    >
      <ToneGlyph tone={tone} />
      <div className="flex min-w-0 flex-1 flex-col gap-0">
        <span className="text-body-13-medium text-text-primary">{title}</span>
        {body ? <span className="text-body-12 text-text-tertiary mt-0">{body}</span> : null}
      </div>
      {actionLabel ? (
        <button
          type="button"
          onClick={onAction}
          className="h-7 shrink-0 rounded-md border border-border-subtle bg-surface-raised px-2 text-label-12 text-text-primary hover:bg-surface-raised/80 cursor-pointer"
        >
          {actionLabel}
        </button>
      ) : null}
      {onClose ? (
        <button
          type="button"
          onClick={onClose}
          aria-label="Close notification"
          className="size-4 shrink-0 text-text-tertiary hover:text-text-primary cursor-pointer flex items-center justify-center p-0"
        >
          <X className="size-4" />
        </button>
      ) : null}
    </div>
  );
}

/**
 * How long a toast stays. Not a --motion-* token: those are how long something
 * takes to move (120/180/240 ms), and this is how long it waits to be read.
 * One number here so the five call sites cannot each pick their own.
 */
const TOAST_DURATION_MS = 4000;

const Toaster = ({ ...props }: ToasterProps) => {
  const { resolvedTheme } = useTheme();

  return (
    <Sonner
      theme={resolvedTheme}
      position="bottom-right"
      duration={TOAST_DURATION_MS}
      offset="var(--space-section)"
      mobileOffset="var(--space-group)"
      className="toaster group"
      icons={{
        success: <CircleCheck className="size-5 shrink-0 text-status-success" />,
        info: <Info className="size-5 shrink-0 text-status-info" />,
        warning: <AlertTriangle className="size-5 shrink-0 text-status-warning" />,
        error: <OctagonX className="size-5 shrink-0 text-status-danger" />,
        loading: <Loader2 className="size-5 shrink-0 animate-spin text-text-tertiary" />,
      }}
      toastOptions={{
        // Keep Sonner's stacking, swipe and lifetime; the app owns appearance.
        unstyled: true,
        classNames: {
          toast:
            "app-toast flex w-95 rounded-float bg-glass-float backdrop-blur-float border border-glass-border shadow-toast pl-4 pr-3 py-3 gap-3 items-start text-text-primary",
          content: "flex min-w-0 flex-1 flex-col gap-0",
          icon: "relative flex size-5 shrink-0 items-center justify-center text-text-tertiary",
          title: "text-body-13-medium text-text-primary",
          description: "text-body-12 text-text-tertiary mt-0",
          actionButton:
            "h-7 shrink-0 rounded-md bg-surface-raised border border-border-subtle px-2 text-label-12 text-text-primary cursor-pointer hover:bg-state-soft focus-visible:outline-status-info",
          cancelButton:
            "h-7 shrink-0 rounded-md bg-surface-raised border border-border-subtle px-2 text-label-12 text-text-secondary cursor-pointer hover:bg-state-soft focus-visible:outline-status-info",
          closeButton: "order-last flex size-4 shrink-0 items-center justify-center p-0 text-text-tertiary hover:text-text-primary cursor-pointer focus-visible:outline-status-info",
        },
      }}
      {...props}
    />
  );
};

export { Toaster };

import type { ReactNode } from "react";
import { Info, CircleCheck, AlertTriangle, OctagonX, LoaderCircle } from "lucide-react";
import { Alert, AlertContent, AlertTitle, AlertDescription } from "./alert";
import { cn } from "@/lib/utils";

type Tone = "info" | "success" | "warning" | "error";
const icons = { info: Info, success: CircleCheck, warning: AlertTriangle, error: OctagonX };

/** Inline results belong to the control/form that produced them. */
export function InlineFeedback({ tone = "error", children, id, className }: {
  tone?: Tone; children: ReactNode; id?: string; className?: string;
}) {
  return <p id={id} role={tone === "error" ? "alert" : "status"}
    className={cn("text-body-12 break-words", tone === "error" ? "text-status-danger" : tone === "success" ? "text-status-success" : "text-text-secondary", className)}>{children}</p>;
}

/** Persistent condition in a region; never auto-dismissed. */
export function FeedbackBanner({ tone = "info", title, children, action, className }: {
  tone?: Tone; title: string; children?: ReactNode; action?: ReactNode; className?: string;
}) {
  const Icon = icons[tone];
  return <Alert variant={tone} role={tone === "error" ? "alert" : "status"}
    className={cn("h-auto min-h-0 flex-wrap", className)}>
    <Icon aria-hidden="true" />
    <AlertContent><AlertTitle>{title}</AlertTitle>{children && <AlertDescription className="break-words">{children}</AlertDescription>}</AlertContent>
    {action}
  </Alert>;
}

export function PendingStatus({ children, className }: { children: ReactNode; className?: string }) {
  return <div role="status" className={cn("flex items-center gap-2 text-body-12 text-text-secondary", className)}>
    <LoaderCircle aria-hidden="true" className="size-4 shrink-0 animate-spin motion-reduce:animate-none" />{children}
  </div>;
}

import type { ReactNode } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { AnimatedNumber } from "@/components/ui/animated-number";

/** Context panels share one title, summary and explicit dismiss action. */
export function TrayPanelHeading({
  title,
  count,
  detail,
  trailing,
  onClose,
  closeTestId,
}: {
  title: string;
  count?: number;
  detail?: ReactNode;
  /** Marks that belong to the whole panel, between the title and the close button. */
  trailing?: ReactNode;
  onClose?: () => void;
  closeTestId?: string;
}) {
  return (
    <header title={typeof detail === "string" ? detail : undefined} className="flex h-9 shrink-0 items-center gap-2 border-b border-border-subtle px-3">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <h3 className="text-body-13-medium text-text-primary">{title}</h3>
          {count !== undefined ? (
            <AnimatedNumber
              value={count}
              className="rounded-md bg-state-soft px-1.5 text-caption-11 text-text-secondary"
            />
          ) : null}
        </div>
        {detail ? (
          <p className="sr-only">{detail}</p>
        ) : null}
      </div>
      {trailing}
      {onClose ? (
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label={`Close ${title.toLowerCase()}`}
          data-testid={closeTestId}
          onClick={onClose}
        >
          <X />
        </Button>
      ) : null}
    </header>
  );
}

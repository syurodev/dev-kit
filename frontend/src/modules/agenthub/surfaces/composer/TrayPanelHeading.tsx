import type { ReactNode } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { AnimatedNumber } from "@/components/ui/animated-number";

/** Context panels share one title, summary and explicit dismiss action. */
export function TrayPanelHeading({
  title,
  count,
  detail,
  onClose,
}: {
  title: string;
  count?: number;
  detail?: ReactNode;
  onClose?: () => void;
}) {
  return (
    <header className="flex shrink-0 items-start gap-3 border-b border-border-subtle px-3 py-3">
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
          <p className="mt-1 text-caption-11 text-text-tertiary">{detail}</p>
        ) : null}
      </div>
      {onClose ? (
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label={`Close ${title.toLowerCase()}`}
          onClick={onClose}
        >
          <X />
        </Button>
      ) : null}
    </header>
  );
}

import * as React from "react";
import { Image as ImageIcon } from "lucide-react";

import { cn } from "@/lib/utils";

export type AgentImageProps = {
  src?: string;
  name?: string;
  /** "1440 × 900" — Figma's own multiplication sign, not an x. */
  dimensions?: string;
  className?: string;
};

/**
 * Agent / Image — Figma `1061:3228`.
 *
 * The same two-ground card the code block uses: surface/raised with a hairline,
 * and the picture itself on surface/base. The dark well matters here — a
 * screenshot with a white background needs something to end against, and an
 * image floating directly on the bubble has no edge.
 *
 * The canvas keeps its height whether or not the picture loads, so a transcript
 * does not jump as attachments resolve, and the placeholder is the same icon
 * the empty state uses rather than a broken-image glyph.
 */
export function AgentImage({ src, name = "image.png", dimensions, className }: AgentImageProps) {
  const [failed, setFailed] = React.useState(false);

  return (
    <div
      data-testid="agent-image"
      className={cn(
        "flex w-full max-w-100 flex-col overflow-hidden rounded-control",
        "border border-border-subtle bg-surface-raised",
        className,
      )}
    >
      <div className="flex h-56.25 w-full shrink-0 items-center justify-center bg-surface-base select-none">
        {src && !failed ? (
          <img
            src={src}
            alt={name}
            onError={() => setFailed(true)}
            className="size-full object-contain"
          />
        ) : (
          <ImageIcon className="size-6 text-icon-secondary" aria-hidden="true" />
        )}
      </div>

      <div className="flex w-full shrink-0 items-center gap-2 px-3 py-2">
        <span className="min-w-0 flex-1 truncate text-code-11 text-text-secondary">
          {name}
        </span>
        {dimensions ? (
          <span className="shrink-0 text-caption-11 whitespace-nowrap text-text-tertiary">
            {dimensions}
          </span>
        ) : null}
      </div>
    </div>
  );
}

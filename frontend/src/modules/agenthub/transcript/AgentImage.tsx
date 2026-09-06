import * as React from "react";
import { ImageIcon } from "lucide-react";
import { cn } from "@/lib/utils";

export type AgentImageProps = {
  /**
   * Image content block.
   * Figma node: 1061:3228 (Agent / Image)
   */
  src?: string;
  name?: string;
  mimeType?: string;
  dimensions?: string;
  size?: string;
  className?: string;
};

export function AgentImage({
  src,
  name = "image.png",
  mimeType,
  dimensions,
  size,
  className,
}: AgentImageProps) {
  const [hasError, setHasError] = React.useState(false);

  const showImage = Boolean(src && !hasError);

  const metaParts: string[] = [];
  if (dimensions) metaParts.push(dimensions);
  if (mimeType) metaParts.push(mimeType.replace("image/", "").toUpperCase());
  if (size) metaParts.push(size);
  const metaLabel = metaParts.join(" · ");

  return (
    <div
      className={cn(
        "flex flex-col w-full max-w-[400px] rounded-md border border-border-subtle bg-surface-base overflow-hidden",
        className,
      )}
      data-testid="agent-image"
    >
      <div className="relative aspect-video w-full overflow-hidden bg-surface-raised/40 flex items-center justify-center select-none">
        {showImage ? (
          <img
            src={src}
            alt={name}
            onError={() => setHasError(true)}
            className="size-full object-cover"
          />
        ) : (
          <div className="flex flex-col items-center gap-1 text-text-tertiary">
            <ImageIcon className="size-(--icon-24)" />
            <span className="text-caption-11">Image</span>
          </div>
        )}
      </div>

      <div className="flex h-(--control-height-sm) shrink-0 items-center justify-between gap-2 border-t border-border-subtle bg-surface-raised/50 px-2.5">
        <span className="truncate text-code-12 text-text-secondary">{name}</span>
        {metaLabel && (
          <span className="shrink-0 text-caption-11 text-text-tertiary">{metaLabel}</span>
        )}
      </div>
    </div>
  );
}

import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import { FileIcon } from "./FileIcon";

/** Name + location: compact chips truncate; prose tables can opt into wrapping. */
export function FileLabel({ path, label = path, suffix, wrap = false }: { path: string; label?: ReactNode; suffix?: string; wrap?: boolean }) {
  return <>
    <FileIcon path={path} className="size-3.5" />
    <span data-file-label className={cn("min-w-0 text-code-12", wrap ? "text-left whitespace-normal wrap-anywhere" : "inline-flex items-baseline")}>
      <span className={wrap ? undefined : "min-w-0 truncate"}>{label}</span>
      {suffix ? <span className="shrink-0 text-text-tertiary">{suffix}</span> : null}
    </span>
  </>;
}

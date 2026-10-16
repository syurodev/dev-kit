import type { ReactNode } from "react";
import { FileIcon } from "./FileIcon";

/** One text baseline for name + location; only the name truncates in narrow chips. */
export function FileLabel({ path, label = path, suffix }: { path: string; label?: ReactNode; suffix?: string }) {
  return <>
    <FileIcon path={path} className="size-3.5" />
    <span data-file-label className="inline-flex min-w-0 items-baseline text-code-12">
      <span className="min-w-0 truncate">{label}</span>
      {suffix ? <span className="shrink-0 text-text-tertiary">{suffix}</span> : null}
    </span>
  </>;
}

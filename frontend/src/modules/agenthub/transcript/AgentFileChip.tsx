import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FileLabel } from "@/components/file-icon/FileLabel";
import { cn } from "@/lib/utils";

export type AgentFileChipProps = {
  path: string;
  line?: number;
  kind?: "file" | "image" | "document";
  detail?: string;
  onClick?: () => void;
  className?: string;
};

export function AgentFileChip({ path, line, kind = "file", detail, onClick, className }: AgentFileChipProps) {
  const content = <>
    <FileLabel
      path={path || (kind === "document" ? "document.pdf" : kind === "image" ? "image.png" : "")}
      label={path}
      suffix={typeof line === "number" ? `:${line}` : undefined}
    />
    {detail ? <span className="shrink-0 text-code-12 text-text-tertiary">{detail}</span> : null}
  </>;
  const props = {
    "data-testid": "agent-file-chip",
    title: `${path}${line === undefined ? "" : `:${line}`}`,
    className: cn("h-6 max-w-full min-w-0 items-center justify-start gap-1.5 rounded-md px-2 py-0 align-middle text-text-secondary", className),
  };
  return onClick
    ? <Button {...props} type="button" variant="secondary" size="xs" onClick={onClick}>{content}</Button>
    : <Badge {...props} variant="secondary">{content}</Badge>;
}

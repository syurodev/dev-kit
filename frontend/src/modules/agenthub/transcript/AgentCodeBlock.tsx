import * as React from "react";
import { Check, Copy } from "lucide-react";

import { cn } from "@/lib/utils";

export type AgentCodeBlockProps = {
  code: string;
  path?: string;
  language?: string;
  className?: string;
};

/**
 * Agent / Code Block — Figma `1023:2660`.
 *
 * Two grounds stacked: the card is surface/raised with a border/subtle
 * hairline, and the code itself sits on surface/base inside it. That inner
 * darker well is the whole idea — it is the same relationship the terminal and
 * the diff use, so all three read as "machine text" without a border each.
 *
 * The head is language, then path, then Copy. There is no collapse chevron:
 * Figma draws none, and nothing in the transcript wired one — a toggle with no
 * caller is a toggle nobody can reach.
 */
export function AgentCodeBlock({
  code,
  path,
  language,
  className,
}: AgentCodeBlockProps) {
  const [copied, setCopied] = React.useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard is unavailable in some webviews; the code is still selectable.
    }
  };

  return (
    <div
      data-testid="agent-code-block"
      className={cn(
        "flex w-full max-w-full flex-col overflow-hidden rounded-control",
        "border border-border-subtle bg-surface-raised",
        className,
      )}
    >
      <div className="flex w-full shrink-0 items-center gap-2 px-3 py-2">
        <span className="shrink-0 text-code-11 whitespace-nowrap text-text-tertiary">
          {language ?? "text"}
        </span>
        <span className="min-w-0 flex-1 truncate text-code-11 text-text-secondary">
          {path ?? ""}
        </span>
        <button
          type="button"
          onClick={handleCopy}
          data-testid="code-block-copy"
          className="flex shrink-0 items-center gap-1 text-caption-11-medium text-text-tertiary hover:text-text-primary cursor-pointer"
        >
          {copied ? (
            <Check className="size-3 text-status-success" aria-hidden="true" />
          ) : (
            <Copy className="size-3" aria-hidden="true" />
          )}
          <span>{copied ? "Copied" : "Copy"}</span>
        </button>
      </div>

      <pre className="w-full overflow-x-auto bg-surface-base p-3 text-code-12 text-text-primary select-text">
        <code>{code}</code>
      </pre>
    </div>
  );
}

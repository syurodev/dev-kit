import * as React from "react";
import { Check, Code2, Copy, FileCode, Terminal } from "lucide-react";

import { cn } from "@/lib/utils";
import { FramedCard, FramedCardAction, FramedCardHeader, FramedCardPreview } from "@/components/ui/framed-card";
import { renderHighlighted, useHighlight } from "@/components/code/CodeBlock";

export type AgentCodeBlockProps = {
  /** The header's name when there is no file: "Output" for what a tool printed. Defaults to the language. */
  label?: string;
  code: string;
  path?: string;
  language?: string;
  /**
   * Wraps long lines and caps the height, scrolling inside. For tool output: a
   * one-line JSON document from an MCP tool otherwise showed as a single cut-off
   * line with a scrollbar on each axis. Code an agent writes keeps its lines.
   */
  wrap?: boolean;
  /**
   * The first line's number, for a listing (a file a tool read). Draws the
   * numbers in their own gutter, out of the selection and out of Copy, instead
   * of leaving them inside the code.
   */
  startLine?: number;
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
 * The head is language, then path, then Copy. No collapse chevron: Figma draws
 * none and nothing in the transcript would wire one.
 */
const SHELLS = new Set(["bash", "sh", "shell", "zsh", "console"]);

/**
 * A block of code — a fence in an answer, a command a tool ran, a file it
 * read — as a Framed card (docs/dev-kit/framed-card.mdx): the header names it
 * (the file when there is one, else the language) and carries Copy; the code
 * is the preview. One shape for every block of code in a conversation.
 */
export function AgentCodeBlock({
  code,
  path,
  language,
  wrap = false,
  startLine,
  label,
  className,
}: AgentCodeBlockProps) {
  const [copied, setCopied] = React.useState(false);
  const highlight = useHighlight(code, language);
  const body = renderHighlighted(code, highlight);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard is unavailable in some webviews; the code is still selectable.
    }
  };

  const lineNumbers =
    startLine === undefined
      ? ""
      : Array.from({ length: code.split("\n").length }, (_, index) => startLine + index).join("\n");
  const lang = language ?? "text";
  const name = path ? path.split("/").pop() || path : "";
  const lineCount = code.split("\n").length;
  const range = startLine !== undefined ? `lines ${startLine}–${startLine + lineCount - 1}` : undefined;
  const Icon = path ? FileCode : SHELLS.has(lang) ? Terminal : Code2;

  return (
    <FramedCard data-testid="agent-code-block" className={cn("max-w-full", className)}>
      <FramedCardHeader
        icon={Icon}
        title={
          path ? (
            <span className="text-code-11" title={path}>
              {name}
            </span>
          ) : label ? (
            label
          ) : (
            <span className="text-code-11">{lang}</span>
          )
        }
        meta={path ? (range ?? lang) : label && lang !== "text" ? lang : range}
        action={
          <FramedCardAction opens={false} onClick={handleCopy} data-testid="code-block-copy">
            {copied ? <Check className="text-status-success" aria-hidden="true" /> : <Copy aria-hidden="true" />}
            {copied ? "Copied" : "Copy"}
          </FramedCardAction>
        }
      />
      <FramedCardPreview flush className="py-0">
        {startLine !== undefined ? (
          // One scroller for both columns, so the numbers stay beside their
          // lines on the vertical axis; the gutter is sticky so it stays on the
          // horizontal one. Lines keep their breaks: a listing is read as code.
          <div data-testid="code-block-listing" className="flex max-h-80 w-full overflow-auto text-code-12">
            <pre
              aria-hidden="true"
              data-testid="code-block-gutter"
              className="sticky left-0 shrink-0 bg-surface-base py-(--space-inline) pr-(--space-control) pl-(--space-inline) text-right text-text-tertiary tabular-nums select-none"
            >
              {lineNumbers}
            </pre>
            <pre className="grow py-(--space-inline) pr-(--space-inline) text-text-primary select-text">
              <code>{body}</code>
            </pre>
          </div>
        ) : (
          <pre
            className={cn(
              "w-full p-(--space-inline) text-code-12 text-text-primary select-text",
              wrap ? "max-h-80 overflow-y-auto whitespace-pre-wrap wrap-anywhere" : "overflow-x-auto",
            )}
          >
            <code>{body}</code>
          </pre>
        )}
      </FramedCardPreview>
    </FramedCard>
  );
}

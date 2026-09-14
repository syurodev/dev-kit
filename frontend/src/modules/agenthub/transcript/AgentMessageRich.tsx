import * as React from "react";

import { cn } from "@/lib/utils";
import { AgentCodeBlock } from "./AgentCodeBlock";
import { parseInline, parseMarkdown, type MarkdownBlock } from "./agentMarkdown";

export type AgentMessageRichProps = {
  /** Markdown as the agent sent it. */
  text: string;
  author?: string;
  className?: string;
};

/**
 * Agent / Message Rich — Figma `1060:3171`.
 *
 * The wide variant of Agent / Message: no avatar, full width, structured prose
 * instead of one paragraph. This is the shape an agent's answer actually takes —
 * a heading, some lists, a table of before/after values — which the plain bubble
 * can only print as markdown source.
 *
 * Prose uses Body/16; dense tables keep Body/12. List markers use the tertiary tier
 * so the text column stays clean, inline code on surface/base (the same well
 * the code block and terminal use, one step down from the bubble), links in
 * status/info, and the blockquote marked by a 2px border/default rule rather
 * than by an indent alone.
 */
function Inline({ text }: { text: string }) {
  return (
    <>
      {parseInline(text).map((token, idx) => {
        switch (token.type) {
          case "code":
            return (
              <code
                key={idx}
                className="rounded-sm bg-surface-base px-1 text-code-12 text-text-primary"
              >
                {token.value}
              </code>
            );
          case "link":
            return (
              <a
                key={idx}
                href={token.href}
                target="_blank"
                rel="noreferrer"
                className="text-status-info underline"
              >
                {token.value}
              </a>
            );
          case "strong":
            return (
              <strong key={idx} className="font-medium text-text-primary">
                {token.value}
              </strong>
            );
          case "em":
            return (
              <em key={idx} className="italic">
                {token.value}
              </em>
            );
          default:
            return <React.Fragment key={idx}>{token.value}</React.Fragment>;
        }
      })}
    </>
  );
}

function Block({ block }: { block: MarkdownBlock }) {
  switch (block.type) {
    case "heading":
      return (
        <p
          className={cn(
            "w-full text-text-primary",
            // Keep headings at least as large as prose; weight marks deeper levels.
            block.level <= 2 ? "text-heading-24" : "text-title-16",
          )}
        >
          <Inline text={block.text} />
        </p>
      );

    case "list":
      return (
        <div className="flex w-full flex-col gap-1 text-body-16 text-text-primary">
          {block.items.map((item, idx) => (
            <div key={idx} className="flex w-full items-start gap-2">
              <span
                aria-hidden="true"
                className="shrink-0 whitespace-nowrap text-text-tertiary"
              >
                {block.ordered ? `${idx + 1}.` : "•"}
              </span>
              <span className="min-w-0 flex-1">
                <Inline text={item} />
              </span>
            </div>
          ))}
        </div>
      );

    case "quote":
      return (
        <div className="flex w-full items-stretch gap-3">
          <span
            aria-hidden="true"
            className="w-0.5 shrink-0 self-stretch bg-border-default"
          />
          <p className="min-w-0 flex-1 text-body-16 text-text-secondary">
            <Inline text={block.text} />
          </p>
        </div>
      );

    case "code":
      return <AgentCodeBlock code={block.code} language={block.language} />;

    case "table":
      return (
        <div className="w-full overflow-x-auto rounded-sm border border-border-subtle">
          <table className="w-full border-collapse">
            <thead>
              <tr className="bg-surface-base">
                {block.head.map((cell, idx) => (
                  <th
                    key={idx}
                    className="px-3 py-2 text-left text-label-12 text-text-tertiary"
                  >
                    {cell}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, rowIdx) => (
                <tr key={rowIdx}>
                  {row.map((cell, idx) => (
                    <td
                      key={idx}
                      className="px-3 py-2 text-left text-body-12 text-text-primary"
                    >
                      <Inline text={cell} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );

    default:
      return (
        <p className="w-full text-body-16 break-words text-text-primary">
          <Inline text={block.text} />
        </p>
      );
  }
}

export function AgentMessageRich({ text, author, className }: AgentMessageRichProps) {
  return (
    <div
      data-testid="agent-message-rich"
      className={cn("flex w-full max-w-full flex-col items-start gap-1", className)}
    >
      {author ? (
        <p className="text-caption-11 whitespace-nowrap text-text-tertiary">{author}</p>
      ) : null}
      <AgentProse text={text} className="rounded-lg bg-surface-raised p-4" />
    </div>
  );
}

export type AgentProseProps = {
  text: string;
  /**
   * Wraps each block so the caller can animate it in. A render prop rather than
   * an animation flag, so this file stays about markdown and knows nothing of
   * motion or of which turn is live.
   */
  wrapBlock?: (block: React.ReactNode, index: number) => React.ReactNode;
  className?: string;
};

/**
 * The agent's words on the page's own ground: no bubble, no author line.
 *
 * A bubble marks a voice in a conversation between people. In a transcript with
 * one person and one agent the side of the column already says who is talking,
 * and the agent's answer is a document — headings, lists, code — that a rounded
 * card squeezes into looking like a chat line.
 */
export function AgentProse({ text, wrapBlock, className }: AgentProseProps) {
  const blocks = React.useMemo(() => parseMarkdown(text), [text]);
  return (
    <div
      data-testid="agent-prose"
      className={cn("flex w-full flex-col gap-4 select-text", className)}
    >
      {blocks.map((block, idx) => {
        const node = <Block block={block} />;
        return <React.Fragment key={idx}>{wrapBlock ? wrapBlock(node, idx) : node}</React.Fragment>;
      })}
    </div>
  );
}

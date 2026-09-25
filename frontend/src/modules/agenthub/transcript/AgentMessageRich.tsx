import * as React from "react";
import { Eye, EyeOff, Square, SquareCheck } from "lucide-react";

import { cn } from "@/lib/utils";
import { AgentCodeBlock } from "./AgentCodeBlock";
import {
  parseInline,
  parseMarkdownIncremental,
  type IncrementalMarkdownState,
  type InlineToken,
  type ListItem,
  type MarkdownBlock,
} from "./agentMarkdown";

/** A masked token reads out this many dots at most — enough to fill the
 *  several lines a real JWT does, without an adversarial one turning into an
 *  unbounded wall of characters. */
const MASK_VISIBLE_CAP = 240;

/**
 * A JWT, hidden by default and shown on click — the same shape Claude
 * Desktop's own masking takes. Nothing is redacted from the transcript
 * itself: this is a render-time toggle, so Copy on the message still copies
 * the real value, and a reload starts masked again rather than remembering
 * that this one reader chose to look.
 */
function MaskedSecret({ value }: { value: string }) {
  const [revealed, setRevealed] = React.useState(false);
  const Icon = revealed ? EyeOff : Eye;
  return (
    <button
      type="button"
      onClick={() => setRevealed((current) => !current)}
      data-testid="masked-secret"
      aria-pressed={revealed}
      title={revealed ? "Click to hide" : "Click to reveal"}
      className="inline-flex max-w-full cursor-pointer items-baseline gap-1 rounded-md bg-muted px-(--space-inline-code-x) py-(--space-inline-code-y) text-left align-baseline font-mono text-code-12 whitespace-pre-wrap wrap-anywhere text-text-primary"
    >
      <Icon aria-hidden="true" className="size-3 shrink-0 translate-y-0.5 text-text-tertiary" />
      {revealed ? value : "•".repeat(Math.min(value.length, MASK_VISIBLE_CAP))}
    </button>
  );
}

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
 * One reading size for everything an answer contains — paragraphs, lists,
 * quotes, table cells — at the app's own body, 14/20; headings step up by
 * weight, not by a jump in size. It used to be Body/16 prose, Heading/24 for
 * `##` and Body/12 in tables, so a short reply with a heading and a table
 * printed at three sizes and read as uneven, and 16px sat a size above
 * everything else in the window. Paragraphs, lists and quotes run at the prose
 * leading (24) rather than body's 20: inline code carries its own padding, and
 * at 20 the chips of two wrapped lines sat a pixel apart and read as one
 * block. Tables keep 20 — they are scanned, not read. List markers use the tertiary tier
 * so the text column stays clean, inline code on surface/base (the same well
 * the code block and terminal use, one step down from the bubble), links in
 * status/info, and the blockquote marked by a 2px border/default rule rather
 * than by an indent alone.
 */
/** Exported for callers that want backtick/bold/italic/link styling without
 *  the block-level parser's paragraph-joining — a plan node's comment thread
 *  keeps its own line breaks (PlanCommentsPopover) rather than reflowing text
 *  that was never fenced as a code block into one run-on line. */
export function Inline({ text }: { text: string }) {
  return <InlineNodes tokens={parseInline(text)} />;
}

function InlineNodes({ tokens }: { tokens: InlineToken[] }) {
  return (
    <>
      {tokens.map((token, idx) => {
        switch (token.type) {
          case "code":
            return (
              // The notes editor's inline code, class for class. It sat on
              // surface/base, which is the transcript's own ground since the
              // answer lost its bubble — a fill the same colour as the page,
              // so `git` in a sentence was only a font change.
              <code
                key={idx}
                // wrap-anywhere, not break-words: a filename or a path has no
                // spaces to wrap at, and break-word/break-words is excluded
                // from the browser's min-content calculation by spec — an
                // ancestor that sizes off min-content (any flex item without
                // its own min-w-0) still opens the line, and the ScrollArea
                // it sits in, sideways, even with break-words set right here.
                // wrap-anywhere counts toward min-content, so it actually
                // holds regardless of what the ancestor chain does.
                className="whitespace-pre-wrap wrap-anywhere rounded-md bg-muted px-(--space-inline-code-x) py-(--space-inline-code-y) text-code-12 text-text-primary"
              >
                {token.value}
              </code>
            );
          case "secret":
            return <MaskedSecret key={idx} value={token.value} />;
          case "link":
            return (
              <a
                key={idx}
                href={token.href}
                target="_blank"
                rel="noreferrer"
                // A long URL is exactly the same unbroken-run case as inline
                // code, and had no wrap protection of its own at all.
                className="wrap-anywhere text-status-info underline"
              >
                <InlineNodes tokens={token.children} />
              </a>
            );
          case "strong":
            return (
              <strong key={idx} className="font-medium text-text-primary">
                <InlineNodes tokens={token.children} />
              </strong>
            );
          case "em":
            return (
              <em key={idx} className="italic">
                <InlineNodes tokens={token.children} />
              </em>
            );
          case "del":
            return (
              <del key={idx} className="text-text-tertiary">
                <InlineNodes tokens={token.children} />
              </del>
            );
          case "break":
            return <br key={idx} />;
          default:
            return <React.Fragment key={idx}>{token.value}</React.Fragment>;
        }
      })}
    </>
  );
}

const ALIGN = { left: "text-left", center: "text-center", right: "text-right" } as const;

function Block({ block }: { block: MarkdownBlock }) {
  switch (block.type) {
    case "heading":
      return (
        <p
          className={cn(
            "w-full wrap-anywhere text-text-primary",
            // Keep headings at least as large as prose; weight marks deeper levels.
            block.level <= 2 ? "text-title-16" : "text-title-14",
          )}
        >
          <InlineNodes tokens={block.inline} />
        </p>
      );

    case "list":
      return (
        <div className="flex w-full flex-col gap-1 text-body-14 leading-(--type-prose-leading) text-text-primary">
          {block.items.map((item, idx) => (
            <div key={idx} className="flex w-full items-start gap-2">
              <ListMarker item={item} ordered={block.ordered} number={block.start + idx} />
              {/* An item holds blocks: a nested list, a code block, a
                  second paragraph — drawn tighter than the answer's own. */}
              <div className="flex min-w-0 flex-1 flex-col gap-2 wrap-anywhere">
                {item.blocks.map((child, childIdx) => (
                  <Block key={childIdx} block={child} />
                ))}
              </div>
            </div>
          ))}
        </div>
      );

    case "quote":
      return (
        <div className="flex w-full items-stretch gap-3">
          <span
            aria-hidden="true"
            className="w-0.5 shrink-0 self-stretch bg-border"
          />
          {/* A quote is someone else's words, one tier down. */}
          <div className="flex min-w-0 flex-1 flex-col gap-2 wrap-anywhere [&_p]:text-text-secondary">
            {block.blocks.map((child, childIdx) => (
              <Block key={childIdx} block={child} />
            ))}
          </div>
        </div>
      );

    case "rule":
      return <hr className="w-full border-border-subtle" />;

    case "code":
      // wrap only a single-line fence: multi-line code keeps its own line
      // breaks (AgentCodeBlock's own reasoning), but one line has no
      // structure to lose, and a token or URL long enough to fill it read as
      // "the message ended" with everything past the fold behind a
      // horizontal scrollbar nothing hinted was there.
      return <AgentCodeBlock code={block.code} language={block.language} wrap={!block.code.includes("\n")} />;

    case "table":
      // A table is scanned along its rows and down its first column, and both
      // need drawing. The first column holds the row's name — "root (src/)",
      // "gateway/" — and does not wrap: wrapped, a name broke into a word and a
      // code chip on separate lines while the long column beside it had room.
      // Rows are divided by hairlines and light up under the pointer, cells sit
      // on their top line so a two-line cell does not float its neighbours to
      // its middle, and a column the markdown aligned (`:---:`, `---:`) is
      // aligned — right-aligned columns are numbers, so their digits line up.
      return (
        <div
          data-testid="agent-table"
          className="w-full overflow-x-auto rounded-control border border-border-subtle"
        >
          <table className="w-full border-collapse">
            <thead>
              <tr className="border-b border-border-subtle bg-(--state-soft)">
                {block.head.map((cell, idx) => (
                  <th
                    key={idx}
                    className={cn(
                      "px-3 py-2 align-bottom text-body-13-medium whitespace-nowrap text-text-secondary",
                      ALIGN[block.align[idx] ?? "left"],
                    )}
                  >
                    <InlineNodes tokens={cell} />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, rowIdx) => (
                <tr
                  key={rowIdx}
                  className="border-t border-border-subtle transition-colors duration-(--motion-fast) first:border-t-0 hover:bg-(--state-soft)"
                >
                  {row.map((cell, idx) => (
                    <td
                      key={idx}
                      className={cn(
                        "px-3 py-2 align-top text-body-14 text-text-primary",
                        idx === 0 && "whitespace-nowrap",
                        ALIGN[block.align[idx] ?? "left"],
                        block.align[idx] === "right" && "tabular-nums",
                      )}
                    >
                      <InlineNodes tokens={cell} />
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
        <p className="w-full text-body-14 leading-(--type-prose-leading) wrap-anywhere text-text-primary">
          <InlineNodes tokens={block.inline} />
        </p>
      );
  }
}

/** A list item's bullet, number, or — for `- [ ]` — its checkbox. */
function ListMarker({ item, ordered, number }: { item: ListItem; ordered: boolean; number: number }) {
  if (item.checked !== undefined) {
    const Icon = item.checked ? SquareCheck : Square;
    return (
      <Icon
        aria-label={item.checked ? "done" : "not done"}
        // Centred on the first line's 24px leading.
        className="mt-1 size-4 shrink-0 text-text-tertiary"
      />
    );
  }
  return (
    <span aria-hidden="true" className="shrink-0 whitespace-nowrap text-text-tertiary">
      {ordered ? `${number}.` : "•"}
    </span>
  );
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
export const AgentProse = React.memo(function AgentProse({ text, wrapBlock, className }: AgentProseProps) {
  const markdownRef = React.useRef<IncrementalMarkdownState | undefined>(undefined);
  const markdown = React.useMemo(() => {
    const next = parseMarkdownIncremental(text, markdownRef.current);
    markdownRef.current = next;
    return next;
  }, [text]);
  const { blocks, blockKeys } = markdown;
  return (
    <div
      data-testid="agent-prose"
      className={cn("flex w-full flex-col gap-4 select-text", className)}
    >
      {blocks.map((block, idx) => {
        const node = <Block block={block} />;
        return <React.Fragment key={blockKeys[idx]}>{wrapBlock ? wrapBlock(node, idx) : node}</React.Fragment>;
      })}
    </div>
  );
});

import { memo, useCallback, useMemo, useRef, type ReactNode } from "react";
import { Square, SquareCheck } from "lucide-react";
import { openExternal } from "@/bridge/browser";
import { CodeBlock } from "@/components/code/CodeBlock";
import { DataTable, type DataTableColumn } from "@/components/ui/data-table/DataTable";
import { cn } from "@/lib/utils";
// The same reading of Markdown the agent transcript draws: marked's lexer turned
// into a small tree, so no HTML string is ever built — raw HTML in a file shows
// as the text it is, and an image shows as the name it points at (nothing is
// fetched from a path or a URL the file chose).
import { parseMarkdown, type InlineToken, type ListItem, type MarkdownBlock } from "@/lib/agentMarkdown";
import { headingSlugs, inlineText, type Slugs } from "./markdownHeadings";
import { resolveDocLink } from "./markdownLinks";

/**
 * A Markdown file, rendered. Read-only: the source is one switch away in the
 * file's editor. Links go three places and nowhere else — the web through the
 * system browser, a heading of this file by scrolling, and another file of the
 * same folder through `onOpenFile`; every other scheme is plain text.
 */
export const MarkdownPreview = memo(function MarkdownPreview({
  source,
  docPath,
  onOpenFile,
  className,
}: {
  source: string;
  /** The file's path in its folder; relative links are read from its directory. */
  docPath: string;
  onOpenFile: (path: string, anchor?: string) => void;
  className?: string;
}) {
  const blocks = useMemo(() => parseMarkdown(source), [source]);
  const slugs = useMemo(() => headingSlugs(blocks), [blocks]);
  const root = useRef<HTMLDivElement>(null);

  const scrollToHeading = useCallback((id: string) => {
    const wanted = id.toLowerCase();
    const target = Array.from(root.current?.querySelectorAll<HTMLElement>("[data-slug]") ?? []).find((el) => el.dataset.slug === wanted);
    target?.scrollIntoView({ block: "start" });
  }, []);

  const follow = useCallback(
    (href: string) => {
      const link = resolveDocLink(docPath, href);
      switch (link.kind) {
        case "url":
          void openExternal(link.url);
          break;
        case "anchor":
          scrollToHeading(link.id);
          break;
        case "file":
          onOpenFile(link.path, link.anchor);
          break;
      }
    },
    [docPath, onOpenFile, scrollToHeading],
  );

  return (
    <div ref={root} data-scroll-box className={cn("h-full min-h-0 overflow-y-auto", className)}>
      <article className="mx-auto flex w-full max-w-190 flex-col gap-4 px-6 py-6" aria-label="Rendered Markdown">
        {blocks.length === 0 ? <p className="text-body-14 text-text-tertiary">This file is empty.</p> : null}
        {blocks.map((block, i) => (
          <Block key={i} block={block} slugs={slugs} follow={follow} />
        ))}
      </article>
    </div>
  );
});

type Follow = (href: string) => void;

const HEADING_CLASS: Record<number, string> = {
  1: "text-heading-24 pt-2",
  2: "text-title-16 pt-3 border-b border-border-subtle pb-1.5",
  3: "text-title-14 pt-2",
};

function Block({ block, slugs, follow }: { block: MarkdownBlock; slugs: Slugs; follow: Follow }) {
  switch (block.type) {
    case "heading": {
      const Tag = `h${Math.min(6, Math.max(1, block.level))}` as "h1";
      return (
        <Tag data-slug={slugs.get(block)} className={cn("scroll-mt-4 text-text-primary wrap-anywhere", HEADING_CLASS[block.level] ?? "text-title-14")}>
          <Inline tokens={block.inline} follow={follow} />
        </Tag>
      );
    }
    case "list": {
      const List = block.ordered ? "ol" : "ul";
      return (
        <List className="flex flex-col gap-1 text-body-14 leading-(--type-prose-leading) text-text-primary">
          {block.items.map((item, i) => (
            <li key={i} className="flex items-start gap-2">
              <Marker item={item} ordered={block.ordered} number={block.start + i} />
              <div className="flex min-w-0 flex-1 flex-col gap-2 wrap-anywhere">
                {item.blocks.map((child, j) => (
                  <Block key={j} block={child} slugs={slugs} follow={follow} />
                ))}
              </div>
            </li>
          ))}
        </List>
      );
    }
    case "quote":
      return (
        <blockquote className="flex items-stretch gap-3">
          <span aria-hidden="true" className="w-0.5 shrink-0 self-stretch bg-border" />
          <div className="flex min-w-0 flex-1 flex-col gap-2 wrap-anywhere [&_p]:text-text-secondary">
            {block.blocks.map((child, i) => (
              <Block key={i} block={child} slugs={slugs} follow={follow} />
            ))}
          </div>
        </blockquote>
      );
    case "rule":
      return <hr className="border-border-subtle" />;
    case "code":
      return (
        <CodeBlock
          code={block.code}
          language={block.language}
          className="overflow-x-auto rounded-control border border-border-subtle bg-surface-well px-3 py-2 text-code-12"
        />
      );
    case "table":
      return <MarkdownTable block={block} follow={follow} />;
    default:
      return (
        <p className="text-body-14 leading-(--type-prose-leading) text-text-primary wrap-anywhere">
          <Inline tokens={block.inline} follow={follow} />
        </p>
      );
  }
}

function Marker({ item, ordered, number }: { item: ListItem; ordered: boolean; number: number }) {
  if (item.checked !== undefined) {
    const Icon = item.checked ? SquareCheck : Square;
    return <Icon aria-label={item.checked ? "done" : "not done"} className="mt-1 size-4 shrink-0 text-text-tertiary" />;
  }
  return (
    <span aria-hidden="true" className="shrink-0 whitespace-nowrap text-text-tertiary">
      {ordered ? `${number}.` : "•"}
    </span>
  );
}

function MarkdownTable({ block, follow }: { block: Extract<MarkdownBlock, { type: "table" }>; follow: Follow }) {
  const columns = useMemo<DataTableColumn[]>(
    () =>
      block.head.map((cell, i) => ({
        key: String(i),
        header: <Inline tokens={cell} follow={follow} />,
        label: inlineText(cell),
        align: block.align[i] ?? "left",
        nowrap: i === 0,
      })),
    [block.head, block.align, follow],
  );
  const rows = useMemo(() => block.rows.map((row) => row.map((cell, i) => <Inline key={i} tokens={cell} follow={follow} />)), [block.rows, follow]);
  const cellText = useMemo(() => block.rows.slice(0, 50).map((row) => row.map(inlineText)), [block.rows]);
  return <DataTable className="rounded-control border border-border-subtle" columns={columns} rows={rows} cellText={cellText} />;
}

function Inline({ tokens, follow }: { tokens: InlineToken[]; follow: Follow }): ReactNode {
  return (
    <>
      {tokens.map((token, i) => {
        switch (token.type) {
          case "text":
            return token.value;
          case "break":
            return <br key={i} />;
          case "code":
          case "secret":
            return (
              <code key={i} className="rounded-sm bg-state-soft px-1 py-0.5 text-code-12 wrap-anywhere">
                {token.value}
              </code>
            );
          case "strong":
            return (
              <strong key={i} className="font-semibold">
                <Inline tokens={token.children} follow={follow} />
              </strong>
            );
          case "em":
            return (
              <em key={i}>
                <Inline tokens={token.children} follow={follow} />
              </em>
            );
          case "del":
            return (
              <del key={i} className="text-text-secondary">
                <Inline tokens={token.children} follow={follow} />
              </del>
            );
          case "link":
            return (
              <a
                key={i}
                href={token.href}
                className="text-text-primary underline decoration-text-tertiary underline-offset-2 hover:decoration-text-primary focus-visible:ring-1 focus-visible:ring-status-info focus-visible:outline-none"
                title={token.href}
                // The webview must never navigate by itself, whatever the click.
                onClick={(e) => {
                  e.preventDefault();
                  follow(token.href);
                }}
                onAuxClick={(e) => e.preventDefault()}
              >
                <Inline tokens={token.children} follow={follow} />
              </a>
            );
        }
      })}
    </>
  );
}

import { NodeApi, KEYS } from "platejs";
import { PlateElement, useEditorRef, useEditorSelector, type PlateElementProps } from "platejs/react";
import { cn } from "@/lib/utils";

type Heading = { index: number; level: number; text: string };

const HEADING_LEVEL: Record<string, number> = {
  [KEYS.h1]: 1,
  [KEYS.h2]: 2,
  [KEYS.h3]: 3,
  [KEYS.h4]: 4,
  [KEYS.h5]: 5,
  [KEYS.h6]: 6,
};

/** The page's own headings, in order — top-level ones, which is where a note's outline lives. */
function headingsOf(children: readonly unknown[]): Heading[] {
  const out: Heading[] = [];
  children.forEach((node, index) => {
    const level = HEADING_LEVEL[(node as { type?: string }).type ?? ""];
    const text = level ? NodeApi.string(node as never).trim() : "";
    if (level && text) out.push({ index, level, text });
  });
  return out;
}

function sameHeadings(a: Heading[], b: Heading[]) {
  return a.length === b.length && a.every((h, i) => h.index === b[i]!.index && h.level === b[i]!.level && h.text === b[i]!.text);
}

/** Left padding by depth, from the shallowest heading present (a note that opens at H2 is not indented). */
const INDENT = ["pl-0", "pl-4", "pl-8", "pl-12", "pl-16", "pl-20"];

/**
 * A table of contents: the headings of the note, each a link that scrolls to
 * it. Not stored as a list — it is derived at draw time, and its Markdown is
 * the line `[TOC]` (markdownRules.ts), so a heading renamed or added shows up
 * without the block being touched.
 */
export function TocElement(props: PlateElementProps) {
  const editor = useEditorRef();
  const headings = useEditorSelector((current) => headingsOf(current.children), [], { equalityFn: sameHeadings });
  const top = Math.min(...headings.map((heading) => heading.level));

  return (
    <PlateElement {...props} className="my-2">
      <nav
        aria-label="Table of contents"
        contentEditable={false}
        className="rounded-control border border-border-subtle px-3 py-2 select-none"
      >
        <p className="mb-1 text-caption-12 text-text-tertiary">Contents</p>
        {headings.length === 0 ? (
          <p className="text-body-13 text-text-tertiary">Add a heading and it will be listed here.</p>
        ) : (
          <ul className="flex flex-col">
            {headings.map((heading) => (
              <li key={heading.index} className={INDENT[heading.level - top] ?? INDENT[INDENT.length - 1]}>
                <button
                  type="button"
                  className={cn(
                    "w-full truncate rounded-sm py-0.5 text-left text-body-14 text-text-secondary outline-none",
                    "hover:text-text-primary focus-visible:ring-1 focus-visible:ring-ring",
                  )}
                  onClick={() => {
                    const node = editor.children[heading.index];
                    if (node) editor.api.toDOMNode(node)?.scrollIntoView({ block: "start", behavior: "smooth" });
                  }}
                >
                  {heading.text}
                </button>
              </li>
            ))}
          </ul>
        )}
      </nav>
      {props.children}
    </PlateElement>
  );
}

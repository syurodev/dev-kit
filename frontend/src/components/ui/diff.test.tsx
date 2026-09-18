import { describe, expect, it } from "bun:test";
import { renderToString } from "react-dom/server";

import { DiffSplitPanes, DIFF_ROW_HEIGHT } from "./diff";
import { changedRuns, splitPairs, sourceLines } from "@/lib/diffHunks";
import { lineDiff } from "@/modules/notes/noteDiff";

const build = (before: string, after: string) => {
  const sourced = sourceLines(lineDiff(before, after));
  const pairs = splitPairs(sourced);
  return { pairs, runs: changedRuns(pairs) };
};

describe("DiffSplitPanes (Figma 08 Diff, example D)", () => {
  it("faces the file and what it becomes, with the decided side emphasised", () => {
    const { pairs, runs } = build("keep\nold\n", "keep\nnew\n");
    const html = renderToString(
      <DiffSplitPanes pairs={pairs} runs={runs} leftLabel="On disk" rightLabel="Proposed" />,
    );

    expect(html).toContain('data-emphasis="default"');
    expect(html).toContain('data-emphasis="active"');
    expect(html.indexOf("On disk")).toBeLessThan(html.indexOf("Proposed"));
    expect(html).toContain('data-kind="removed"');
    expect(html).toContain('data-kind="added"');
  });

  it("draws one ribbon band per changed run, on the rows it joins", () => {
    // Two separate edits: two bands, not one covering the quiet lines between.
    const { pairs, runs } = build("a\nb\nc\nd\ne\n", "a\nB\nc\nD\ne\n");
    const html = renderToString(
      <DiffSplitPanes pairs={pairs} runs={runs} leftLabel="a" rightLabel="b" />,
    );
    const rects = html.match(/<rect /g) ?? [];
    expect(rects).toHaveLength(2);
    // Row 1 and row 3 changed: the bands sit at their offsets, one row tall.
    expect(html).toContain(`y="${DIFF_ROW_HEIGHT}"`);
    expect(html).toContain(`y="${DIFF_ROW_HEIGHT * 3}"`);
    expect(html).toContain(`height="${DIFF_ROW_HEIGHT}"`);
  });

  it("leaves a blank opposite a line the other side does not have", () => {
    const { pairs, runs } = build("a\n", "a\nb\n");
    const html = renderToString(
      <DiffSplitPanes pairs={pairs} runs={runs} leftLabel="a" rightLabel="b" />,
    );
    expect(html).toContain('data-kind="blank"');
  });
});

describe("line numbers", () => {
  it("numbers each side from the file it belongs to", async () => {
    const { pairs, runs } = build("a\nb\nc", "a\nB\nc");
    const html = renderToString(
      <DiffSplitPanes pairs={pairs} runs={runs} leftLabel="On disk" rightLabel="Proposed" />,
    );
    // Each pane numbers its own file from 1, left pane first in the markup.
    const numbers = [...html.matchAll(/data-slot="diff-line-number"[^>]*>(\d+)</g)].map((m) => m[1]);
    expect(numbers).toEqual(["1", "2", "3", "1", "2", "3"]);
    // Out of the selection: copying the diff gives code, not numbered code.
    expect(html).toContain("select-none");
  });
});

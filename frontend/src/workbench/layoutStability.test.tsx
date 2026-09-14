import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToString } from "react-dom/server";
import { Chip } from "@/components/ui/chip";

const SRC = join(import.meta.dir, "..");
const read = (rel: string) => readFileSync(join(SRC, rel), "utf8");

/**
 * Smoothness 3.2/3.4/3.9: the things that make a pane move after it has
 * already been drawn — a chip that arrives late, a scrollbar that appears, an
 * empty state that is a different shape from the content it replaces.
 */
describe("layout stability", () => {
  it("a placeholder chip reserves the same box as the filled one", () => {
    // The chip is what holds Kafka's topic title still while describeTopic is
    // out. Placeholder and filled must be the same element with the same width
    // floor — only the visibility differs — or the swap moves the title.
    const placeholder = renderToString(
      <Chip className="min-w-24 justify-center">
        <span className="tabular-nums">0</span> partitions
      </Chip>,
    );
    const filled = renderToString(
      <Chip className="min-w-24 justify-center">
        <span className="tabular-nums">12</span> partitions
      </Chip>,
    );
    for (const html of [placeholder, filled]) {
      expect(html).toContain("min-w-24");
      expect(html).toContain("tabular-nums");
      expect(html).toContain("h-6");
    }
    // Same classes on both, so the box cannot differ.
    const classesOf = (html: string) => html.match(/class="([^"]*)"/)?.[1];
    expect(classesOf(placeholder)).toBe(classesOf(filled));
  });

  it("the Kafka topic header keeps its chip row mounted", () => {
    const body = read("modules/kafka/KafkaView.tsx");
    // Mounted-but-invisible, not conditionally rendered: the row must survive
    // topicDetail being undefined.
    expect(body).toContain('!topicDetail && "invisible"');
    expect(body).not.toMatch(/\{topicDetail && \(\s*<div className="flex items-center gap-2">/);
  });

  it("scroll areas that gain and lose a scrollbar reserve the gutter", () => {
    const scrollers: [string, string][] = [
      // The console's result scroller moved into the shared grid.
      ["modules/database/grid/ResultGrid.tsx", "result grid"],
      ["modules/database/components/QueryHistoryView.tsx", "history list"],
      ["modules/database/components/SavedQueriesView.tsx", "saved list"],
      ["modules/database/components/LiveActivityView.tsx", "activity table"],
      ["modules/kafka/KafkaView.tsx", "peek table"],
      ["modules/kafka/KafkaSidebar.tsx", "rail"],
      ["modules/database/components/DatabaseSidebar.tsx", "rail"],
    ];
    for (const [file, what] of scrollers) {
      // Reserve the scrolling edge only: mirroring the scrollbar gutter on
      // the other edge wastes label room in narrow rails.
      expect(read(file), `${what} (${file}) must not reflow when rows cross the scroll threshold`)
        .toContain("scrollbar-gutter-stable");
    }
  });

  it("converted empty states use the shared primitive, not py-16 copy", () => {
    for (const file of [
      "modules/database/components/QueryHistoryView.tsx",
      "modules/database/components/SavedQueriesView.tsx",
      "modules/database/components/LiveActivityView.tsx",
      "modules/kafka/KafkaView.tsx",
    ]) {
      const body = read(file);
      expect(body, `${file} still centres an ad-hoc empty state`).not.toContain("py-16");
      expect(body).toContain("EmptyState");
    }
  });
});

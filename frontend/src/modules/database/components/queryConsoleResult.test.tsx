import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToString } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/tooltip";
import { QueryConsole } from "./QueryConsole";

const render = () =>
  renderToString(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <QueryConsole consoleId="never-run" />
      </TooltipProvider>
    </QueryClientProvider>,
  );

/**
 * The result is a slot, and a slot with nothing in it is not drawn.
 *
 * It used to be a card telling you to type a query, under an editor you were
 * already typing in — a third of the console spent on instructions nobody
 * needed twice, and a handle to drag between the editor and an empty box.
 *
 * Only the unrun console is rendered here. Zustand's server snapshot is the
 * store's INITIAL state, so a seeded `consoles` never reaches a component
 * through renderToString and a "with a result" case would silently be
 * testing the empty one. The states that need a store are measured in the
 * browser instead; what a source read can still hold is that the markup for
 * them exists and is behind the guard.
 */
describe("the result slot", () => {
  it("is not there before anything has run", () => {
    const html = render();
    expect(html, "the empty card is gone").not.toContain("Query console ready");
    expect(html, "a handle to drag against nothing").not.toContain("resizable-handle");
    expect(html, "the editor is the whole console").toContain("cm-surfaceless");
  });

  const source = readFileSync(join(import.meta.dir, "QueryConsole.tsx"), "utf8");

  it("draws the panel and its handle only behind the result", () => {
    const guard = source.indexOf("{queryResult ? (");
    expect(guard).toBeGreaterThan(-1);
    expect(source.indexOf("<ResizableHandle"), "the handle is inside the guard").toBeGreaterThan(
      guard,
    );
    expect(source.indexOf('id="results"'), "the panel is inside the guard").toBeGreaterThan(guard);
  });

  /*
   * Every number that header printed is in the pill beside Run. Printing one
   * twice makes the reader check whether the two agree — which is how "0.0s"
   * sitting next to "32 ms" about the same run was found.
   */
  it("says nothing the pill already says", () => {
    const panel = source.slice(source.indexOf('id="results"'));
    expect(panel).not.toContain("queryResult.rowCount");
    expect(panel).not.toContain("queryDurationMs");
    expect(panel).not.toContain("columns.length");
  });

  // Nothing else says it: a result cut at the row ceiling looks exactly like
  // a complete one.
  it("still says when the rows were cut off, and offers a way out", () => {
    const panel = source.slice(source.indexOf('id="results"'));
    expect(panel).toContain("row cap reached");
    expect(panel).toContain("Close the result");
  });

  // Open in Database from a db.query card shows the agent's rows here without
  // running anything; until the person runs it the panel says whose rows they
  // are, and the note carries the cut (the cap chip would say it twice).
  it("says when the rows are the agent's, not a run of this console", () => {
    const panel = source.slice(source.indexOf('id="results"'));
    expect(panel).toContain('origin === "agent"');
    expect(panel).toContain("agentResultNote(");
    const note = panel.indexOf("agentResultNote(");
    const chip = panel.indexOf("row cap reached");
    expect(note, "the note and the chip are one choice").toBeLessThan(chip);
  });
});

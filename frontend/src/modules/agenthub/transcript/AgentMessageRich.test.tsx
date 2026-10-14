import { describe, expect, test } from "bun:test";
import { renderToString } from "react-dom/server";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AgentProse } from "./AgentMessageRich";

const render = (text: string) =>
  renderToString(
    <TooltipProvider>
      <AgentProse text={text} settled />
    </TooltipProvider>,
  );

const TABLE = [
  "| Folder | Files | Note |",
  "| :--- | ---: | :---: |",
  "| root (`src/`) | 12 | **main** entry |",
  "| gateway/ | 3 | ok |",
].join("\n");

describe("markdown tables in prose", () => {
  test("draw as the shared resizable table", () => {
    const html = render(TABLE);
    expect(html).toContain('data-testid="agent-table"');
    expect(html).toContain("table-fixed");
    expect(html).toContain("<colgroup>");
    expect(html.match(/role="separator"/g)?.length).toBe(3);
  });

  test("keep their alignment and first-column emphasis", () => {
    const html = render(TABLE);
    const firstRow = html.split("<tbody")[1].split("</tr>")[0];
    const cells = firstRow.split("<td").slice(1);
    expect(cells[0]).toContain("whitespace-nowrap");
    expect(cells[1]).toContain("text-right");
    expect(cells[1]).toContain("tabular-nums");
    expect(cells[2]).toContain("text-center");
    expect(cells[1]).not.toContain("whitespace-nowrap");
  });

  test("keep inline markdown in their cells", () => {
    const html = render(TABLE);
    expect(html).toContain("<strong");
    expect(html).toContain("src/");
  });
});
